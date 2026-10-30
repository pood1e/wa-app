package store

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func mergeInboundMessageState(existing *waappv1.InboundMessage, next *waappv1.InboundMessage) {
	if existing == nil || next == nil {
		return
	}
	if existing.GetEncryptionState() == waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTED && next.GetEncryptionState() != waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTED {
		next.EncryptionState = existing.GetEncryptionState()
		next.Kind = existing.GetKind()
		next.LastError = existing.GetLastError()
	}
	if next.GetProviderMessageId() == "" {
		next.ProviderMessageId = existing.GetProviderMessageId()
	}
	if next.GetProviderTimestamp() == nil {
		next.ProviderTimestamp = existing.GetProviderTimestamp()
	}
	if next.GetReadAt() == nil {
		next.ReadAt = existing.GetReadAt()
	}
	if next.GetReadReceiptSentAt() == nil {
		next.ReadReceiptSentAt = existing.GetReadReceiptSentAt()
	}
	if next.GetDeliveryReceiptSentAt() == nil {
		next.DeliveryReceiptSentAt = existing.GetDeliveryReceiptSentAt()
	}
	if existing.GetDeleteStatus() == waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_DELETED_FOR_ME || next.GetDeleteStatus() == waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_UNSPECIFIED {
		next.DeleteStatus = existing.GetDeleteStatus()
	}
	if next.GetDeleteStatus() == waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_UNSPECIFIED {
		next.DeleteStatus = waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_NOT_DELETED
	}
	if next.GetDeletedAt() == nil {
		next.DeletedAt = existing.GetDeletedAt()
	}
}
func firstProtoTime(values ...*timestamppb.Timestamp) *timestamppb.Timestamp {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
