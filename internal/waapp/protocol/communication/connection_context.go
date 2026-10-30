package communication

import (
	"context"
	"time"
)

const connectionOperationTimeout = 45 * time.Second

func (c *Connection) operationContext(caller context.Context) (context.Context, func()) {
	ctx, finish := c.ownershipContext(caller)
	ctx, cancel := context.WithTimeout(ctx, connectionOperationTimeout)
	return ctx, func() { cancel(); finish() }
}

func (c *Connection) ownershipContext(caller context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(caller)
	stop := context.AfterFunc(c.ownerCtx, cancel)
	if c.ownerCtx.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}

// installSession binds socket lifetime to ownership independently of the operation lock.
func (c *Connection) installSession(session *ChatdSession) {
	c.Session = session
	c.stopSessionCancel = context.AfterFunc(c.ownerCtx, func() { _ = session.Close() })
}
