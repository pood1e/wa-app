package account

import (
	"context"
	"errors"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type deliveryRepository struct {
	Repository
	message *waappv1.InboundMessage
	saves   int
}

func (r *deliveryRepository) ListPendingDeliveryReceipts(context.Context, string, int) ([]*waappv1.InboundMessage, error) {
	if r.message.GetDeliveryReceiptSentAt() != nil {
		return nil, nil
	}
	return []*waappv1.InboundMessage{r.message}, nil
}
func (r *deliveryRepository) SaveInboundMessages(context.Context, []*waappv1.InboundMessage) error {
	r.saves++
	return nil
}

type deliverySender struct {
	wacore.Communication
	fail  bool
	calls int
}

func (s *deliverySender) SendMessageReceipts(_ context.Context, input wacore.EngineMessageReceiptInput) wacore.EngineMessageReceiptResult {
	s.calls++
	if s.fail {
		return wacore.EngineMessageReceiptResult{Err: errors.New("connection lost")}
	}
	return wacore.EngineMessageReceiptResult{Sent: len(input.Messages)}
}

func TestDeliveryReceiptFailureRemainsPendingUntilRetry(t *testing.T) {
	store := &deliveryRepository{message: &waappv1.InboundMessage{MessageId: "fixture", Kind: waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE, EncryptionState: waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTED, ProviderMessageId: "upstream", ContactRef: "12025550124@s.whatsapp.net"}}
	core := &serverCore{store: store, clock: shared.SystemClock{}}
	sender := &deliverySender{fail: true}
	session := &waappv1.MessageSession{WaAccountId: "fixture"}
	core.flushDeliveryReceipts(t.Context(), session, sender)
	if store.saves != 0 || store.message.GetDeliveryReceiptSentAt() != nil {
		t.Fatal("failed delivery receipt was marked as sent")
	}
	sender.fail = false
	core.flushDeliveryReceipts(t.Context(), session, sender)
	core.flushDeliveryReceipts(t.Context(), session, sender)
	if store.saves != 1 || sender.calls != 2 || store.message.GetDeliveryReceiptSentAt() == nil {
		t.Fatal("pending delivery receipt was lost or sent again after success")
	}
}
