package account

import (
	"context"
	"errors"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type receiptConnectionProvider interface {
	Acquire(context.Context, *waappv1.LoginState) (wacore.Communication, error)
}

func sendReadReceiptsWithConnection(ctx context.Context, connections receiptConnectionProvider, login *waappv1.LoginState, input wacore.EngineMessageReceiptInput) wacore.EngineMessageReceiptResult {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var result wacore.EngineMessageReceiptResult
	for attempt := range 2 {
		runner, err := connections.Acquire(ctx, login)
		if err != nil {
			return wacore.EngineMessageReceiptResult{Err: err}
		}
		sender, ok := runner.(waMessageReceiptSender)
		if !ok {
			return wacore.EngineMessageReceiptResult{Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "WA read receipt sender is not configured", false)}
		}
		result = sender.SendMessageReceipts(ctx, input)
		if result.Err == nil && result.Sent != len(input.Messages) {
			result.Err = shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "WA read receipts were not fully sent", true)
		}
		if !receiptConnectionEnded(ctx, result.Err) {
			return result
		}
		// Receipts are idempotent; reacquire ownership after a connection is replaced.
		if attempt == 0 && !shared.SleepContext(ctx, 100*time.Millisecond) {
			return wacore.EngineMessageReceiptResult{Sent: result.Sent, Err: ctx.Err()}
		}
	}
	result.Err = shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "account connection changed; read receipts are pending", true)
	return result
}

func receiptConnectionEnded(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	var appErr *shared.AppError
	return errors.As(err, &appErr) && appErr.Code == waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT && appErr.Retryable
}
