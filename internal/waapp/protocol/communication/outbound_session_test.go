package communication

import (
	"bytes"
	"encoding/binary"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestOutboundSessionExchangesMessagesAcrossRecovery(t *testing.T) {
	for _, oneTime := range []bool{false, true} {
		name := "signed prekey"
		if oneTime {
			name = "one-time prekey"
		}
		t.Run(name, func(t *testing.T) {
			alice, bob := newMessageParty(t), newMessageParty(t)
			bundle, err := preKeyBundleFromNode(messagePartyBundle(t, bob, oneTime))
			if err != nil {
				t.Fatal(err)
			}
			if err := initializeOutboundSession(&alice, "bob", bundle); err != nil {
				t.Fatal(err)
			}
			exchange := func(sender, receiver *state.NativeState, target, source, text, wantKind string) {
				t.Helper()
				payload, kind, err := encryptDirectSignalPayload(sender, target, NativeTextMessagePlaintext(text))
				if err != nil {
					t.Fatal(err)
				}
				if kind != wantKind {
					t.Fatalf("envelope = %s, want %s", kind, wantKind)
				}
				output, err := DecryptNativeSignalPayloadByType(receiver, payload, kind, source, true)
				if err != nil {
					t.Fatal(err)
				}
				if got := NativePlaintextText(output.Plaintext); got != text {
					t.Fatalf("received %q, want %q", got, text)
				}
			}
			exchange(&alice, &bob, "bob", "alice", "首条消息🙂\nline two", "pkmsg")
			alice, bob = reloadMessageParty(t, alice), reloadMessageParty(t, bob)
			exchange(&alice, &bob, "bob", "alice", "before reply", "pkmsg")
			exchange(&bob, &alice, "alice", "bob", "reply from a new ratchet", "msg")
			alice, bob = reloadMessageParty(t, alice), reloadMessageParty(t, bob)
			if alice.Signal.Sessions[SignalSenderKey("bob")].PendingPreKey != nil {
				t.Fatal("authenticated reply did not confirm the outgoing prekey")
			}
			exchange(&alice, &bob, "bob", "alice", "after reply", "msg")
			exchange(&bob, &alice, "alice", "bob", "second reply", "msg")
		})
	}
}

func TestPeerPrekeySignatureMustAuthenticateBeforeSessionCreation(t *testing.T) {
	peer := newMessageParty(t)
	for _, field := range []string{"value", "signature"} {
		node := messagePartyBundle(t, peer, true)
		signed, _ := ChatdChild(node, "skey")
		child, _ := ChatdChild(signed, field)
		child.Content.([]byte)[0] ^= 1
		if _, err := preKeyBundleFromNode(node); err == nil {
			t.Fatalf("unauthenticated signed prekey %s was accepted", field)
		}
	}
}

func TestOutboundSessionPreservesTrustedIdentity(t *testing.T) {
	alice, bob := newMessageParty(t), newMessageParty(t)
	bundle, err := preKeyBundleFromNode(messagePartyBundle(t, bob, false))
	if err != nil {
		t.Fatal(err)
	}
	alice.Signal.RemoteIdentities[SignalSenderKey("bob")] = wire.B64u(bytes.Repeat([]byte{1}, 32))
	if err := initializeOutboundSession(&alice, "bob", bundle); err == nil {
		t.Fatal("changed peer identity was silently trusted")
	}
	if len(alice.Signal.Sessions) != 0 {
		t.Fatal("rejected peer changed session state")
	}
}

func newMessageParty(t *testing.T) state.NativeState {
	t.Helper()
	parameters := protocoltest.AndroidClient()
	current, err := state.NewNativeState(&waappv1.PhoneTarget{CountryCallingCode: "1", NationalNumber: "2025550123"}, parameters.GetMetadata(), parameters.GetMemoryGib())
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func reloadMessageParty(t *testing.T, current state.NativeState) state.NativeState {
	t.Helper()
	credentials, recovery, err := current.Export()
	if err != nil {
		t.Fatal(err)
	}
	next, err := state.Restore(credentials, recovery)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func messagePartyBundle(t *testing.T, current state.NativeState, oneTime bool) wire.ChatdNode {
	t.Helper()
	public := func(pair wire.NativeCurveKeyPair) []byte {
		t.Helper()
		value, err := pair.PublicBytes()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	idBytes := func(id int32) []byte { return []byte{byte(id >> 16), byte(id >> 8), byte(id)} }
	private, err := current.Signal.Identity.PrivateBytes()
	if err != nil {
		t.Fatal(err)
	}
	signedPublic, err := wire.WithSignalCurvePrefix(public(current.Signal.SignedPreKey.KeyPair))
	if err != nil {
		t.Fatal(err)
	}
	signature, err := wire.XeddsaSignCurve25519(private, signedPublic)
	if err != nil {
		t.Fatal(err)
	}
	children := []wire.ChatdNode{{Tag: "registration", Content: binary.BigEndian.AppendUint32(nil, uint32(current.Signal.RegistrationID))},
		{Tag: "identity", Content: public(current.Signal.Identity)},
		{Tag: "skey", Content: []wire.ChatdNode{{Tag: "id", Content: idBytes(current.Signal.SignedPreKey.ID)}, {Tag: "value", Content: public(current.Signal.SignedPreKey.KeyPair)}, {Tag: "signature", Content: signature}}}}
	if oneTime {
		key := current.Signal.OneTimePreKeys[0]
		children = append(children, wire.ChatdNode{Tag: "key", Content: []wire.ChatdNode{{Tag: "id", Content: idBytes(key.ID)}, {Tag: "value", Content: public(key.KeyPair)}}})
	}
	return wire.ChatdNode{Tag: "user", Content: children}
}
