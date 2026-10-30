package communication

import (
	"bytes"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestMessageProjectionCanRetryAfterSignalCommit(t *testing.T) {
	current := newMessageParty(t)
	frame := signalConversationFixture(t, current, nil, [][]byte{NativeTextMessagePlaintext("durable 中文🙂")})[0]
	message := wacore.EngineMessageInput{WAAccountID: "synthetic-account", ClientProfileID: "synthetic-profile"}
	ApplyChatdReceiveState(&current, message, []wire.ChatdEncPayload{{Payload: frame.preKey, EncType: "pkmsg", Sender: "synthetic-peer"}}, ChatdSessionUpdate{})
	repository := &serializedMessageState{}
	engine, err := New(repository, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.CloseIdleConnections()
	if err := engine.SaveState(t.Context(), message.ClientProfileID, current); err != nil {
		t.Fatal(err)
	}
	input := wacore.EngineDecryptInput{MessageID: "stored-message", ClientProfileID: message.ClientProfileID,
		PayloadRef: wire.PayloadRefForEnc(message.WAAccountID, frame.preKey), IncludePlaintextText: true,
		SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE}
	first := engine.DecryptMessage(t.Context(), input)
	if first.Err != nil {
		t.Fatal(first.Err)
	}
	before := bytes.Clone(repository.recovery)
	// The application projection may fail after the protocol state has committed.
	restarted, err := New(repository, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.CloseIdleConnections()
	retry := restarted.DecryptMessage(t.Context(), input)
	if retry.Err != nil || retry.DecryptedMessage.GetPlaintextText().GetValue() != "durable 中文🙂" || retry.DecryptedMessage.GetPlaintextRef() != first.DecryptedMessage.GetPlaintextRef() || !bytes.Equal(before, repository.recovery) {
		t.Fatal("projection retry lost plaintext or consumed a second message key")
	}
	input.MessageID = "another-message"
	if replay := restarted.DecryptMessage(t.Context(), input); replay.Err == nil {
		t.Fatal("another message reused committed plaintext or a consumed Signal key")
	}
}
