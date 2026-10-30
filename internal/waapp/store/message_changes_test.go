package store_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestMessageProjectionPersistsChangesBeforeTargetsAndDeduplicatesTransport(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, reopen := openRepository(t, backend)
			accountID, session := seedMessageProjection(t, repo)
			at := time.Now().UTC().Truncate(time.Millisecond)
			const chat = "12025550124@s.whatsapp.net"
			create := func(id, providerID, text string, second int, change *waappv1.MessageChange) *waappv1.InboundMessage {
				message := &waappv1.InboundMessage{MessageId: id, MessageSessionId: session.GetMessageSessionId(), ProviderMessageId: providerID, ContactRef: chat, SenderRef: chat, Kind: waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE, Direction: waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND, EncryptionState: waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTED, ReceivedAt: timestamppb.New(at.Add(time.Duration(second) * time.Second))}
				content := &waappv1.MessageContent{Type: waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT, Change: change}
				if change != nil {
					message.Kind = waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_SYSTEM
					content.Type = waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL
				}
				if err := repo.SaveInboundMessages(t.Context(), []*waappv1.InboundMessage{message}); err != nil {
					t.Fatal(err)
				}
				decrypted := &waappv1.DecryptedMessage{DecryptedMessageId: "decrypted-" + id, MessageId: id, Status: waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED, PlaintextText: &waappv1.SensitiveText{Value: text, RedactedValue: shared.Redacted(text)}, DecryptedAt: message.ReceivedAt, Content: content}
				if err := repo.SaveDecryptedMessage(t.Context(), decrypted); err != nil {
					t.Fatal(err)
				}
				return message
			}
			change := &waappv1.MessageChange{Type: waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_EDIT, TargetProviderMessageId: "target", TargetFromMe: true, Text: &waappv1.SensitiveText{Value: "edited secret", RedactedValue: "******"}, Timestamp: timestamppb.New(at.Add(time.Second))}
			create("edit", "edit-provider", "", 1, change)
			original := create("original", "target", "original secret", 2, nil)
			original.ReadAt = timestamppb.New(at.Add(3 * time.Second))
			if err := repo.SaveInboundMessages(t.Context(), []*waappv1.InboundMessage{original}); err != nil {
				t.Fatal(err)
			}
			create("retry", "target", "original secret", 4, nil)
			corrupt := proto.Clone(original).(*waappv1.InboundMessage)
			corrupt.MessageId = "corrupt"
			corrupt.EncryptionState = waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTION_FAILED
			corrupt.ReceivedAt = timestamppb.New(at.Add(5 * time.Second))
			corrupt.LastError = &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_DECRYPTION_FAILED, Retryable: true}
			if err := repo.SaveInboundMessages(t.Context(), []*waappv1.InboundMessage{corrupt}); err != nil {
				t.Fatal(err)
			}
			pending, err := repo.ListPendingEncryptedInboundMessages(t.Context(), accountID, session.GetClientProfileId(), 100)
			if err != nil || len(pending) != 0 {
				t.Fatal("recovered logical message retained a failed transport retry", err)
			}
			create("next", "different-provider", "next", 6, nil)
			create("edit-next", "edit-next-provider", "", 7, &waappv1.MessageChange{Type: waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_EDIT, TargetProviderMessageId: "different-provider", TargetFromMe: true, Text: &waappv1.SensitiveText{Value: "edited next"}, Timestamp: timestamppb.New(at.Add(7 * time.Second))})
			if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{{ContactId: "changes-contact", WaAccountId: accountID, Jid: chat, Audit: shared.NewAuditStamp(at, at)}}); err != nil {
				t.Fatal(err)
			}
			repo.Close()
			repo = reopen()
			app := account.New(repo, nil, nil, nil, nil, nil)
			first, _ := app.ListAccountMessages(t.Context(), &waappv1.ListAccountMessagesRequest{WaAccountId: accountID, ContactRef: chat, Limit: 1, IncludeSensitiveText: true})
			if first.GetError() != nil || len(first.GetMessages()) != 1 || first.GetMessages()[0].GetProviderMessageId() != "different-provider" || first.GetNextCursor() == "" {
				t.Fatalf("first page: %v", first.GetError())
			}
			second, _ := app.ListAccountMessages(t.Context(), &waappv1.ListAccountMessagesRequest{WaAccountId: accountID, ContactRef: chat, Limit: 1, Cursor: first.GetNextCursor(), IncludeSensitiveText: true})
			if second.GetError() != nil || len(second.GetMessages()) != 1 {
				t.Fatal("logical page missing", second.GetError())
			}
			message := second.GetMessages()[0]
			if second.GetNextCursor() != "" || message.GetAccountMessageId() != "original" || message.GetText().GetValue() != "edited secret" || message.GetEditedAt() == nil || message.GetReadAt() == nil {
				t.Fatal("reordered edit, duplicate transport or read facts were lost")
			}
			contacts, _ := app.ListWAContacts(t.Context(), &waappv1.ListWAContactsRequest{WaAccountId: accountID, Limit: 10})
			if contacts.GetError() != nil || len(contacts.GetContacts()) != 1 || contacts.GetContacts()[0].GetLastMessagePreview() != "edited next" || contacts.GetContacts()[0].GetMessageCount() != 2 || contacts.GetContacts()[0].GetUnreadCount() != 1 {
				t.Fatal("contact preview did not use the edited message", contacts.GetError())
			}
			redacted, _ := app.ListAccountMessages(t.Context(), &waappv1.ListAccountMessagesRequest{WaAccountId: accountID, ContactRef: chat, Limit: 10})
			for _, item := range redacted.GetMessages() {
				if item.GetText().GetValue() != "" || item.GetContent().GetQuote().GetText().GetValue() != "" {
					t.Fatal("sensitive projection leaked")
				}
			}
			latest, err := repo.GetLatestDecryptedMessage(t.Context(), "edit")
			if err != nil || latest.GetContent().GetChange().GetTargetProviderMessageId() != "target" {
				t.Fatal("restart lost typed change", err)
			}
			changes, err := repo.ListMessageChanges(t.Context(), "another-account", []string{chat}, []string{"target"}, true)
			if err != nil || len(changes) > 0 {
				t.Fatal("change lookup crossed accounts")
			}
			duplicate := proto.Clone(original).(*waappv1.InboundMessage)
			duplicate.EncryptionState = waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_ENCRYPTED
			duplicate.ReadAt = nil
			if err := repo.SaveInboundMessages(t.Context(), []*waappv1.InboundMessage{duplicate}); err != nil {
				t.Fatal(err)
			}
			stored, err := repo.GetInboundMessage(t.Context(), original.GetMessageId())
			if err != nil || stored.GetEncryptionState() != waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTED || stored.GetReadAt() == nil {
				t.Fatal("duplicate receive regressed authenticated state")
			}
		})
	}
}

func seedMessageProjection(t *testing.T, repo repository) (string, *waappv1.MessageSession) {
	t.Helper()
	accepted, err := account.New(repo, nil, nil, nil, nil, nil).AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "message-changes"}, Material: testMaterial(t)})
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
	session := &waappv1.MessageSession{MessageSessionId: "message-changes-session", WaAccountId: login.GetWaAccountId(), ClientProfileId: login.GetClientProfileId(), RegisteredIdentityId: login.GetRegisteredIdentityId(), ProtocolProfileId: profile.GetProtocolProfileId(), Status: waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_OPEN, OpenedAt: timestamppb.Now()}
	if err := repo.SaveMessageSession(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	return login.GetWaAccountId(), session
}
