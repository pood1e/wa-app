package communication

import (
	"context"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

// lockedIQSender keeps state reads, wire operations and state commits under one connection lock.
type lockedIQSender struct{ connection *Connection }

func (s lockedIQSender) SendIQ(ctx context.Context, current state.NativeState, identity, version string, node wire.ChatdNode, timeout string) (wire.ChatdNode, ChatdSessionUpdate, error) {
	return s.connection.sendIQLocked(ctx, current, identity, version, node, timeout)
}

func (c *Connection) SendMessageReceipts(ctx context.Context, input wacore.EngineMessageReceiptInput) wacore.EngineMessageReceiptResult {
	ctx, finish := c.operationContext(ctx)
	defer finish()
	ctx, cancel := context.WithTimeout(ctx, DefaultMessageReceiptTimeout)
	defer cancel()
	if err := c.LockInteractiveIQLocked(ctx); err != nil {
		return wacore.EngineMessageReceiptResult{Err: err}
	}
	defer c.UnlockInteractiveIQLocked()
	messages := NormalizeMessageReceipts(input.Messages)
	if len(messages) == 0 {
		return wacore.EngineMessageReceiptResult{}
	}
	session, err := c.EnsureSessionWithTimeoutLocked(ctx, c.Input)
	if err != nil {
		return wacore.EngineMessageReceiptResult{Err: err}
	}
	result := session.SendMessageReceipts(ctx, messages, input.Type)
	if result.Err != nil {
		c.closeFailedSessionLocked()
	}
	return result
}
