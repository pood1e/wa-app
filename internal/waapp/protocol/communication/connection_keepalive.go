package communication

import (
	"context"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

const (
	heartbeatInterval        = 20 * time.Second
	heartbeatResponseTimeout = 10 * time.Second
	heartbeatFailureBudget   = 3 * time.Minute
)

// keepAliveLocked shares the receive loop's socket and preserves interleaved events.
func (c *Connection) keepAliveLocked(ctx context.Context, session *ChatdSession, input wacore.EngineMessageInput) error {
	now := time.Now()
	if session.nextHeartbeatAt.IsZero() {
		session.nextHeartbeatAt = now.Add(heartbeatInterval)
		session.lastHeartbeatSuccess = now
	}
	if now.Before(session.nextHeartbeatAt) {
		return nil
	}
	session.nextHeartbeatAt = now.Add(heartbeatInterval)
	heartbeatCtx, cancel := context.WithTimeout(ctx, heartbeatResponseTimeout)
	defer cancel()
	response, items, update, err := session.SendIQ(heartbeatCtx, input, wire.BuildPingNode(), heartbeatResponseTimeout, "chatd heartbeat response timed out")
	c.BufferPendingLocked(items, update)
	if err == nil {
		err = ChatdIQError(response)
	}
	if err == nil {
		session.lastHeartbeatSuccess = time.Now()
		return nil
	}
	if session.Transport.failure != nil || ctx.Err() != nil || time.Since(session.lastHeartbeatSuccess) >= heartbeatFailureBudget {
		return err
	}
	return nil
}
