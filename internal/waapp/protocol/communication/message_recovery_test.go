package communication

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type serializedMessageState struct {
	credentials, recovery []byte
	saveErr               error
}

func (*serializedMessageState) ListRetiredProtocolProfiles(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *serializedMessageState) SaveProtocolState(_ context.Context, _ string, credentials *protocolv1.AccountCredentials, recovery *protocolv1.AccountRecoveryState) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	var err error
	s.credentials, err = proto.Marshal(credentials)
	if err != nil {
		return err
	}
	s.recovery, err = proto.Marshal(recovery)
	return err
}

func TestDecryptionDoesNotAcknowledgeUnpersistedState(t *testing.T) {
	parameters := protocoltest.AndroidClient()
	current, err := state.NewNativeState(&waappv1.PhoneTarget{CountryCallingCode: "1", NationalNumber: "2025550123"}, parameters.GetMetadata(), parameters.GetMemoryGib())
	if err != nil {
		t.Fatal(err)
	}
	frame := signalConversationFixture(t, current, nil, [][]byte{NativeTextMessagePlaintext("synthetic")})[0]
	input := wacore.EngineMessageInput{WAAccountID: "synthetic-account", ClientProfileID: "synthetic-profile"}
	ApplyChatdReceiveState(&current, input, []wire.ChatdEncPayload{{Payload: frame.preKey, EncType: "pkmsg", Sender: "synthetic-peer"}}, ChatdSessionUpdate{})
	repository := &serializedMessageState{}
	engine, err := New(repository, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.CloseIdleConnections()
	if err := engine.SaveState(t.Context(), input.ClientProfileID, current); err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(repository.recovery)
	repository.saveErr = errors.New("synthetic storage failure")
	result := engine.DecryptMessage(t.Context(), wacore.EngineDecryptInput{ClientProfileID: input.ClientProfileID, PayloadRef: wire.PayloadRefForEnc(input.WAAccountID, frame.preKey), SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE})
	if !errors.Is(result.Err, repository.saveErr) || result.DecryptedMessage != nil || !bytes.Equal(before, repository.recovery) {
		t.Fatal("decryption acknowledged a session transition that was not persisted")
	}
}

func (s *serializedMessageState) GetProtocolState(context.Context, string) (*protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState, error) {
	credentials, recovery := &protocolv1.AccountCredentials{}, &protocolv1.AccountRecoveryState{}
	if err := proto.Unmarshal(s.credentials, credentials); err != nil {
		return nil, nil, err
	}
	return credentials, recovery, proto.Unmarshal(s.recovery, recovery)
}

func TestReceivedMessagesDecryptAfterStateReload(t *testing.T) {
	for _, encType := range []string{"msg", "pkmsg"} {
		t.Run(encType, func(t *testing.T) {
			parameters := protocoltest.AndroidClient()
			current, err := state.NewNativeState(&waappv1.PhoneTarget{CountryCallingCode: "1", NationalNumber: "2025550123"}, parameters.GetMetadata(), parameters.GetMemoryGib())
			if err != nil {
				t.Fatal(err)
			}
			frames := signalConversationFixture(t, current, nil, [][]byte{NativeTextMessagePlaintext("first"), NativeTextMessagePlaintext("late"), NativeTextMessagePlaintext("ahead")})
			repository := &serializedMessageState{}
			engine, err := New(repository, "", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer engine.CloseIdleConnections()
			input := wacore.EngineMessageInput{WAAccountID: "synthetic-account", ClientProfileID: "synthetic-profile"}
			for index, frame := range frames {
				payload, kind := frame.preKey, "pkmsg"
				if index > 0 && encType == "msg" {
					payload, kind = frame.message, "msg"
				}
				ApplyChatdReceiveState(&current, input, []wire.ChatdEncPayload{{Payload: payload, EncType: kind, Sender: "synthetic-peer"}}, ChatdSessionUpdate{})
			}
			if err := engine.SaveState(t.Context(), input.ClientProfileID, current); err != nil {
				t.Fatal(err)
			}
			for _, index := range []int{0, 2, 1} {
				payload := frames[index].preKey
				if index > 0 && encType == "msg" {
					payload = frames[index].message
				}
				if index == 1 {
					before := bytes.Clone(repository.recovery)
					preview := engine.DecryptMessage(t.Context(), wacore.EngineDecryptInput{ClientProfileID: input.ClientProfileID, PayloadRef: wire.PayloadRefForEnc(input.WAAccountID, payload), SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_TRANSIENT, IncludePlaintextText: true})
					if preview.Err != nil || preview.DecryptedMessage.GetPlaintextText().GetValue() != "late" || !bytes.Equal(before, repository.recovery) {
						t.Fatal("preview consumed or failed to read a skipped message key")
					}
					stored, err := engine.loadState(t.Context(), input.ClientProfileID)
					if err != nil {
						t.Fatal(err)
					}
					corrupt := bytes.Clone(frames[index].message)
					corrupt[len(corrupt)-1] ^= 1
					ApplyChatdReceiveState(&stored, input, []wire.ChatdEncPayload{{Payload: corrupt, EncType: "msg", Sender: "synthetic-peer"}}, ChatdSessionUpdate{})
					if err := engine.SaveState(t.Context(), input.ClientProfileID, stored); err != nil {
						t.Fatal(err)
					}
					before = bytes.Clone(repository.recovery)
					failed := engine.DecryptMessage(t.Context(), wacore.EngineDecryptInput{ClientProfileID: input.ClientProfileID, PayloadRef: wire.PayloadRefForEnc(input.WAAccountID, corrupt), SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE})
					if failed.Err == nil || !bytes.Equal(before, repository.recovery) {
						t.Fatal("unauthenticated message consumed a skipped key")
					}
				}
				result := engine.DecryptMessage(t.Context(), wacore.EngineDecryptInput{
					MessageID: "synthetic-message", ClientProfileID: input.ClientProfileID, PayloadRef: wire.PayloadRefForEnc(input.WAAccountID, payload),
					SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE, IncludePlaintextText: true,
				})
				if result.Err != nil {
					t.Fatalf("decrypt message counter %d after state reload: %v", index, result.Err)
				}
				want := []string{"first", "late", "ahead"}[index]
				if got := result.DecryptedMessage.GetPlaintextText().GetValue(); got != want {
					t.Fatalf("decrypted text = %q, want %q", got, want)
				}
				if index == 1 {
					replay := engine.DecryptMessage(t.Context(), wacore.EngineDecryptInput{ClientProfileID: input.ClientProfileID, PayloadRef: wire.PayloadRefForEnc(input.WAAccountID, payload), SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE})
					if replay.Err == nil {
						t.Fatal("consumed message key accepted a replay")
					}
				}
			}
		})
	}
}
