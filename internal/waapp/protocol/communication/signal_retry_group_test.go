package communication

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestGroupRetryTargetsOneMemberAndRetainsSenderKeyWithMedia(t *testing.T) {
	alice, bob := newMessageParty(t), newMessageParty(t)
	alice.RegistrationJID = "12025550123@s.whatsapp.net"
	bob.RegistrationJID = "12025550124@s.whatsapp.net"
	bundle, err := preKeyBundleFromNode(messagePartyBundle(t, bob, true))
	if err != nil {
		t.Fatal(err)
	}
	if err := initializeOutboundSession(&alice, bob.RegistrationJID, bundle); err != nil {
		t.Fatal(err)
	}
	directRoundtrip(t, &alice, &bob, "initial")
	const group = "120363000001@g.us"
	metadata := wire.ChatdNode{Tag: "group", Attrs: map[string]string{"id": group, "subject": "Synthetic group"}, Content: []wire.ChatdNode{
		{Tag: "participant", Attrs: map[string]string{"jid": alice.RegistrationJID}},
		{Tag: "participant", Attrs: map[string]string{"jid": bob.RegistrationJID}},
	}}
	image := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("group image"), MediaKey: []byte("synthetic-private-key")}}
	original := sendSyntheticGroupMessage(t, &alice, metadata, image)
	alice = reloadMessageParty(t, alice)
	receipt, err := buildSignalRetryReceipt(&bob, stateMessagePayload(group, alice.RegistrationJID, original.Attrs["id"]), 1)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Attrs["from"] = group
	receipt.Attrs["participant"] = bob.RegistrationJID
	delete(receipt.Attrs, "to")
	session, peer := syntheticChatdPair(t)
	session.PhoneJID = alice.RegistrationJID
	request, valid := session.parseSignalRetry(receipt)
	if !valid {
		t.Fatal("group retry did not parse")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	failed := make(chan error, 1)
	go func() {
		node, err := peer.ReadNode()
		if err == nil {
			err = peer.SendNode(wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": node.Attrs["id"], "type": "result"}, Content: []wire.ChatdNode{metadata}})
		}
		failed <- err
	}()
	preparation := messagePreparation{session: session, current: &alice, timeout: time.Second}
	response, err := preparation.retryMessage(ctx, receipt, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-failed; err != nil {
		t.Fatal(err)
	}
	if response.Attrs["id"] != original.Attrs["id"] || response.Attrs["to"] != group || response.Attrs["participant"] != bob.RegistrationJID || len(wire.ChatdChildren(response)) != 1 {
		t.Fatal("group retry lost its id or targeted routing")
	}
	enc, _ := ChatdChild(response, "enc")
	plain, err := DecryptNativeSignalPayloadByType(&bob, enc.Content.([]byte), enc.Attrs["type"], alice.RegistrationJID, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := applySignalControlMessage(ctx, &bob, alice.RegistrationJID, plain.Plaintext); err != nil {
		t.Fatal(err)
	}
	body, err := DecodeSignalMessage(plain.Plaintext)
	if err != nil {
		t.Fatal(err)
	}
	text, content, kind := ProjectMessage(body, true)
	if text != "[图片] group image" || kind != waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE || content.GetType() != waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_IMAGE || len(bob.SenderKeyRecords) == 0 {
		t.Fatal("group retry dropped the media body or sender key")
	}
	if duplicate, err := preparation.retryMessage(ctx, receipt, request); err != nil || duplicate.Tag != "" {
		t.Fatal("duplicate retry consumed another message key")
	}
	request.sender.User = "12025550125"
	if _, err := preparation.retryMessage(ctx, receipt, request); !errors.Is(err, errSignalRetryRejected) {
		t.Fatal("an unaddressed device obtained group plaintext")
	}
}

func stateMessagePayload(chat, sender, id string) state.NativeMessagePayload {
	return state.NativeMessagePayload{Contact: chat, Sender: sender, ProviderMessageID: id, ProviderTimestamp: time.Now().Unix()}
}
