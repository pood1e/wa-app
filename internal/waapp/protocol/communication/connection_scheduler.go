package communication

import (
	"context"
	"errors"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (e *Connection) BufferPendingLocked(items []ChatdReceivedItem, update ChatdSessionUpdate) {
	if len(items) == 0 && !HasChatdSessionUpdate(update) {
		return
	}
	e.Pending = append(e.Pending, items...)
	e.PendingUp = MergeChatdSessionUpdate(e.PendingUp, update)
}

func (e *Connection) ReceiveBatchWithActiveReadLocked(ctx context.Context, session *ChatdSession, input wacore.EngineMessageInput) ([]*waappv1.InboundMessage, []pwire.ChatdEncPayload, []*waappv1.OtpMessage, ChatdSessionUpdate, error) {
	read, readCtx := e.StartActiveReadLocked(ctx)
	messages, payloads, otps, update, err := session.ReceiveBatch(readCtx, input, e.clock)
	preempted := e.FinishActiveReadLocked(read)
	if preempted && errors.Is(err, context.Canceled) && ctx.Err() == nil {
		err = nil
	}
	return messages, payloads, otps, update, err
}

func (e *Connection) StartActiveReadLocked(ctx context.Context) (*LongConnectionActiveRead, context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	readCtx, cancel := context.WithCancel(ctx)
	read := &LongConnectionActiveRead{Cancel: cancel}
	e.activeRead.Store(read)
	if e.iqWaiters.Load() > 0 {
		read.Preempted.Store(true)
		cancel()
	}
	return read, readCtx
}

func (e *Connection) FinishActiveReadLocked(read *LongConnectionActiveRead) bool {
	if read == nil {
		return false
	}
	read.Cancel()
	e.activeRead.CompareAndSwap(read, nil)
	return read.Preempted.Load()
}

func (e *Connection) LockInteractiveIQLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.iqWaiters.Add(1)
	defer e.iqWaiters.Add(-1)
	if read := e.activeRead.Load(); read != nil {
		read.Preempted.Store(true)
		read.Cancel()
	}
	if err := e.operations.Acquire(ctx, 1); err != nil {
		return err
	}
	if e.Closed {
		e.operations.Release(1)
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "WA long connection runner is closed", true)
	}
	return nil
}

func (e *Connection) UnlockInteractiveIQLocked() {
	e.operations.Release(1)
}

func ContextBoundTimeout(ctx context.Context, fallback time.Duration) time.Duration {
	if fallback <= 0 {
		fallback = DefaultChatdReadWindow
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < fallback {
			return remaining
		}
	}
	return fallback
}

func (e *Connection) EnsureSessionWithTimeoutLocked(ctx context.Context, input wacore.EngineMessageInput) (*ChatdSession, error) {
	if e.Closed {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "account connection is closed", true)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.Session != nil {
		return e.Session, nil
	}
	openCtx, cancel := context.WithTimeout(ctx, LongConnectionChatdOpenTimeout)
	defer cancel()
	return e.EnsureSessionLocked(openCtx, input)
}

func (e *Connection) EnsureSessionLocked(ctx context.Context, input wacore.EngineMessageInput) (*ChatdSession, error) {
	if e.Session != nil {
		return e.Session, nil
	}
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return nil, err
	}
	state.EnsureMaps()
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = EnsureChatStatic(state.ChatStatic)
		if err := e.SaveState(ctx, input.ClientProfileID, state); err != nil {
			return nil, err
		}
	}
	session, err := e.OpenSessionWithEngine(ctx, e.Engine, input, state)
	if err == nil {
		e.installSession(session)
		return session, nil
	}
	return nil, err
}

func (e *Connection) OpenSessionWithEngine(ctx context.Context, engine *Engine, input wacore.EngineMessageInput, state pstate.NativeState) (*ChatdSession, error) {
	if engine == nil {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "native engine is required", false)
	}
	proxyURL, err := engine.proxyURL(ctx, input.ClientProfileID)
	if err != nil {
		return nil, err
	}
	cfg := ChatdConfigForState(proxyURL, state, LongConnectionChatdAttemptTimeout)
	cfg.Endpoints = LongConnectionChatdEndpoints(state)
	client := NewChatdClient(cfg)
	session, err := client.OpenSession(ctx, state, input.RegisteredIdentityID, DefaultLoginPayload, input.AppVersion)
	if err != nil {
		return nil, engine.saveLoginRejection(ctx, input.ClientProfileID, state, err)
	}
	if err := e.activateSession(ctx, engine, input, state, session); err != nil {
		_ = session.Close()
		return nil, err
	}
	return session, nil
}
