package session

import (
	"context"
	"slices"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type pendingDecryptionHost struct {
	Host
	learned bool
	calls   []string
}

func (h *pendingDecryptionHost) DecryptWithProtocol(_ context.Context, req *waappv1.DecryptMessageRequest, _ wacore.Communication, _ waappv1.WaOtpSource) (*waappv1.DecryptMessageResponse, error) {
	h.calls = append(h.calls, req.GetMessageId())
	if req.GetMessageId() == "prekey" {
		h.learned = true
	}
	if !h.learned {
		return &waappv1.DecryptMessageResponse{Error: &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_DECRYPTION_FAILED}}, nil
	}
	return &waappv1.DecryptMessageResponse{DecryptedMessage: &waappv1.DecryptedMessage{Status: waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED}}, nil
}

type pendingDecryptionRepository struct {
	Repository
	queries int
	batches [][]*waappv1.InboundMessage
}

func (r *pendingDecryptionRepository) ListPendingEncryptedInboundMessages(context.Context, string, string, int) ([]*waappv1.InboundMessage, error) {
	r.queries++
	if len(r.batches) > 0 {
		batch := r.batches[0]
		r.batches = r.batches[1:]
		return batch, nil
	}
	if r.queries > 1 {
		return nil, nil
	}
	return []*waappv1.InboundMessage{{MessageId: "waiting-for-key", EncryptionState: waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_ENCRYPTED}}, nil
}

func TestPendingMessagesRetryWhenAnotherMessageEstablishesKeys(t *testing.T) {
	host, repository := &pendingDecryptionHost{}, &pendingDecryptionRepository{}
	manager := &Manager{host: host, repository: repository, ids: shared.RandomIDGenerator{}}
	session := &waappv1.MessageSession{}
	manager.decryptMessageBatch(t.Context(), session, []*waappv1.InboundMessage{{MessageId: "waiting-for-key"}}, nil)
	if repository.queries != 0 {
		t.Fatal("a failed decrypt retried the pending queue without learning keys")
	}
	manager.decryptMessageBatch(t.Context(), session, []*waappv1.InboundMessage{{MessageId: "prekey"}}, nil)
	if repository.queries != 2 || !slices.Equal(host.calls, []string{"waiting-for-key", "prekey", "waiting-for-key"}) {
		t.Fatal("learning new keys did not retry the previously unreadable message")
	}
}

func TestStartupPendingQueueRetriesAfterRestoringSenderKeys(t *testing.T) {
	group := &waappv1.InboundMessage{MessageId: "group", EncryptionState: waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_ENCRYPTED}
	prekey := &waappv1.InboundMessage{MessageId: "prekey", EncryptionState: waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_ENCRYPTED}
	host := new(pendingDecryptionHost)
	repository := &pendingDecryptionRepository{batches: [][]*waappv1.InboundMessage{{group, prekey}, {group}}}
	manager := &Manager{host: host, repository: repository, ids: shared.RandomIDGenerator{}}
	manager.decryptPendingMessages(t.Context(), new(waappv1.MessageSession), nil)
	if repository.queries != 3 || !slices.Equal(host.calls, []string{"group", "prekey", "group"}) {
		t.Fatal("startup left an older group message pending after learning its sender key")
	}
}
