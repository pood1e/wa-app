package account

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type receiptConnections struct {
	results []wacore.EngineMessageReceiptResult
	calls   int
	cancel  context.CancelFunc
}

func (c *receiptConnections) Acquire(context.Context, *waappv1.LoginState) (wacore.Communication, error) {
	c.calls++
	return &receiptRunner{connections: c, result: c.results[c.calls-1]}, nil
}

type receiptRunner struct {
	wacore.Communication
	connections *receiptConnections
	result      wacore.EngineMessageReceiptResult
}

func (r *receiptRunner) SendMessageReceipts(context.Context, wacore.EngineMessageReceiptInput) wacore.EngineMessageReceiptResult {
	if r.connections.cancel != nil {
		r.connections.cancel()
	}
	return r.result
}

func TestReadReceiptsReacquireReplacedConnection(t *testing.T) {
	for _, first := range []error{context.Canceled, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "connection closed", true)} {
		t.Run(first.Error(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				connections := &receiptConnections{results: []wacore.EngineMessageReceiptResult{{Err: first}, {Sent: 1}}}
				result := sendReadReceiptsWithConnection(t.Context(), connections, &waappv1.LoginState{}, wacore.EngineMessageReceiptInput{Messages: []wacore.EngineMessageReceipt{{ProviderMessageID: "fixture"}}})
				if result.Err != nil || result.Sent != 1 || connections.calls != 2 {
					t.Fatalf("receipt retry = %+v, acquisitions = %d", result, connections.calls)
				}
			})
		})
	}
}

func TestReadReceiptsPreserveCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	connections := &receiptConnections{results: []wacore.EngineMessageReceiptResult{{Err: context.Canceled}}, cancel: cancel}
	result := sendReadReceiptsWithConnection(ctx, connections, &waappv1.LoginState{}, wacore.EngineMessageReceiptInput{})
	if !errors.Is(result.Err, context.Canceled) || connections.calls != 1 {
		t.Fatalf("canceled receipt = %+v, acquisitions = %d", result, connections.calls)
	}
}

func TestReadReceiptsBoundConnectionRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		connections := &receiptConnections{results: []wacore.EngineMessageReceiptResult{{Err: context.Canceled}, {Err: context.Canceled}}}
		result := sendReadReceiptsWithConnection(t.Context(), connections, &waappv1.LoginState{}, wacore.EngineMessageReceiptInput{})
		failure := shared.ToProtoError(result.Err)
		if connections.calls != 2 || failure.GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT || !failure.GetRetryable() {
			t.Fatalf("exhausted receipts = %+v, acquisitions = %d", result, connections.calls)
		}
	})
}

func TestReadReceiptsRejectPartialSuccess(t *testing.T) {
	connections := &receiptConnections{results: []wacore.EngineMessageReceiptResult{{Sent: 1}}}
	input := wacore.EngineMessageReceiptInput{Messages: []wacore.EngineMessageReceipt{{ProviderMessageID: "first"}, {ProviderMessageID: "second"}}}
	result := sendReadReceiptsWithConnection(t.Context(), connections, &waappv1.LoginState{}, input)
	if result.Err == nil || result.Sent != 1 || connections.calls != 1 {
		t.Fatalf("partial receipts = %+v, acquisitions = %d", result, connections.calls)
	}
}
