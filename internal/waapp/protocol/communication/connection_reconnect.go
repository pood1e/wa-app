package communication

import "context"

func (c *Connection) Reconnect(ctx context.Context) error {
	ctx, finish := c.operationContext(ctx)
	defer finish()
	if err := c.LockInteractiveIQLocked(ctx); err != nil {
		return err
	}
	defer c.UnlockInteractiveIQLocked()
	return c.CloseLocked()
}
