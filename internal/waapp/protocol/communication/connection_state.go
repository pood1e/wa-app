package communication

import (
	"context"
	"errors"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

type connectionState struct {
	connection *Connection
	current    state.NativeState
	changed    bool
}

func withConnectionState[T any](ctx context.Context, c *Connection, work func(context.Context, *connectionState) (T, error)) (result T, err error) {
	ctx, finish := c.operationContext(ctx)
	defer finish()
	if err = c.LockInteractiveIQLocked(ctx); err != nil {
		return result, err
	}
	defer c.UnlockInteractiveIQLocked()
	current, err := c.loadState(ctx, c.Input.ClientProfileID)
	if err != nil {
		return result, err
	}
	session := &connectionState{connection: c, current: current}
	result, err = work(ctx, session)
	if session.changed {
		checkpoint, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, c.SaveState(checkpoint, c.Input.ClientProfileID, session.current))
	}
	return result, err
}

func (s *connectionState) query(ctx context.Context, request wire.ChatdNode) (wire.ChatdNode, error) {
	c := s.connection
	response, update, err := c.sendIQLocked(ctx, s.current, c.Input.RegisteredIdentityID, c.Input.AppVersion, request, "account request timed out")
	s.changed = ApplyChatdSessionUpdateState(&s.current, update) || s.changed
	if err == nil {
		err = ChatdIQError(response)
	}
	return response, err
}
