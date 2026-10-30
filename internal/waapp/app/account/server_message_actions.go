package account

import (
	"context"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	protocolcommunication "github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

type waMessageReceiptSender interface {
	SendMessageReceipts(context.Context, wacore.EngineMessageReceiptInput) wacore.EngineMessageReceiptResult
}

type messageActionRecord struct {
	message *waappv1.InboundMessage
	session *waappv1.MessageSession
}

func (s *messagingHandler) MarkAccountMessagesRead(ctx context.Context, req *waappv1.MarkAccountMessagesReadRequest) (*waappv1.MarkAccountMessagesReadResponse, error) {

	accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.MarkAccountMessagesReadResponse{Error: shared.ToProtoError(err)}, nil
	}
	if _, err := s.GetWAAccountRecord(ctx, accountID); err != nil {
		return &waappv1.MarkAccountMessagesReadResponse{Error: shared.ToProtoError(err)}, nil
	}
	records, err := s.loadMessageReadRecords(ctx, accountID, req.GetAccountMessageIds(), req.GetContactRef(), req.GetLocalOnly())
	if err != nil {
		return &waappv1.MarkAccountMessagesReadResponse{Error: shared.ToProtoError(err)}, nil
	}
	now := s.clock.Now()
	changed := markMessagesRead(records, now)
	if changed > 0 {
		if err := s.store.SaveInboundMessages(ctx, actionRecordMessages(records)); err != nil {
			return &waappv1.MarkAccountMessagesReadResponse{Error: shared.ToProtoError(err)}, nil
		}
	}
	resp := &waappv1.MarkAccountMessagesReadResponse{UpdatedCount: int32(changed)}
	if req.GetLocalOnly() {
		return resp, nil
	}
	sent, sendErr := s.sendReadReceipts(ctx, accountID, records)
	resp.ReceiptSentCount = int32(sent)
	if sendErr != nil {
		resp.ReceiptError = shared.ToProtoError(sendErr)
	} else if sent > 0 {
		for _, record := range records {
			if len(messageReceiptMessages([]messageActionRecord{record}, waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_READ)) > 0 {
				record.message.ReadReceiptSentAt = shared.ProtoTimestamp(s.clock.Now())
			}
		}
		if err := s.store.SaveInboundMessages(ctx, actionRecordMessages(records)); err != nil {
			resp.ReceiptError = shared.ToProtoError(err)
		}
	}
	return resp, nil
}

func (s *messagingHandler) DeleteAccountMessages(ctx context.Context, req *waappv1.DeleteAccountMessagesRequest) (*waappv1.DeleteAccountMessagesResponse, error) {

	accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.DeleteAccountMessagesResponse{Error: shared.ToProtoError(err)}, nil
	}
	if _, err := s.GetWAAccountRecord(ctx, accountID); err != nil {
		return &waappv1.DeleteAccountMessagesResponse{Error: shared.ToProtoError(err)}, nil
	}
	mode := req.GetMode()
	if mode == waappv1.AccountMessageDeleteMode_ACCOUNT_MESSAGE_DELETE_MODE_UNSPECIFIED {
		mode = waappv1.AccountMessageDeleteMode_ACCOUNT_MESSAGE_DELETE_MODE_FOR_ME
	}
	if mode == waappv1.AccountMessageDeleteMode_ACCOUNT_MESSAGE_DELETE_MODE_FOR_EVERYONE {
		return s.revokeAccountMessages(ctx, accountID, req.GetAccountMessageIds())
	}
	if mode != waappv1.AccountMessageDeleteMode_ACCOUNT_MESSAGE_DELETE_MODE_FOR_ME {
		return &waappv1.DeleteAccountMessagesResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "unsupported delete mode", false))}, nil
	}
	records, err := s.loadMessageActionRecords(ctx, accountID, req.GetAccountMessageIds())
	if err != nil {
		return &waappv1.DeleteAccountMessagesResponse{Error: shared.ToProtoError(err)}, nil
	}
	changed := markMessagesDeletedForMe(records, s.clock.Now())
	if changed > 0 {
		if err := s.store.SaveInboundMessages(ctx, actionRecordMessages(records)); err != nil {
			return &waappv1.DeleteAccountMessagesResponse{Error: shared.ToProtoError(err)}, nil
		}
	}
	return &waappv1.DeleteAccountMessagesResponse{UpdatedCount: int32(changed)}, nil
}

func (s *serverCore) loadMessageReadRecords(ctx context.Context, accountID string, requestedIDs []string, contactRef string, localOnly bool) ([]messageActionRecord, error) {
	if len(normalizeActionMessageIDs(requestedIDs)) > 0 {
		return s.loadMessageActionRecords(ctx, accountID, requestedIDs)
	}
	contactRef = strings.TrimSpace(contactRef)
	if contactRef == "" {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "account_message_ids or contact_ref is required", false)
	}
	messages, err := s.store.ListPendingReadMessagesByContactRefs(ctx, accountID, s.resolveContactActionRefs(ctx, accountID, contactRef), !localOnly, wamodel.MaxMessageActionBatchSize)
	if err != nil {
		return nil, err
	}
	records := make([]messageActionRecord, 0, len(messages))
	for _, msg := range messages {
		records = append(records, messageActionRecord{message: msg})
	}
	return records, nil
}

func (s *serverCore) loadMessageActionRecords(ctx context.Context, accountID string, requestedIDs []string) ([]messageActionRecord, error) {
	ids := normalizeActionMessageIDs(requestedIDs)
	if len(ids) == 0 {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "account_message_ids is required", false)
	}
	records := make([]messageActionRecord, 0, len(ids))
	for _, id := range ids {
		msg, err := s.store.GetInboundMessage(ctx, id)
		if err != nil {
			return nil, err
		}
		session, err := s.store.GetMessageSession(ctx, msg.GetMessageSessionId())
		if err != nil {
			return nil, err
		}
		if session.GetWaAccountId() != accountID {
			return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "message does not belong to WA account", false)
		}
		records = append(records, messageActionRecord{message: msg, session: session})
	}
	return records, nil
}

func normalizeActionMessageIDs(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
		if len(out) >= wamodel.MaxMessageActionBatchSize {
			break
		}
	}
	return out
}

func markMessagesRead(records []messageActionRecord, at time.Time) int {
	changed := 0
	for _, record := range records {
		msg := record.message
		if !readableInboundMessage(msg) || msg.GetReadAt() != nil {
			continue
		}
		msg.ReadAt = shared.ProtoTimestamp(at)
		changed++
	}
	return changed
}

func markMessagesDeletedForMe(records []messageActionRecord, at time.Time) int {
	changed := 0
	for _, record := range records {
		msg := record.message
		if msg == nil || msg.GetDeleteStatus() == waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_DELETED_FOR_ME {
			continue
		}
		msg.DeleteStatus = waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_DELETED_FOR_ME
		msg.DeletedAt = shared.ProtoTimestamp(at)
		changed++
	}
	return changed
}

func actionRecordMessages(records []messageActionRecord) []*waappv1.InboundMessage {
	messages := make([]*waappv1.InboundMessage, 0, len(records))
	for _, record := range records {
		if record.message != nil {
			messages = append(messages, record.message)
		}
	}
	return messages
}

func (s *serverCore) sendReadReceipts(ctx context.Context, accountID string, records []messageActionRecord) (int, error) {
	receipts := protocolcommunication.NormalizeMessageReceipts(messageReceiptMessages(records, waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_READ))
	if len(receipts) == 0 {
		return 0, nil
	}
	loginState, err := s.activeContactResolveLoginState(ctx, accountID)
	if err != nil {
		return 0, err
	}
	result := sendReadReceiptsWithConnection(ctx, s.longConnections, loginState, wacore.EngineMessageReceiptInput{
		WAAccountID:          accountID,
		ClientProfileID:      loginState.GetClientProfileId(),
		RegisteredIdentityID: loginState.GetRegisteredIdentityId(),
		AppVersion:           s.loginStateAppVersion(ctx, loginState),
		Messages:             receipts,
		RemoteTimeout:        protocolcommunication.DefaultMessageReceiptTimeout,
		Type:                 waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_READ,
	})
	return result.Sent, result.Err
}

func messageReceiptMessages(records []messageActionRecord, receiptType waappv1.MessageReceiptType) []wacore.EngineMessageReceipt {
	out := []wacore.EngineMessageReceipt{}
	for _, record := range records {
		msg := record.message
		eligible := readableInboundMessage(msg)
		if receiptType == waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_DELIVERED {
			eligible = receiptableInboundMessage(msg)
		}
		if !eligible || msg.GetProviderMessageId() == "" || msg.GetReadReceiptSentAt() != nil {
			continue
		}
		chatJID := strings.TrimSpace(msg.GetContactRef())
		participantJID := strings.TrimSpace(msg.GetSenderRef())
		if chatJID == "" {
			chatJID = participantJID
		}
		if chatJID == "" {
			continue
		}
		if participantJID == chatJID {
			participantJID = ""
		}
		out = append(out, wacore.EngineMessageReceipt{ChatJID: chatJID, ParticipantJID: participantJID, ProviderMessageID: msg.GetProviderMessageId()})
	}
	return out
}

func readableInboundMessage(msg *waappv1.InboundMessage) bool {
	return msg != nil && msg.GetKind() == waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE &&
		msg.GetDirection() != waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND &&
		msg.GetDeleteStatus() != waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_DELETED_FOR_ME &&
		(msg.GetEncryptionState() == waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTED || msg.GetEncryptionState() == waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_PLAINTEXT)
}

func (s *serverCore) messageActionRunner(ctx context.Context, requestContext *waappv1.RequestContext, loginState *waappv1.LoginState) (wacore.Communication, func(), error) {
	runner, err := s.longConnections.Acquire(ctx, loginState)
	return runner, func() {}, err
}

func receiptableInboundMessage(msg *waappv1.InboundMessage) bool {
	return msg != nil && (msg.GetKind() == waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE || msg.GetKind() == waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_SYSTEM) && msg.GetDirection() != waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND && msg.GetSource() != waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_IMPORTED_HISTORY && msg.GetEncryptionState() == waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTED && msg.GetDeleteStatus() != waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_DELETED_FOR_ME
}
