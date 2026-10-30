package communication

import (
	"bytes"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestChangedSenderRequiresVerifiedIdentityAndAuthenticatedCiphertext(t *testing.T) {
	oldSender, newSender, receiver := newMessageParty(t), newMessageParty(t), newMessageParty(t)
	receiverBundle, err := preKeyBundleFromNode(messagePartyBundle(t, receiver, false))
	if err != nil {
		t.Fatal(err)
	}
	for _, sender := range []*state.NativeState{&oldSender, &newSender} {
		if err := initializeOutboundSession(sender, "receiver", receiverBundle); err != nil {
			t.Fatal(err)
		}
	}
	previous, kind, err := encryptDirectSignalPayload(&oldSender, "receiver", NativeTextMessagePlaintext("previous"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptNativeSignalPayloadByType(&receiver, previous, kind, "sender", true); err != nil {
		t.Fatal(err)
	}
	raw, _, err := encryptDirectSignalPayload(&newSender, "receiver", NativeTextMessagePlaintext("after identity change"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParsePreKeySignalMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	engine, repository := newHistoricalMessageEngine(t, receiver, newMessageParty(t))
	beforeKeys := bytes.Clone(repository.states["current"].credentials)
	beforeState := bytes.Clone(repository.states["current"].recovery)
	input := wacore.EngineDecryptInput{ClientProfileID: "current", MessageID: "fixture", SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE, IncludePlaintextText: true}
	payload := state.NativeMessagePayload{Sender: "sender", EncType: "pkmsg", Payload: wire.B64u(raw)}
	wrongBundle, err := preKeyBundleFromNode(messagePartyBundle(t, oldSender, false))
	if err != nil {
		t.Fatal(err)
	}
	current, err := engine.loadState(t.Context(), "current")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.decryptWithVerifiedIdentity(t.Context(), input, &current, payload, wrongBundle, parsed.IdentityKey); err == nil {
		t.Fatal("message accepted an identity that disagrees with the verified peer bundle")
	}
	bundle, err := preKeyBundleFromNode(messagePartyBundle(t, newSender, false))
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Clone(raw)
	tampered[len(tampered)-1] ^= 1
	payload.Payload = wire.B64u(tampered)
	if err := engine.decryptWithVerifiedIdentity(t.Context(), input, &current, payload, bundle, parsed.IdentityKey); err == nil {
		t.Fatal("unauthenticated ciphertext replaced the trusted identity")
	}
	if !bytes.Equal(repository.states["current"].recovery, beforeState) {
		t.Fatal("failed identity refresh modified persisted sessions")
	}
	current, err = engine.loadState(t.Context(), "current")
	if err != nil {
		t.Fatal(err)
	}
	payload.Payload = wire.B64u(raw)
	if err := engine.decryptWithVerifiedIdentity(t.Context(), input, &current, payload, bundle, parsed.IdentityKey); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(repository.states["current"].credentials, beforeKeys) {
		t.Fatal("refreshing a peer changed the local account credentials")
	}
	if err := validateRemoteSignalIdentity(&current, "sender", parsed.IdentityKey); err != nil {
		t.Fatal("verified sender identity was not retained")
	}
}
