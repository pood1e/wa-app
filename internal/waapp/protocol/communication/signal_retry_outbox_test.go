package communication

import (
	"bytes"
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestRetryOutboxResumesExactCiphertextAfterWriteFailure(t *testing.T) {
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
	body := &waE2E.Message{Conversation: proto.String("outbox recovery")}
	node := wire.ChatdNode{Tag: "message", Attrs: map[string]string{"id": "outbox-provider", "to": bob.RegistrationJID}, Content: []wire.ChatdNode{{Tag: "participants", Content: []wire.ChatdNode{{Tag: "to", Attrs: map[string]string{"jid": bob.RegistrationJID}}}}}}
	if _, err := rememberSentMessage(&alice, body, node, time.Now()); err != nil {
		t.Fatal(err)
	}
	receipt, err := buildSignalRetryReceipt(&bob, stateMessagePayload(alice.RegistrationJID, alice.RegistrationJID, "outbox-provider"), 2)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Attrs["from"] = bob.RegistrationJID
	delete(receipt.Attrs, "to")
	if !queueSignalRetries(&alice, []wire.ChatdNode{receipt}) {
		t.Fatal("retry was not queued")
	}
	repository := &serializedMessageState{}
	engine, err := New(repository, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.CloseIdleConnections)
	input := wacore.EngineMessageInput{ClientProfileID: "synthetic-profile"}
	if err := engine.SaveState(t.Context(), input.ClientProfileID, alice); err != nil {
		t.Fatal(err)
	}
	session, peer := syntheticChatdPair(t)
	session.PhoneJID = alice.RegistrationJID
	for _, pending := range alice.PendingSignalRetries {
		decoded, err := wire.NewBinaryNodeCodec().DecodeNodePayload(pending.GetReceipt())
		if err != nil {
			t.Fatal("stored retry node decode failed", err)
		}
		parsed, valid := session.parseSignalRetry(decoded)
		if !valid {
			t.Fatal("stored retry source did not parse")
		}
		copy := reloadMessageParty(t, alice)
		preparation := messagePreparation{session: session, current: &copy}
		if response, err := preparation.retryMessage(t.Context(), decoded, parsed); err != nil || response.Tag == "" {
			t.Fatalf("stored retry preparation failed: %v", err)
		}
	}
	closed := make(chan error, 1)
	go func() {
		_, err := peer.ReadNode()
		closeErr := session.Conn.Close()
		if err == nil {
			err = closeErr
		}
		closed <- err
	}()
	runner := NewConnection(engine, ConnectionOptions{Context: t.Context(), Input: input})
	runner.Session = session
	t.Cleanup(func() { _ = runner.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := runner.flushSignalRetries(ctx, &alice); err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	alice, err = engine.loadState(ctx, input.ClientProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if len(alice.PendingSignalRetries) != 1 {
		t.Fatal("failed write discarded retry work")
	}
	var expected []byte
	for _, pending := range alice.PendingSignalRetries {
		expected = bytes.Clone(pending.GetResponse())
	}
	if len(expected) == 0 {
		t.Fatal("ciphertext was not persisted before its failed write")
	}
	before := alice.Signal.Sessions[bob.RegistrationJID].SenderChain.Index
	session, peer = syntheticChatdPair(t)
	session.PhoneJID = alice.RegistrationJID
	runner.Session = session
	responses := make(chan wire.ChatdNode, 1)
	failed := make(chan error, 1)
	go func() {
		for range 2 {
			response, err := peer.ReadNode()
			if err != nil {
				failed <- err
				return
			}
			if response.Tag == "message" {
				responses <- response
			}
		}
	}()
	if err := runner.flushSignalRetries(ctx, &alice); err != nil {
		t.Fatal(err)
	}
	var response wire.ChatdNode
	select {
	case response = <-responses:
	case err := <-failed:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	codec := wire.NewBinaryNodeCodec()
	saved, err := codec.DecodeNodePayload(expected)
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := ChatdChild(response, "enc")
	stored, _ := ChatdChild(saved, "enc")
	if !bytes.Equal(enc.Content.([]byte), stored.Content.([]byte)) || before != alice.Signal.Sessions[bob.RegistrationJID].SenderChain.Index || len(alice.PendingSignalRetries) > 0 {
		t.Fatal("outbox replay advanced the ratchet or changed ciphertext")
	}
	plain, err := DecryptNativeSignalPayloadByType(&bob, enc.Content.([]byte), enc.Attrs["type"], alice.RegistrationJID, true)
	if err != nil || NativePlaintextText(plain.Plaintext) != "outbox recovery" {
		t.Fatalf("resumed retry did not decrypt: %v", err)
	}
}
