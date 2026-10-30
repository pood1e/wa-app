package wamodel

import (
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type AccountMessageParts struct {
	AccountID         string
	SessionID         string
	MessageID         string
	ProviderID        string
	Kind              waappv1.InboundMessageKind
	EncryptionState   waappv1.MessageEncryptionState
	AckStatus         waappv1.MessageAckStatus
	Direction         waappv1.AccountMessageDirection
	Source            waappv1.AccountMessageSource
	ContactRef        string
	SenderRef         string
	PayloadRef        string
	Plaintext         string
	Redacted          string
	SecretRef         string
	LastError         *waappv1.WaError
	ReceivedAt        time.Time
	ReadAt            time.Time
	ReadReceiptSentAt time.Time
	DeleteStatus      waappv1.MessageDeleteStatus
	DeletedAt         time.Time
	Content           *waappv1.MessageContent
}

func NewAccountMessage(parts AccountMessageParts, includeSensitiveText bool) *waappv1.AccountMessage {
	displayPlaintext := parts.Plaintext
	displayRedacted := parts.Redacted
	text := &waappv1.SensitiveText{
		RedactedValue: shared.FirstNonEmpty(displayRedacted, shared.Redacted(displayPlaintext), PayloadTextSummary(parts.PayloadRef)),
		SecretRef:     parts.SecretRef,
	}
	if includeSensitiveText {
		text.Value = displayPlaintext
	}
	return &waappv1.AccountMessage{
		AccountMessageId:  parts.MessageID,
		ProviderMessageId: parts.ProviderID,
		WaAccountId:       parts.AccountID,
		MessageSessionId:  parts.SessionID,
		ContactRef:        ContactRefForMessage(parts.ContactRef, parts.SenderRef),
		SenderRef:         parts.SenderRef,
		Direction:         AccountMessageDirection(parts.Direction, parts.Kind),
		Source:            AccountMessageSource(parts.Source),
		Kind:              parts.Kind,
		EncryptionState:   parts.EncryptionState,
		AckStatus:         parts.AckStatus,
		Text:              text,
		ReceivedAt:        shared.ProtoTimestamp(parts.ReceivedAt),
		LastError:         parts.LastError,
		ReadAt:            shared.ProtoTimestamp(parts.ReadAt),
		ReadReceiptSentAt: shared.ProtoTimestamp(parts.ReadReceiptSentAt),
		DeleteStatus:      AccountMessageDeleteStatus(parts.DeleteStatus),
		DeletedAt:         shared.ProtoTimestamp(parts.DeletedAt),
		Content:           MessageContentOutput(parts.Content, includeSensitiveText),
	}
}

func NewAccountMessageFromInbound(accountID string, msg *waappv1.InboundMessage, decrypted *waappv1.DecryptedMessage, includeSensitiveText bool) *waappv1.AccountMessage {
	if msg == nil {
		return nil
	}
	text := decrypted.GetPlaintextText()
	return NewAccountMessage(AccountMessageParts{
		AccountID:         accountID,
		SessionID:         msg.GetMessageSessionId(),
		MessageID:         msg.GetMessageId(),
		ProviderID:        msg.GetProviderMessageId(),
		Kind:              msg.GetKind(),
		EncryptionState:   msg.GetEncryptionState(),
		AckStatus:         msg.GetAckStatus(),
		Direction:         msg.GetDirection(),
		Source:            msg.GetSource(),
		ContactRef:        msg.GetContactRef(),
		SenderRef:         msg.GetSenderRef(),
		PayloadRef:        msg.GetPayloadRef(),
		Plaintext:         text.GetValue(),
		Redacted:          text.GetRedactedValue(),
		SecretRef:         text.GetSecretRef(),
		LastError:         msg.GetLastError(),
		ReceivedAt:        shared.TimeFromProto(msg.GetReceivedAt()),
		ReadAt:            ProtoTimeOrZero(msg.GetReadAt()),
		ReadReceiptSentAt: ProtoTimeOrZero(msg.GetReadReceiptSentAt()),
		DeleteStatus:      msg.GetDeleteStatus(),
		DeletedAt:         ProtoTimeOrZero(msg.GetDeletedAt()),
		Content:           decrypted.GetContent(),
	}, includeSensitiveText)
}

func MessageContentOutput(content *waappv1.MessageContent, sensitive bool) *waappv1.MessageContent {
	if content == nil {
		return nil
	}
	out := proto.Clone(content).(*waappv1.MessageContent)
	if !sensitive {
		if out.GetQuote().GetText() != nil {
			out.Quote.Text.Value = ""
		}
		if out.GetChange().GetText() != nil {
			out.Change.Text.Value = ""
		}
	}
	return out
}

func AccountMessageDeleteStatus(status waappv1.MessageDeleteStatus) waappv1.MessageDeleteStatus {
	if status == waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_UNSPECIFIED {
		return waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_NOT_DELETED
	}
	return status
}

func ProtoTimeOrZero(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime().UTC()
}

func ContactRefForMessage(contactRef string, sender string) string {
	value := strings.TrimSpace(shared.FirstNonEmpty(contactRef, sender))
	if value == "" {
		return "unknown"
	}
	return value
}

func AccountMessageDirection(direction waappv1.AccountMessageDirection, kind waappv1.InboundMessageKind) waappv1.AccountMessageDirection {
	if direction != waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_UNSPECIFIED {
		return direction
	}
	if kind == waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_SYSTEM {
		return waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_SYSTEM
	}
	return waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND
}

func AccountMessageSource(source waappv1.AccountMessageSource) waappv1.AccountMessageSource {
	if source != waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_UNSPECIFIED {
		return source
	}
	return waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_LONG_CONNECTION
}

func PayloadTextSummary(payloadRef string) string {
	payloadRef = strings.TrimSpace(payloadRef)
	if strings.HasPrefix(payloadRef, "node:") {
		return strings.TrimPrefix(payloadRef, "node:")
	}
	return ""
}
