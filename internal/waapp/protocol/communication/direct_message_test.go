package communication

import (
	"bytes"
	"context"
	"fmt"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"testing"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestDirectMessageDiscoversDevicesAndMigratesPNWithoutResettingSession(t *testing.T) {
	alice := newMessageParty(t)
	alice.EnsureMaps()
	bob, companion := newMessageParty(t), newMessageParty(t)
	const pn, lid = "12025550124@s.whatsapp.net", "987654321@lid"
	bundle, err := preKeyBundleFromNode(messagePartyBundle(t, bob, true))
	if err != nil {
		t.Fatal(err)
	}
	if err := initializeOutboundSession(&alice, pn, bundle); err != nil {
		t.Fatal(err)
	}
	base := alice.Signal.Sessions[pn].AliceBaseKey
	token := []byte("synthetic-issued-contact-token")
	alice.PrivacyTokens[lid] = state.NativePrivacyToken{Token: wire.B64u(token), Timestamp: time.Now().Unix()}
	session, peer := syntheticChatdPair(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	finished := make(chan wire.ChatdNode, 1)
	failed := make(chan error, 1)
	companionBundle := messagePartyBundle(t, companion, true)
	go func() {
		for range 3 {
			node, err := peer.ReadNode()
			if err != nil {
				failed <- err
				return
			}
			response := wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"type": "result", "id": node.Attrs["id"]}}
			switch node.Attrs["xmlns"] {
			case "usync":
				response.Content = []wire.ChatdNode{{Tag: "usync", Content: []wire.ChatdNode{{Tag: "list", Content: []wire.ChatdNode{{
					Tag: "user", Attrs: map[string]string{"jid": pn}, Content: []wire.ChatdNode{
						{Tag: "lid", Attrs: map[string]string{"val": lid}},
						{Tag: "devices", Content: []wire.ChatdNode{{Tag: "device-list", Content: []wire.ChatdNode{
							{Tag: "device", Attrs: map[string]string{"id": "0"}}, {Tag: "device", Attrs: map[string]string{"id": "3"}},
						}}}},
					},
				}}}}}}
			case "encrypt":
				key, _ := ChatdChild(node, "key")
				user, _ := ChatdChild(key, "user")
				if user.Attrs["jid"] != "987654321:3@lid" {
					failed <- fmt.Errorf("existing primary session was reset")
					return
				}
				companionBundle.Attrs = map[string]string{"jid": user.Attrs["jid"]}
				response.Content = []wire.ChatdNode{{Tag: "list", Content: []wire.ChatdNode{companionBundle}}}
			default:
				if node.Tag != "message" {
					failed <- fmt.Errorf("unexpected request %s", node.Tag)
					return
				}
				finished <- node
				response = wire.ChatdNode{Tag: "ack", Attrs: map[string]string{"class": "message", "id": node.Attrs["id"]}}
			}
			if err := peer.SendNode(response); err != nil {
				failed <- err
				return
			}
		}
	}()
	repository := &serializedMessageState{}
	engine, err := New(repository, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.CloseIdleConnections()
	result := engine.SendMessageOnSession(ctx, session, &alice, wacore.EngineMessageSendInput{ContactJID: pn, Message: &waE2E.Message{Conversation: proto.String("two devices 中文🙂")}},
		wacore.EngineMessageInput{WAAccountID: "synthetic", MessageSessionID: "synthetic-session"}, time.Second)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	var node wire.ChatdNode
	select {
	case node = <-finished:
	case err := <-failed:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	attached, ok := ChatdChild(node, "tctoken")
	if !ok || !bytes.Equal(attached.Content.([]byte), token) {
		t.Fatal("the issued token was not matched to the discovered LID")
	}
	participants, _ := ChatdChild(node, "participants")
	if len(wire.ChatdChildren(participants)) != 2 || node.Attrs["to"] != pn {
		t.Fatal("message fanout lost recipient routing")
	}
	for _, target := range wire.ChatdChildren(participants) {
		receiver := &bob
		if target.Attrs["jid"] == "12025550124:3@s.whatsapp.net" {
			receiver = &companion
		}
		enc, _ := ChatdChild(target, "enc")
		plain, err := DecryptNativeSignalPayloadByType(receiver, enc.Content.([]byte), enc.Attrs["type"], "synthetic-sender", true)
		if err != nil || NativePlaintextText(plain.Plaintext) != "two devices 中文🙂" {
			t.Fatalf("recipient could not decrypt its device payload: %v", err)
		}
	}
	alice = reloadMessageParty(t, alice)
	if _, exists := alice.Signal.Sessions[pn]; exists || alice.Signal.Sessions[lid].AliceBaseKey != base {
		t.Fatal("LID migration duplicated or reset the primary session")
	}
}
