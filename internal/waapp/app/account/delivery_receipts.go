package account

import (
	"context"
	"fmt"
	"log"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *serverCore) flushDeliveryReceipts(ctx context.Context, session *waappv1.MessageSession, runner wacore.Communication) {
	if _, ok := runner.(waMessageReceiptSender); !ok {
		return
	}
	messages, err := s.store.ListPendingDeliveryReceipts(ctx, session.GetWaAccountId(), wamodel.MaxMessageActionBatchSize)
	if err == nil {
		err = s.sendDeliveryReceipts(ctx, session, messages, runner)
	}
	if err != nil && ctx.Err() == nil {
		log.Printf("WA delivery receipt pending: code=%s", shared.ToProtoError(err).GetCode())
	}
}

func (s *serverCore) sendDeliveryReceipts(ctx context.Context, session *waappv1.MessageSession, messages []*waappv1.InboundMessage, runner wacore.Communication) error {
	sender, ok := runner.(waMessageReceiptSender)
	if !ok {
		return nil
	}
	var records []messageActionRecord
	for _, message := range messages {
		if receiptableInboundMessage(message) && message.GetReadReceiptSentAt() == nil && message.GetDeliveryReceiptSentAt() == nil && message.GetSource() != waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_IMPORTED_HISTORY && message.GetProviderMessageId() != "" {
			records = append(records, messageActionRecord{message: message})
		}
	}
	receipts := communication.NormalizeMessageReceipts(messageReceiptMessages(records, waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_DELIVERED))
	if len(receipts) == 0 {
		return nil
	}
	result := sender.SendMessageReceipts(ctx, wacore.EngineMessageReceiptInput{
		WAAccountID: session.GetWaAccountId(), ClientProfileID: session.GetClientProfileId(), RegisteredIdentityID: session.GetRegisteredIdentityId(),
		Messages: receipts, Type: waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_DELIVERED,
	})
	if result.Err != nil {
		return result.Err
	}
	if result.Sent != len(receipts) {
		return fmt.Errorf("delivery receipt batch was incomplete")
	}
	for _, record := range records {
		record.message.DeliveryReceiptSentAt = shared.ProtoTimestamp(s.clock.Now())
	}
	return s.store.SaveInboundMessages(ctx, actionRecordMessages(records))
}
