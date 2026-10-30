package communication

import (
	"bytes"
	"context"
	"strconv"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestSignalRetryRecoversLostSessionAndDecryptsReplyAfterRestart(t *testing.T) {
	for _, platform := range []protocolv1.ClientPlatform{protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID, protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS} {
		t.Run(platform.String(), func(t *testing.T) { testSignalRetryConversation(t, platform) })
	}
}

func testSignalRetryConversation(t *testing.T, platform protocolv1.ClientPlatform) {
	t.Helper()
	alice, bob := newMessageParty(t), newMessageParty(t)
	alice.RegistrationJID = "12025550123@s.whatsapp.net"
	bob.RegistrationJID = "12025550124@s.whatsapp.net"
	bob.Client.Platform = platform
	bundle, err := preKeyBundleFromNode(messagePartyBundle(t, bob, true))
	if err != nil {
		t.Fatal(err)
	}
	if err := initializeOutboundSession(&alice, bob.RegistrationJID, bundle); err != nil {
		t.Fatal(err)
	}
	directRoundtrip(t, &alice, &bob, "initial")
	before, _, err := bob.Export()
	if err != nil {
		t.Fatal(err)
	}
	original := &waE2E.Message{Conversation: proto.String("recover this 中文🙂")}
	node := wire.ChatdNode{Tag: "message", Attrs: map[string]string{"id": "stable-provider", "to": bob.RegistrationJID}, Content: []wire.ChatdNode{{Tag: "participants", Content: []wire.ChatdNode{{Tag: "to", Attrs: map[string]string{"jid": bob.RegistrationJID}}}}}}
	if _, err := rememberSentMessage(&alice, original, node, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, kind, err := encryptDirectSignalPayload(&alice, bob.RegistrationJID, NativeTextMessagePlaintext(original.GetConversation()))
	if err != nil {
		t.Fatal(err)
	}
	delete(bob.Signal.Sessions, SignalSenderKey(alice.RegistrationJID))
	input := wacore.EngineMessageInput{WAAccountID: "synthetic-account", ClientProfileID: "synthetic-profile", MessageSessionID: "synthetic-session"}
	repository := &serializedMessageState{}
	engine, err := New(repository, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.CloseIdleConnections)
	session, peer := syntheticChatdPair(t)
	session.PhoneJID = bob.RegistrationJID
	runner := NewConnection(engine, ConnectionOptions{Context: t.Context(), Input: input})
	runner.Session = session
	t.Cleanup(func() { _ = runner.Close() })
	for count := uint32(1); count <= 2; count++ {
		ApplyChatdReceiveState(&bob, input, []wire.ChatdEncPayload{{StanzaID: "stable-provider", StanzaTimestamp: strconv.FormatInt(time.Now().Unix(), 10), Contact: alice.RegistrationJID, Sender: alice.RegistrationJID, EncType: kind, Payload: raw, RetryCount: count - 1}}, ChatdSessionUpdate{})
		payloadRef := wire.PayloadRefForEnc(input.WAAccountID, raw)
		if err := engine.SaveState(t.Context(), input.ClientProfileID, bob); err != nil {
			t.Fatal(err)
		}
		request := receiveRetryRequest(t, runner, peer, wacore.EngineDecryptInput{MessageID: "failed-attempt", ClientProfileID: input.ClientProfileID, PayloadRef: payloadRef, IncludePlaintextText: true, SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE})
		bob, err = engine.loadState(t.Context(), input.ClientProfileID)
		if err != nil {
			t.Fatal(err)
		}
		key := retryStateKey(alice.RegistrationJID, alice.RegistrationJID, "stable-provider")
		if bob.SignalRetries[key].GetRequestedCount() != count || len(bob.SignalRetries[key].GetPendingReceipt()) != 0 {
			t.Fatal("retry counter or dispatch state was not durable")
		}
		request.Attrs["from"] = bob.RegistrationJID
		delete(request.Attrs, "to")
		aliceSession := &ChatdSession{PhoneJID: alice.RegistrationJID}
		parsed, ok := aliceSession.parseSignalRetry(request)
		if !ok {
			t.Fatal("native retry receipt did not parse")
		}
		preparation := messagePreparation{current: &alice, session: aliceSession}
		response, err := preparation.retryMessage(t.Context(), request, parsed)
		if err != nil {
			t.Fatal(err)
		}
		if response.Attrs["id"] != "stable-provider" || response.Attrs["device_fanout"] != "false" {
			t.Fatal("retry changed the message id or fanned out")
		}
		enc, _ := ChatdChild(response, "enc")
		raw, kind = enc.Content.([]byte), enc.Attrs["type"]
		if count == 1 && kind != "msg" || count == 2 && kind != "pkmsg" {
			t.Fatalf("retry %d encrypted as %s", count, kind)
		}
		alice = reloadMessageParty(t, alice)
	}
	plain, err := DecryptNativeSignalPayloadByType(&bob, raw, kind, alice.RegistrationJID, true)
	if err != nil || NativePlaintextText(plain.Plaintext) != original.GetConversation() {
		t.Fatalf("retry did not restore the message: %v", err)
	}
	bob = reloadMessageParty(t, bob)
	directRoundtrip(t, &bob, &alice, "reply after recovery")
	after, _, err := bob.Export()
	if err != nil {
		t.Fatal(err)
	}
	before.OneTimePreKeys, after.OneTimePreKeys = nil, nil
	if !proto.Equal(before, after) {
		t.Fatal("message recovery replaced the account's own credentials")
	}
}

func receiveRetryRequest(t *testing.T, runner *Connection, peer *ChatdTransport, input wacore.EngineDecryptInput) wire.ChatdNode {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	nodes := make(chan wire.ChatdNode, 1)
	errors := make(chan error, 1)
	go func() {
		node, err := peer.ReadNode()
		if err != nil {
			errors <- err
		} else {
			nodes <- node
		}
	}()
	result := runner.DecryptMessage(ctx, input)
	if result.Err == nil {
		t.Fatal("a deliberately lost Signal session decrypted")
	}
	select {
	case node := <-nodes:
		return node
	case err := <-errors:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return wire.ChatdNode{}
}

func directRoundtrip(t *testing.T, sender, receiver *state.NativeState, text string) {
	t.Helper()
	for _, pair := range [][2]*state.NativeState{{sender, receiver}, {receiver, sender}} {
		raw, kind, err := encryptDirectSignalPayload(pair[0], pair[1].RegistrationJID, NativeTextMessagePlaintext(text))
		if err != nil {
			t.Fatal(err)
		}
		plain, err := DecryptNativeSignalPayloadByType(pair[1], raw, kind, pair[0].RegistrationJID, true)
		if err != nil || NativePlaintextText(plain.Plaintext) != text {
			t.Fatalf("duplex decrypt failed: %v", err)
		}
	}
}

func TestRetryReceiptPrekeysAreSignedAndNeverIncludePrivateOrCompanionMaterial(t *testing.T) {
	current := newMessageParty(t)
	before := current.Signal.Identity
	payload := state.NativeMessagePayload{Contact: "12025550123@s.whatsapp.net", Sender: "12025550123@s.whatsapp.net", ProviderMessageID: "fixture", ProviderTimestamp: time.Now().Unix()}
	receipt, err := buildSignalRetryReceipt(&current, payload, 2)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := preKeyBundleFromNode(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.RegistrationID() != uint32(current.Signal.RegistrationID) || current.Signal.Identity != before {
		t.Fatal("retry registration or own identity changed")
	}
	keys, _ := ChatdChild(receipt, "keys")
	if _, exists := ChatdChild(keys, "device-identity"); exists {
		t.Fatal("primary client invented companion identity")
	}
	encoded, err := wire.NewBinaryNodeCodec().EncodeNode(receipt)
	if err != nil {
		t.Fatal(err)
	}
	private, err := current.Signal.Identity.PrivateBytes()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, private) {
		t.Fatal("retry receipt exposed a private key")
	}
}

func TestSignalRetryCompletesOnlyAfterItsVisibleMessageArrives(t *testing.T) {
	for _, test := range []struct {
		name     string
		message  *waE2E.Message
		complete bool
	}{
		{"sender_key_only", &waE2E.Message{SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{}}, false},
		{"body", &waE2E.Message{Conversation: proto.String("recovered")}, true},
		{"sender_key_and_media", &waE2E.Message{SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{}, ImageMessage: &waE2E.ImageMessage{}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := newMessageParty(t)
			current.EnsureMaps()
			payload := stateMessagePayload("12025550124@s.whatsapp.net", "12025550124@s.whatsapp.net", "fixture")
			key := protocolRetryKey(&current, payload.Contact, payload.Sender, payload.ProviderMessageID)
			current.SignalRetries[key] = &protocolv1.SignalRetryState{RequestedCount: 2, PendingReceipt: []byte("pending")}
			raw, err := proto.Marshal(test.message)
			if err != nil {
				t.Fatal(err)
			}
			completeSignalRetry(&current, payload, raw)
			current = reloadMessageParty(t, current)
			record := current.SignalRetries[key]
			if record.GetCompleted() != test.complete || record.GetRequestedCount() != 2 || test.complete && len(record.GetPendingReceipt()) != 0 {
				t.Fatal("retry termination lost its body or count semantics")
			}
		})
	}
}
