package store_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func TestRuntimeProjectionsAndAccountDeletion(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, _ := openRepository(t, backend)
			response, err := account.New(repo, nil, nil, nil, nil, nil).AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "projections"}, Material: testMaterial(t)})
			if err != nil || response.GetError() != nil {
				t.Fatal("seed account failed")
			}
			id := response.GetAccount().GetWaAccountId()
			login, err := repo.GetActiveLoginState(t.Context(), id, "")
			if err != nil {
				t.Fatal(err)
			}
			profile, err := repo.GetClientProfile(t.Context(), login.GetClientProfileId())
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			stamp := timestamppb.New(now)
			current := &waappv1.MessageSession{MessageSessionId: "fixture-session", WaAccountId: id, ClientProfileId: login.GetClientProfileId(), RegisteredIdentityId: login.GetRegisteredIdentityId(), ProtocolProfileId: profile.GetProtocolProfileId(), Status: waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_OPEN, OpenedAt: stamp, LastSeenAt: stamp}
			if err := repo.SaveMessageSession(t.Context(), current); err != nil {
				t.Fatal(err)
			}
			contact := &waappv1.WAContact{ContactId: "fixture-contact", WaAccountId: id, Jid: "12025550124@s.whatsapp.net", DisplayName: "fixture", Kind: waappv1.WAContactKind_WA_CONTACT_KIND_USER, Audit: shared.NewAuditStamp(now, now)}
			if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{contact}); err != nil {
				t.Fatal(err)
			}
			incoming := &waappv1.InboundMessage{MessageId: "fixture-message", MessageSessionId: current.GetMessageSessionId(), ProviderMessageId: "fixture-upstream-id", Kind: waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE, EncryptionState: waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTED, SenderRef: contact.GetJid(), ContactRef: contact.GetJid(), PayloadRef: "plaintext:fixture", ReceivedAt: stamp}
			incoming.ReadAt = stamp
			if err := repo.SaveInboundMessages(t.Context(), []*waappv1.InboundMessage{incoming}); err != nil {
				t.Fatal(err)
			}
			pending, err := repo.ListPendingReadMessagesByContactRefs(t.Context(), id, []string{contact.GetJid()}, true, 10)
			if err != nil || len(pending) != 1 {
				t.Fatal("local read prevented a failed remote receipt from being retried", err)
			}
			delivery, err := repo.ListPendingDeliveryReceipts(t.Context(), id, 10)
			if err != nil || len(delivery) != 1 {
				t.Fatal("a decrypted message lost its pending delivery receipt", err)
			}
			incoming.DeliveryReceiptSentAt = stamp
			pending, err = repo.ListPendingReadMessagesByContactRefs(t.Context(), id, []string{contact.GetJid()}, false, 10)
			if err != nil || len(pending) != 0 {
				t.Fatal("local-only read selected already read messages", err)
			}
			incoming.ReadReceiptSentAt = stamp
			if err := repo.SaveInboundMessages(t.Context(), []*waappv1.InboundMessage{incoming}); err != nil {
				t.Fatal(err)
			}
			delivery, err = repo.ListPendingDeliveryReceipts(t.Context(), id, 10)
			if err != nil || len(delivery) != 0 {
				t.Fatal("a sent delivery receipt remained pending", err)
			}
			stored, err := repo.GetInboundMessage(t.Context(), incoming.GetMessageId())
			if err != nil || stored.GetDeliveryReceiptSentAt() == nil {
				t.Fatal("delivery receipt dispatch was not persisted", err)
			}
			pending, err = repo.ListPendingReadMessagesByContactRefs(t.Context(), id, []string{contact.GetJid()}, true, 10)
			if err != nil || len(pending) != 0 {
				t.Fatal("a sent read receipt remained pending", err)
			}
			decrypted := &waappv1.DecryptedMessage{DecryptedMessageId: "fixture-decrypted", MessageId: incoming.GetMessageId(), Status: waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED, PlaintextText: &waappv1.SensitiveText{Value: "fixture text", RedactedValue: "fixture"}, DecryptedAt: stamp}
			if err := repo.SaveDecryptedMessage(t.Context(), decrypted); err != nil {
				t.Fatal(err)
			}
			candidate := &waappv1.ExtractedCandidate{CandidateId: "fixture-candidate", MessageId: incoming.GetMessageId(), DecryptedMessageId: decrypted.GetDecryptedMessageId(), Kind: waappv1.CandidateKind_CANDIDATE_KIND_OTP, Text: &waappv1.SensitiveText{Value: "000000", RedactedValue: "******"}, ExtractedAt: stamp}
			if err := repo.SaveCandidates(t.Context(), []*waappv1.ExtractedCandidate{candidate}); err != nil {
				t.Fatal(err)
			}
			otp := &waappv1.OtpMessage{OtpMessageId: "fixture-otp", WaAccountId: id, ClientProfileId: login.GetClientProfileId(), MessageId: incoming.GetMessageId(), CandidateId: candidate.GetCandidateId(), Otp: candidate.GetText(), ReceivedAt: stamp, Audit: shared.NewAuditStamp(now, now)}
			if err := repo.SaveOTPMessage(t.Context(), otp); err != nil {
				t.Fatal(err)
			}
			messages, _, err := repo.ListAccountMessages(t.Context(), id, []string{contact.GetJid()}, "", 10, true)
			if err != nil || len(messages) != 1 {
				t.Fatal("message projection failed", err)
			}
			if messages[0].GetProviderMessageId() != incoming.GetProviderMessageId() {
				t.Fatal("message projection lost the upstream correlation ID")
			}
			if messages[0].GetReadReceiptSentAt() == nil || !messages[0].GetReadReceiptSentAt().AsTime().Equal(stamp.AsTime()) {
				t.Fatal("message projection lost its sent read receipt timestamp")
			}
			contacts, _, err := repo.ListWAContacts(t.Context(), id, "", 10)
			if err != nil || len(contacts) != 1 {
				t.Fatal("contact projection failed", err)
			}
			otps, _, err := repo.ListAccountOTPMessages(t.Context(), id, "", 10, false)
			if err != nil || len(otps) != 1 || otps[0].GetOtp().GetValue() != "" {
				t.Fatal("OTP projection did not respect redaction", err)
			}
			if err := repo.DeleteWAAccount(t.Context(), id); err != nil {
				t.Fatal("account deletion failed", err)
			}
			if _, err := repo.GetWAAccount(t.Context(), id); !wamodel.IsWAAccountNotFound(err) {
				t.Fatal("deleted account remained visible")
			}
			if _, _, err := repo.GetProtocolState(t.Context(), login.GetClientProfileId()); err == nil {
				t.Fatal("account deletion retained private protocol state")
			}
			if err := repo.SaveMessageSession(t.Context(), current); err == nil {
				t.Fatal("late session write recreated deleted account data")
			}
			if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{contact}); err == nil {
				t.Fatal("late contact write recreated deleted account data")
			}
		})
	}
}
