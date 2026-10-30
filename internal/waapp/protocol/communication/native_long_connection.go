package communication

import (
	"context"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

const (
	LongConnectionChatdAttemptTimeout = 20 * time.Second
	LongConnectionChatdOpenTimeout    = 45 * time.Second
)

type Connection struct {
	*Engine

	operations        *semaphore.Weighted
	Session           *ChatdSession
	Input             wacore.EngineMessageInput
	Pending           []ChatdReceivedItem
	PendingUp         ChatdSessionUpdate
	activeRead        atomic.Pointer[LongConnectionActiveRead]
	iqWaiters         atomic.Int32
	Closed            bool
	ownerCtx          context.Context
	cancel            context.CancelFunc
	stopSessionCancel func() bool
}

type LongConnectionActiveRead struct {
	Cancel    context.CancelFunc
	Preempted atomic.Bool
}

type ConnectionOptions struct {
	Context context.Context
	Input   wacore.EngineMessageInput
}

func NewConnection(engine *Engine, opts ConnectionOptions) *Connection {
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	owner, cancel := context.WithCancel(opts.Context)
	return &Connection{Engine: engine, Input: opts.Input, ownerCtx: owner, cancel: cancel, operations: semaphore.NewWeighted(1)}
}

func (e *Connection) Close() error {
	e.cancel()
	_ = e.operations.Acquire(context.Background(), 1)
	defer e.operations.Release(1)
	e.Closed = true
	return e.CloseLocked()
}

func (e *Connection) ReceiveMessageBatch(ctx context.Context, input wacore.EngineMessageInput) wacore.EngineMessageBatchResult {
	ctx, finish := e.ownershipContext(ctx)
	defer finish()
	if err := e.operations.Acquire(ctx, 1); err != nil {
		return wacore.EngineMessageBatchResult{Err: err}
	}
	defer e.operations.Release(1)
	if e.Closed {
		return wacore.EngineMessageBatchResult{Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "WA long connection runner is closed", true)}
	}
	if input.MessageSessionID != "" {
		e.Input = input
	}

	session, err := e.EnsureSessionWithTimeoutLocked(ctx, input)
	if err != nil {
		e.closeFailedSessionLocked()
		return wacore.EngineMessageBatchResult{Err: ChatdReceiveError(err)}
	}
	if err := e.keepAliveLocked(ctx, session, input); err != nil {
		if ctx.Err() != nil {
			e.closeFailedSessionLocked()
		} else {
			_ = e.CloseLocked()
		}
		return wacore.EngineMessageBatchResult{Err: ChatdReceiveError(err)}
	}
	messages, payloads, otps, update, drained := e.DrainPendingLocked(input)
	if !drained {
		messages, payloads, otps, update, err = e.ReceiveBatchWithActiveReadLocked(ctx, session, input)
		if err != nil {
			if ctx.Err() != nil {
				e.closeFailedSessionLocked()
				return wacore.EngineMessageBatchResult{Err: ChatdReceiveError(ctx.Err())}
			}
			e.CloseLocked()
			if IsAuthenticationFailure(err) || IsAccountTakeoverError(err) {
				return wacore.EngineMessageBatchResult{Err: ChatdReceiveError(err)}
			}
			session, retryErr := e.EnsureSessionWithTimeoutLocked(ctx, input)
			if retryErr != nil {
				return wacore.EngineMessageBatchResult{Err: ChatdReceiveError(retryErr)}
			}
			messages, payloads, otps, update, err = e.ReceiveBatchWithActiveReadLocked(ctx, session, input)
			if err != nil {
				e.CloseLocked()
				return wacore.EngineMessageBatchResult{Err: ChatdReceiveError(err)}
			}
		}
	}
	{
		ctx, cancel := context.WithTimeout(ctx, connectionOperationTimeout)
		defer cancel()
		state, err := e.loadState(ctx, input.ClientProfileID)
		if err != nil {
			e.CloseLocked()
			return wacore.EngineMessageBatchResult{Err: err}
		}
		if ApplyChatdReceiveState(&state, input, payloads, update) {
			if err := e.SaveState(ctx, input.ClientProfileID, state); err != nil {
				e.CloseLocked()
				return wacore.EngineMessageBatchResult{Err: err}
			}
		}
		if err := e.flushSignalRetriesIfActive(ctx, &state, update); err != nil {
			e.CloseLocked()
			return wacore.EngineMessageBatchResult{Err: ChatdReceiveError(err)}
		}
	}
	return wacore.EngineMessageBatchResult{Messages: messages, Contacts: wamodel.ContactsFromContactHints(input.WAAccountID, nil, update.ContactHints, e.clock.Now()), OTPMessages: otps, AccountLogout: AccountLogoutFromUpdate(update.AccountLogout), Receipts: update.Receipts, Presences: update.Presences}
}

func (e *Connection) ApplyAccountSettings(ctx context.Context, input wacore.EngineAccountSettingsInput) wacore.EngineAccountSettingsResult {
	ctx, finish := e.operationContext(ctx)
	defer finish()
	if e == nil || e.Engine == nil {
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "native engine is required", false)}
	}
	if err := e.LockInteractiveIQLocked(ctx); err != nil {
		return wacore.EngineAccountSettingsResult{Err: err}
	}
	defer e.UnlockInteractiveIQLocked()
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: err}
	}
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = EnsureChatStatic(state.ChatStatic)
		_ = e.SaveState(ctx, input.ClientProfileID, state)
	}
	return e.Engine.ApplyAccountSettingsWithSender(ctx, input, state, lockedIQSender{e})
}

// ResolveContacts 让联系人 usync 复用这条共享长连接(单 chatd),而不是另开并发 ACTIVE 连接,
// 从而不再自我触发服务端 <conflict type="replaced">。
func (e *Connection) ResolveContacts(ctx context.Context, input wacore.EngineContactResolveInput) wacore.EngineContactResolveResult {
	ctx, finish := e.operationContext(ctx)
	defer finish()
	if e == nil || e.Engine == nil {
		return wacore.EngineContactResolveResult{Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "native engine is required", false)}
	}
	if err := e.LockInteractiveIQLocked(ctx); err != nil {
		return wacore.EngineContactResolveResult{Err: err}
	}
	defer e.UnlockInteractiveIQLocked()
	return e.Engine.ResolveContactsWithSender(ctx, input, lockedIQSender{e})
}

func (e *Connection) SendIQ(ctx context.Context, state pstate.NativeState, registeredIdentityID string, appVersion string, request pwire.ChatdNode, timeoutMessage string) (pwire.ChatdNode, ChatdSessionUpdate, error) {
	ctx, finish := e.operationContext(ctx)
	defer finish()
	if err := e.LockInteractiveIQLocked(ctx); err != nil {
		return pwire.ChatdNode{}, ChatdSessionUpdate{}, err
	}
	defer e.UnlockInteractiveIQLocked()
	return e.sendIQLocked(ctx, state, registeredIdentityID, appVersion, request, timeoutMessage)
}

func (e *Connection) sendIQLocked(ctx context.Context, state pstate.NativeState, registeredIdentityID string, appVersion string, request pwire.ChatdNode, timeoutMessage string) (pwire.ChatdNode, ChatdSessionUpdate, error) {
	if e.Closed {
		return pwire.ChatdNode{}, ChatdSessionUpdate{}, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "WA long connection runner is closed", true)
	}
	input := e.Input
	if input.RegisteredIdentityID == "" {
		input.RegisteredIdentityID = registeredIdentityID
	}
	session, err := e.EnsureSessionForIQLocked(ctx, input, state)
	if err != nil {
		e.closeFailedSessionLocked()
		return pwire.ChatdNode{}, ChatdSessionUpdate{}, err
	}
	response, items, update, err := session.SendIQ(ctx, input, request, ContextBoundTimeout(ctx, DefaultAccountIQTimeout), timeoutMessage)
	e.BufferPendingLocked(items, update)
	if err != nil {
		e.closeFailedSessionLocked()
		return pwire.ChatdNode{}, update, err
	}
	return response, update, nil
}

func (e *Connection) EnsureSessionForIQLocked(ctx context.Context, input wacore.EngineMessageInput, state pstate.NativeState) (*ChatdSession, error) {
	if e.Session != nil {
		return e.Session, nil
	}
	if input.ClientProfileID != "" {
		openCtx, cancel := context.WithTimeout(ctx, LongConnectionChatdOpenTimeout)
		defer cancel()
		return e.EnsureSessionLocked(openCtx, input)
	}
	openCtx, cancel := context.WithTimeout(ctx, LongConnectionChatdOpenTimeout)
	defer cancel()
	session, err := e.OpenSessionWithEngine(openCtx, e.Engine, input, state)
	if err == nil {
		e.installSession(session)
		return session, nil
	}
	return nil, err
}

func (e *Connection) DrainPendingLocked(input wacore.EngineMessageInput) ([]*waappv1.InboundMessage, []pwire.ChatdEncPayload, []*waappv1.OtpMessage, ChatdSessionUpdate, bool) {
	if len(e.Pending) == 0 && !HasChatdSessionUpdate(e.PendingUp) {
		return nil, nil, nil, ChatdSessionUpdate{}, false
	}
	limit := input.MaxMessages
	if limit <= 0 {
		limit = 10
	}
	count := len(e.Pending)
	if count > limit {
		count = limit
	}
	items := append([]ChatdReceivedItem(nil), e.Pending[:count]...)
	e.Pending = append([]ChatdReceivedItem(nil), e.Pending[count:]...)
	update := e.PendingUp
	e.PendingUp = ChatdSessionUpdate{}
	messages, payloads, otps := SplitReceivedItems(items)
	return messages, payloads, otps, update, true
}
