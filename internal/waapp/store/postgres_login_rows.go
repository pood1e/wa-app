package store

import (
	"database/sql"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type loginStateRow struct {
	id                   string
	registrationID       string
	waAccountIDValue     string
	clientProfileID      string
	registeredIdentityID string
	serviceAccountID     string
	serviceLoginID       string
	status               string
	errCode              string
	errMessage           string
	errRetryable         bool
	registeredAt         time.Time
	lastVerifiedAt       sql.NullTime
	createdAt            time.Time
	updatedAt            time.Time
}

func (r loginStateRow) toProto() *waappv1.LoginState {
	return &waappv1.LoginState{
		LoginStateId:         r.id,
		RegistrationId:       r.registrationID,
		WaAccountId:          r.waAccountIDValue,
		ClientProfileId:      r.clientProfileID,
		RegisteredIdentityId: r.registeredIdentityID,
		ServiceAccountId:     r.serviceAccountID,
		ServiceLoginId:       r.serviceLoginID,
		Status:               waappv1.LoginStateStatus(waappv1.LoginStateStatus_value[r.status]),
		RegisteredAt:         timestamppb.New(r.registeredAt.UTC()),
		LastVerifiedAt:       sqlTime(r.lastVerifiedAt),
		Audit:                shared.NewAuditStamp(r.createdAt, r.updatedAt),
		LastError:            protoError(r.errCode, r.errMessage, r.errRetryable),
	}
}

type sessionRow struct {
	id                string
	waAccountIDValue  string
	clientProfileID   string
	identityID        string
	protocolProfileID string
	status            string
	errCode           string
	errMessage        string
	errRetryable      bool
	openedAt          time.Time
	lastSeenAt        sql.NullTime
	closedAt          sql.NullTime
}

func (r sessionRow) toProto() *waappv1.MessageSession {
	return &waappv1.MessageSession{
		MessageSessionId:     r.id,
		WaAccountId:          r.waAccountIDValue,
		ClientProfileId:      r.clientProfileID,
		RegisteredIdentityId: r.identityID,
		ProtocolProfileId:    r.protocolProfileID,
		Status:               waappv1.MessageSessionStatus(waappv1.MessageSessionStatus_value[r.status]),
		OpenedAt:             timestamppb.New(r.openedAt.UTC()),
		LastSeenAt:           sqlTime(r.lastSeenAt),
		ClosedAt:             sqlTime(r.closedAt),
		LastError:            protoError(r.errCode, r.errMessage, r.errRetryable),
	}
}

type messageRow struct {
	id                    string
	sessionID             string
	kind                  string
	encryptionState       string
	ackStatus             string
	direction             string
	source                string
	contactRef            string
	senderRef             string
	payloadRef            string
	providerMessageID     string
	providerTimestamp     sql.NullTime
	readAt                sql.NullTime
	readReceiptSentAt     sql.NullTime
	deliveryReceiptSentAt sql.NullTime
	deleteStatus          string
	deletedAt             sql.NullTime
	errCode               string
	errMessage            string
	errRetryable          bool
	receivedAt            time.Time
}

func (r messageRow) toProto() *waappv1.InboundMessage {
	return &waappv1.InboundMessage{
		MessageId:             r.id,
		MessageSessionId:      r.sessionID,
		Kind:                  waappv1.InboundMessageKind(waappv1.InboundMessageKind_value[r.kind]),
		EncryptionState:       waappv1.MessageEncryptionState(waappv1.MessageEncryptionState_value[r.encryptionState]),
		AckStatus:             waappv1.MessageAckStatus(waappv1.MessageAckStatus_value[r.ackStatus]),
		Direction:             messageDirection(r.direction),
		Source:                messageSource(r.source),
		ContactRef:            r.contactRef,
		SenderRef:             r.senderRef,
		PayloadRef:            r.payloadRef,
		ProviderMessageId:     r.providerMessageID,
		ProviderTimestamp:     sqlTime(r.providerTimestamp),
		ReadAt:                sqlTime(r.readAt),
		ReadReceiptSentAt:     sqlTime(r.readReceiptSentAt),
		DeliveryReceiptSentAt: sqlTime(r.deliveryReceiptSentAt),
		DeleteStatus:          messageDeleteStatus(r.deleteStatus),
		DeletedAt:             sqlTime(r.deletedAt),
		ReceivedAt:            timestamppb.New(r.receivedAt.UTC()),
		LastError:             protoError(r.errCode, r.errMessage, r.errRetryable),
	}
}

func scanPostgresInboundMessage(scanner interface{ Scan(...any) error }) (*waappv1.InboundMessage, error) {
	var r messageRow
	if err := scanner.Scan(&r.id, &r.sessionID, &r.kind, &r.encryptionState, &r.ackStatus, &r.direction, &r.source, &r.contactRef, &r.senderRef, &r.payloadRef, &r.providerMessageID, &r.providerTimestamp, &r.readAt, &r.deleteStatus, &r.deletedAt, &r.errCode, &r.errMessage, &r.errRetryable, &r.receivedAt, &r.readReceiptSentAt, &r.deliveryReceiptSentAt); err != nil {
		return nil, err
	}
	return r.toProto(), nil
}

func messageDirection(value string) waappv1.AccountMessageDirection {
	if value == "" {
		return waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND
	}
	status, ok := waappv1.AccountMessageDirection_value[value]
	if !ok {
		return waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_UNSPECIFIED
	}
	return waappv1.AccountMessageDirection(status)
}

func messageSource(value string) waappv1.AccountMessageSource {
	if value == "" {
		return waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_LONG_CONNECTION
	}
	status, ok := waappv1.AccountMessageSource_value[value]
	if !ok {
		return waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_UNSPECIFIED
	}
	return waappv1.AccountMessageSource(status)
}

func messageDeleteStatus(value string) waappv1.MessageDeleteStatus {
	if value == "" {
		return waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_NOT_DELETED
	}
	status, ok := waappv1.MessageDeleteStatus_value[value]
	if !ok {
		return waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_UNSPECIFIED
	}
	return waappv1.MessageDeleteStatus(status)
}

type decryptedRow struct {
	id           string
	messageID    string
	status       string
	plaintextRef string
	plaintext    string
	redacted     string
	secretRef    string
	errCode      string
	errMessage   string
	errRetryable bool
	decryptedAt  time.Time
}

func (r decryptedRow) toProto() *waappv1.DecryptedMessage {
	return &waappv1.DecryptedMessage{
		DecryptedMessageId: r.id,
		MessageId:          r.messageID,
		Status:             waappv1.DecryptionStatus(waappv1.DecryptionStatus_value[r.status]),
		PlaintextRef:       r.plaintextRef,
		PlaintextText:      &waappv1.SensitiveText{Value: r.plaintext, RedactedValue: r.redacted, SecretRef: r.secretRef},
		DecryptedAt:        timestamppb.New(r.decryptedAt.UTC()),
		LastError:          protoError(r.errCode, r.errMessage, r.errRetryable),
	}
}

type otpMessageRow struct {
	id                   string
	waAccountIDValue     string
	clientProfileID      string
	registeredIdentityID string
	messageID            string
	candidateID          string
	source               string
	sourceParty          string
	otpValue             string
	otpRedacted          string
	otpSecretRef         string
	receivedAt           time.Time
	expiresAt            sql.NullTime
	createdAt            time.Time
	updatedAt            time.Time
}
