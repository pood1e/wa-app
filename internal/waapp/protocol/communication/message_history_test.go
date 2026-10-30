package communication

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type historicalMessageState struct {
	states  map[string]*serializedMessageState
	retired []string
	fail    string
}

func (s *historicalMessageState) GetProtocolState(ctx context.Context, id string) (*protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState, error) {
	return s.states[id].GetProtocolState(ctx, id)
}

func (s *historicalMessageState) SaveProtocolState(ctx context.Context, id string, keys *protocolv1.AccountCredentials, recovery *protocolv1.AccountRecoveryState) error {
	if id == s.fail {
		return errors.New("synthetic state save failure")
	}
	if s.states[id] == nil {
		s.states[id] = new(serializedMessageState)
	}
	return s.states[id].SaveProtocolState(ctx, id, keys, recovery)
}

func (s *historicalMessageState) ListRetiredProtocolProfiles(context.Context, string) ([]string, error) {
	return s.retired, nil
}

func newHistoricalMessageEngine(t *testing.T, current, previous state.NativeState) (*Engine, *historicalMessageState) {
	t.Helper()
	repository := &historicalMessageState{states: map[string]*serializedMessageState{}, retired: []string{"unrelated", "previous"}}
	engine, err := New(repository, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.CloseIdleConnections)
	for id, material := range map[string]state.NativeState{"current": current, "previous": previous, "unrelated": newMessageParty(t)} {
		if err := engine.SaveState(t.Context(), id, material); err != nil {
			t.Fatal(err)
		}
	}
	return engine, repository
}

func TestDelayedMessageUsesRetiredRatchetWithoutChangingActiveKeys(t *testing.T) {
	current, previous := newMessageParty(t), newMessageParty(t)
	frame := signalConversationFixture(t, previous, nil, [][]byte{NativeTextMessagePlaintext("delayed message")})[0]
	input := wacore.EngineMessageInput{WAAccountID: "synthetic-account", ClientProfileID: "current"}
	ApplyChatdReceiveState(&current, input, []wire.ChatdEncPayload{{Payload: frame.preKey, EncType: "pkmsg", Sender: "synthetic-peer"}}, ChatdSessionUpdate{})
	engine, repository := newHistoricalMessageEngine(t, current, previous)
	request := wacore.EngineDecryptInput{MessageID: "delayed", ClientProfileID: "current", PayloadRef: wire.PayloadRefForEnc(input.WAAccountID, frame.preKey), SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_TRANSIENT, IncludePlaintextText: true}
	beforeCurrent, beforePrevious := bytes.Clone(repository.states["current"].recovery), bytes.Clone(repository.states["previous"].recovery)
	result := engine.DecryptMessage(t.Context(), request)
	if result.Err != nil || result.DecryptedMessage.GetPlaintextText().GetValue() != "delayed message" || !bytes.Equal(beforeCurrent, repository.states["current"].recovery) || !bytes.Equal(beforePrevious, repository.states["previous"].recovery) {
		t.Fatal("transient historical decryption changed stored state or lost plaintext")
	}
	activeKeys := bytes.Clone(repository.states["current"].credentials)
	request.SessionCommitPolicy = waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE
	repository.fail = "current"
	if result := engine.DecryptMessage(t.Context(), request); result.Err == nil || result.DecryptedMessage != nil {
		t.Fatal("decryption acknowledged an unpersisted active result")
	}
	repository.fail = ""
	for range 2 {
		result = engine.DecryptMessage(t.Context(), request)
		if result.Err != nil || result.DecryptedMessage.GetPlaintextText().GetValue() != "delayed message" {
			t.Fatal("retry lost the authenticated plaintext after the old ratchet advanced")
		}
	}
	restored, err := engine.loadState(t.Context(), "current")
	if err != nil || len(restored.Signal.Sessions) != 0 || !bytes.Equal(activeKeys, repository.states["current"].credentials) {
		t.Fatal("historical decryption overwrote the active identity or ratchet")
	}
}

func TestDelayedGroupDistributionRestoresOnlyItsOriginalIdentity(t *testing.T) {
	current, previous, sender := newMessageParty(t), newMessageParty(t), newMessageParty(t)
	const group, peer = "123456789@g.us", "12025550123:3@lid"
	name, err := groupSenderKeyName(group, peer)
	if err != nil {
		t.Fatal(err)
	}
	builder, cipher := newGroupCrypto(&sender, name, true)
	distribution, err := builder.Create(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	body, err := proto.Marshal(&waE2E.Message{SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{GroupID: proto.String(group), AxolotlSenderKeyDistributionMessage: distribution.Serialize()}})
	if err != nil {
		t.Fatal(err)
	}
	prekey := signalConversationFixture(t, previous, nil, [][]byte{body})[0].preKey
	encrypted, err := cipher.Encrypt(t.Context(), padSignalMessage(NativeTextMessagePlaintext("delayed group message")))
	if err != nil {
		t.Fatal(err)
	}
	groupPayload := encrypted.SignedSerialize()
	input := wacore.EngineMessageInput{WAAccountID: "synthetic-account", ClientProfileID: "current"}
	ApplyChatdReceiveState(&current, input, []wire.ChatdEncPayload{
		{Payload: prekey, Contact: group, Sender: peer, EncType: "pkmsg"},
		{Payload: groupPayload, Contact: group, Sender: peer, EncType: "skmsg"},
	}, ChatdSessionUpdate{})
	engine, _ := newHistoricalMessageEngine(t, current, previous)
	request := wacore.EngineDecryptInput{MessageID: "group", ClientProfileID: "current", PayloadRef: wire.PayloadRefForEnc(input.WAAccountID, groupPayload), SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE, IncludePlaintextText: true}
	if result := engine.DecryptMessage(t.Context(), request); !retryableDecryptionFailure(result.Err) {
		t.Fatal("group message did not wait for its sender key")
	}
	control := request
	control.MessageID, control.PayloadRef = "distribution", wire.PayloadRefForEnc(input.WAAccountID, prekey)
	if result := engine.DecryptMessage(t.Context(), control); result.Err != nil || result.MessageKind != waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_SYSTEM || result.DecryptedMessage.GetPlaintextText().GetValue() != "" {
		t.Fatal("sender-key distribution was not processed as a control message")
	}
	if result := engine.DecryptMessage(t.Context(), request); result.Err != nil || result.DecryptedMessage.GetPlaintextText().GetValue() != "delayed group message" {
		t.Fatal("stored historical sender key did not decrypt the delayed group message")
	}
	restored, err := engine.loadState(t.Context(), "current")
	if err != nil || len(restored.Signal.Sessions) != 0 || len(restored.SenderKeyRecords) != 0 {
		t.Fatal("historical control message copied keys into the active identity")
	}
}
