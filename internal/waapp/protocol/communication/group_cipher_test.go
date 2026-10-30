package communication

import (
	"bytes"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestGroupCipherPersistsDistributionAndOutOfOrderMessages(t *testing.T) {
	alice, bob := newMessageParty(t), newMessageParty(t)
	group, sender := "123456789@g.us", "12025550123:3@lid"
	name, err := groupSenderKeyName(group, sender)
	if err != nil {
		t.Fatal(err)
	}
	builder, cipher := newGroupCrypto(&alice, name, true)
	distribution, err := builder.Create(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	body, err := proto.Marshal(&waE2E.Message{SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{GroupID: proto.String(group), AxolotlSenderKeyDistributionMessage: distribution.Serialize()}})
	if err != nil {
		t.Fatal(err)
	}
	if NativePlaintextText(body) != "" {
		t.Fatal("sender-key material was displayed as a user message")
	}
	if err := applySignalControlMessage(t.Context(), &bob, sender, padSignalMessage(body)); err != nil {
		t.Fatal(err)
	}
	frames := [][]byte{}
	for _, text := range []string{"first", "late", "ahead"} {
		message, err := cipher.Encrypt(t.Context(), padSignalMessage(NativeTextMessagePlaintext(text)))
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, message.SignedSerialize())
	}
	alice, bob = reloadMessageParty(t, alice), reloadMessageParty(t, bob)
	for _, i := range []int{0, 2, 1} {
		payload := state.NativeMessagePayload{Contact: group, Sender: sender, EncType: "skmsg", Payload: wire.B64u(frames[i])}
		if i == 1 {
			before := bytes.Clone(bob.SenderKeyRecords[senderKeyStorageKey(name)])
			if _, err := DecryptNativeSignalPayload(&bob, payload, false); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, bob.SenderKeyRecords[senderKeyStorageKey(name)]) {
				t.Fatal("preview consumed a group message key")
			}
		}
		output, err := DecryptNativeSignalPayload(&bob, payload, true)
		if err != nil {
			t.Fatal(err)
		}
		if NativePlaintextText(output.Plaintext) != []string{"first", "late", "ahead"}[i] {
			t.Fatal("group plaintext changed after recovery")
		}
		if _, err := DecryptNativeSignalPayload(&bob, payload, true); err == nil {
			t.Fatal("consumed group message key accepted a replay")
		}
	}
}
