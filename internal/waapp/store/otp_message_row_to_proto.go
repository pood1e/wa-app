package store

import (
	"database/sql"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (r otpMessageRow) toProto(includeSensitiveValue bool) *waappv1.OtpMessage {
	text := &waappv1.SensitiveText{
		RedactedValue: r.otpRedacted,
		SecretRef:     r.otpSecretRef,
	}
	if includeSensitiveValue {
		text.Value = r.otpValue
	}
	return &waappv1.OtpMessage{
		OtpMessageId:         r.id,
		WaAccountId:          r.waAccountIDValue,
		ClientProfileId:      r.clientProfileID,
		RegisteredIdentityId: r.registeredIdentityID,
		MessageId:            r.messageID,
		CandidateId:          r.candidateID,
		Source:               waappv1.WaOtpSource(waappv1.WaOtpSource_value[r.source]),
		SourceParty:          r.sourceParty,
		Otp:                  text,
		ReceivedAt:           timestamppb.New(r.receivedAt.UTC()),
		ExpiresAt:            sqlTime(r.expiresAt),
		Audit:                shared.NewAuditStamp(r.createdAt, r.updatedAt),
	}
}

type contactRow struct {
	id               string
	waAccountIDValue string
	jid              string
	number           string
	displayName      string
	savedName        string
	waName           string
	verifiedName     string
	profilePictureID string
	kind             string
	isWhatsAppUser   bool
	isReachable      bool
	createdAt        time.Time
	updatedAt        time.Time
	messageCount     int64
	unreadCount      int64
	lastMessageAt    sql.NullTime
	lastPlaintext    string
	lastRedacted     string
	lastPayloadRef   string
	lastEncryption   string
}

func (r contactRow) toProto() *waappv1.WAContact {
	kind := waappv1.WAContactKind(waappv1.WAContactKind_value[r.kind])
	contact := &waappv1.WAContact{
		ContactId:        r.id,
		WaAccountId:      r.waAccountIDValue,
		Jid:              r.jid,
		Number:           r.number,
		DisplayName:      r.displayName,
		SavedName:        r.savedName,
		WaName:           r.waName,
		VerifiedName:     r.verifiedName,
		ProfilePictureId: r.profilePictureID,
		Kind:             kind,
		IsWhatsappUser:   r.isWhatsAppUser,
		IsReachable:      r.isReachable,
		Audit:            shared.NewAuditStamp(r.createdAt, r.updatedAt),
		MessageCount:     int32(r.messageCount),
		UnreadCount:      int32(r.unreadCount),
		LastMessageAt:    sqlTime(r.lastMessageAt),
		LastMessagePreview: wamodel.ContactMessagePreview(
			r.lastPlaintext,
			r.lastRedacted,
			r.lastPayloadRef,
			waappv1.MessageEncryptionState(waappv1.MessageEncryptionState_value[r.lastEncryption]),
		),
	}
	wamodel.EnrichWAContactFallback(contact)
	return contact
}
