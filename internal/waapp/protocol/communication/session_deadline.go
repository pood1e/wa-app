package communication

import (
	"context"
	"fmt"
	"time"
)

func (s *ChatdSession) bindDeadline(ctx context.Context, deadline time.Time) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.Conn == nil {
		return nil, fmt.Errorf("chatd session is not open")
	}
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	conn := s.Conn
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, s.Transport.fail(err)
	}
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = conn.SetDeadline(time.Now())
		close(stopped)
	})
	return func() {
		if !stop() {
			<-stopped
		}
		if err := conn.SetDeadline(time.Time{}); err != nil {
			s.Transport.fail(err)
		}
	}, nil
}
