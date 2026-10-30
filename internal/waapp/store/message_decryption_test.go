package store_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type messageDecryptor struct {
	wacore.Communication
	result wacore.EngineDecryptResult
}

func (d messageDecryptor) DecryptMessage(context.Context, wacore.EngineDecryptInput) wacore.EngineDecryptResult {
	return d.result
}

func TestDecryptionFailurePersistsAndRetriesAfterLearningKeys(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, _ := openRepository(t, backend)
			app := account.New(repo, nil, nil, nil, nil, nil)
			accepted, err := app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "decryption"}, Material: testMaterial(t)})
			if err != nil || accepted.GetError() != nil {
				t.Fatal("seed account failed")
			}
			login, err := repo.GetActiveLoginState(t.Context(), accepted.GetAccount().GetWaAccountId(), "")
			if err != nil {
				t.Fatal(err)
			}
			profile, err := repo.GetClientProfile(t.Context(), login.GetClientProfileId())
			if err != nil {
				t.Fatal(err)
			}
			stamp := timestamppb.New(time.Now())
			session := &waappv1.MessageSession{MessageSessionId: "decryption-session", WaAccountId: login.GetWaAccountId(), ClientProfileId: login.GetClientProfileId(), RegisteredIdentityId: login.GetRegisteredIdentityId(), ProtocolProfileId: profile.GetProtocolProfileId(), Status: waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_OPEN, OpenedAt: stamp}
			if err := repo.SaveMessageSession(t.Context(), session); err != nil {
				t.Fatal(err)
			}
			message := &waappv1.InboundMessage{MessageId: "decryption-message", MessageSessionId: session.GetMessageSessionId(), Kind: waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE, EncryptionState: waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_ENCRYPTED, Direction: waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND, ContactRef: "12025550124@s.whatsapp.net", PayloadRef: "native-enc:synthetic", ReceivedAt: stamp}
			if err := repo.SaveInboundMessages(t.Context(), []*waappv1.InboundMessage{message}); err != nil {
				t.Fatal(err)
			}
			request := &waappv1.DecryptMessageRequest{MessageId: message.GetMessageId(), SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE}
			for _, retryable := range []bool{false, true} {
				decryptor := messageDecryptor{result: wacore.EngineDecryptResult{Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_DECRYPTION_FAILED, "synthetic decryption failure", retryable)}}
				response, err := app.DecryptWithProtocol(t.Context(), request, decryptor, waappv1.WaOtpSource_WA_OTP_SOURCE_LONG_CONNECTION)
				if err != nil || response.GetError() == nil {
					t.Fatal("decryption failure was not returned")
				}
				stored, err := repo.GetInboundMessage(t.Context(), message.GetMessageId())
				if err != nil || stored.GetEncryptionState() != waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTION_FAILED || stored.GetLastError().GetRetryable() != retryable {
					t.Fatal("failed decryption remained pending or lost retry semantics")
				}
				pending, err := repo.ListPendingEncryptedInboundMessages(t.Context(), login.GetWaAccountId(), login.GetClientProfileId(), 10)
				if err != nil || (len(pending) == 1) != retryable {
					t.Fatal("retry queue included a permanent failure or lost a recoverable message")
				}
			}
			decryptor := messageDecryptor{result: wacore.EngineDecryptResult{
				DecryptedMessage: &waappv1.DecryptedMessage{DecryptedMessageId: "decrypted-control", MessageId: message.GetMessageId(), Status: waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED, DecryptedAt: stamp},
				MessageKind:      waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_SYSTEM,
			}}
			response, err := app.DecryptWithProtocol(t.Context(), request, decryptor, waappv1.WaOtpSource_WA_OTP_SOURCE_LONG_CONNECTION)
			if err != nil || response.GetError() != nil {
				t.Fatal("retry with learned keys failed")
			}
			stored, err := repo.GetInboundMessage(t.Context(), message.GetMessageId())
			if err != nil || stored.GetEncryptionState() != waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTED || stored.GetKind() != waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_SYSTEM || stored.GetLastError() != nil {
				t.Fatal("successful decryption retained an error or lost the control message kind")
			}
			pending, err := repo.ListPendingEncryptedInboundMessages(t.Context(), login.GetWaAccountId(), login.GetClientProfileId(), 10)
			if err != nil || len(pending) != 0 {
				t.Fatal("decrypted message remained in the retry queue")
			}
			visible, _, err := repo.ListAccountMessages(t.Context(), login.GetWaAccountId(), []string{message.GetContactRef()}, "", 10, true)
			if err != nil || len(visible) != 0 {
				t.Fatal("sender-key control traffic was exposed as a chat message")
			}
		})
	}
}
