package app

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

const (
	longConnectionChatdAttemptTimeout = 20 * time.Second
	longConnectionChatdOpenTimeout    = 45 * time.Second
)

type longConnectionNativeEngine struct {
	*NativeEngine

	mu          sync.Mutex
	cond        *sync.Cond
	session     *chatdSession
	input       EngineMessageInput
	pending     []chatdReceivedItem
	pendingUp   chatdSessionUpdate
	activeRead  *longConnectionActiveRead
	iqWaiters   int
	iqInFlight  bool
	closed      bool
	fallback    *NativeEngine
	release     func()
	releaseOnce sync.Once
}

type longConnectionActiveRead struct {
	cancel    context.CancelFunc
	done      chan struct{}
	session   *chatdSession
	preempted bool
}

type longConnectionNativeEngineOptions struct {
	Release  func()
	Fallback *NativeEngine
	Input    EngineMessageInput
}

var longConnectionProxySessionFallbackLogs = proxyLogLimiter{last: map[string]time.Time{}}

func newLongConnectionNativeEngine(engine *NativeEngine, opts longConnectionNativeEngineOptions) *longConnectionNativeEngine {
	cleanup := opts.Release
	if cleanup == nil {
		cleanup = func() {}
	}
	runner := &longConnectionNativeEngine{NativeEngine: engine, fallback: opts.Fallback, input: opts.Input, release: cleanup}
	runner.cond = sync.NewCond(&runner.mu)
	return runner
}

func (e *longConnectionNativeEngine) Close() error {
	e.mu.Lock()
	e.closed = true
	e.preemptActiveReadLocked()
	err := e.closeLocked()
	e.broadcastLocked()
	e.mu.Unlock()
	e.releaseProxyRoute()
	return err
}

func (e *longConnectionNativeEngine) ReceiveMessageBatch(ctx context.Context, input EngineMessageInput) EngineMessageBatchResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.waitForInteractiveIQLocked(ctx); err != nil {
		return EngineMessageBatchResult{Err: err}
	}
	if e.closed {
		return EngineMessageBatchResult{Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "WA long connection runner is closed", true)}
	}
	if input.MessageSessionID != "" {
		e.input = input
	}

	session, err := e.ensureSessionWithTimeoutLocked(ctx, input)
	if err != nil {
		e.closeLocked()
		return EngineMessageBatchResult{Err: chatdReceiveError(err)}
	}
	now := e.clock.Now()
	messages, payloads, otps, update, drained := e.drainPendingLocked(input)
	if !drained {
		var preempted bool
		messages, payloads, otps, update, err, preempted = e.receiveBatchWithActiveReadLocked(ctx, session, input, now)
		if err != nil {
			if preempted {
				return EngineMessageBatchResult{}
			}
			e.closeLocked()
			session, retryErr := e.ensureSessionWithTimeoutLocked(ctx, input)
			if retryErr != nil {
				return EngineMessageBatchResult{Err: chatdReceiveError(retryErr)}
			}
			now = e.clock.Now()
			messages, payloads, otps, update, err, preempted = e.receiveBatchWithActiveReadLocked(ctx, session, input, now)
			if err != nil {
				if preempted {
					return EngineMessageBatchResult{}
				}
				e.closeLocked()
				return EngineMessageBatchResult{Err: chatdReceiveError(err)}
			}
		}
	}
	if len(payloads) > 0 || len(update.ContactHints) > 0 || update.RoutingInfo != "" || update.Endpoint.Host != "" || update.ServerStaticPublic != "" {
		state, err := e.loadState(ctx, input.ClientProfileID)
		if err != nil {
			e.closeLocked()
			return EngineMessageBatchResult{Err: err}
		}
		if applyChatdReceiveState(&state, input, payloads, update) {
			if err := e.saveState(ctx, input.ClientProfileID, state); err != nil {
				e.closeLocked()
				return EngineMessageBatchResult{Err: err}
			}
		}
	}
	return EngineMessageBatchResult{Messages: messages, Contacts: contactsFromContactHints(input.WAAccountID, nil, update.ContactHints, now), OTPMessages: otps, AccountLogout: accountLogoutFromUpdate(update.AccountLogout)}
}

func (e *longConnectionNativeEngine) ResolveContactProfilePicture(ctx context.Context, input EngineContactProfilePictureInput) EngineContactProfilePictureResult {
	if e == nil || e.NativeEngine == nil {
		return EngineContactProfilePictureResult{Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "native engine is required", false)}
	}
	return e.NativeEngine.resolveContactProfilePictureWithSender(ctx, input, e)
}

func (e *longConnectionNativeEngine) ApplyAccountSettings(ctx context.Context, input EngineAccountSettingsInput) EngineAccountSettingsResult {
	if e == nil || e.NativeEngine == nil {
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "native engine is required", false)}
	}
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: err}
	}
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = ensureChatStatic(state.ChatStatic)
		_ = e.saveState(ctx, input.ClientProfileID, state)
	}
	return e.NativeEngine.applyAccountSettingsWithSender(ctx, input, state, e)
}

// ResolveContacts 让联系人 usync 复用这条共享长连接(单 chatd),而不是另开并发 ACTIVE 连接,
// 从而不再自我触发服务端 <conflict type="replaced">。
func (e *longConnectionNativeEngine) ResolveContacts(ctx context.Context, input EngineContactResolveInput) EngineContactResolveResult {
	if e == nil || e.NativeEngine == nil {
		return EngineContactResolveResult{Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "native engine is required", false)}
	}
	return e.NativeEngine.resolveContactsWithSender(ctx, input, e)
}

func (e *longConnectionNativeEngine) sendIQ(ctx context.Context, state nativeState, registeredIdentityID string, appVersion string, request chatdNode, timeoutMessage string) (chatdNode, chatdSessionUpdate, error) {
	if err := e.lockInteractiveIQLocked(ctx); err != nil {
		return chatdNode{}, chatdSessionUpdate{}, err
	}
	defer e.unlockInteractiveIQLocked()
	if e.closed {
		return chatdNode{}, chatdSessionUpdate{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "WA long connection runner is closed", true)
	}
	input := e.input
	if input.RegisteredIdentityID == "" {
		input.RegisteredIdentityID = registeredIdentityID
	}
	session, err := e.ensureSessionForIQLocked(ctx, input, state)
	if err != nil {
		e.closeLocked()
		return chatdNode{}, chatdSessionUpdate{}, err
	}
	response, items, update, err := sendChatdIQWithContext(ctx, session, input, request, contextBoundTimeout(ctx, defaultAccountIQTimeout), timeoutMessage)
	e.bufferPendingLocked(items, update)
	if err != nil {
		e.closeLocked()
		return chatdNode{}, update, err
	}
	return response, update, nil
}

func (e *longConnectionNativeEngine) ensureSessionForIQLocked(ctx context.Context, input EngineMessageInput, state nativeState) (*chatdSession, error) {
	if e.session != nil {
		return e.session, nil
	}
	if input.ClientProfileID != "" {
		openCtx, cancel := context.WithTimeout(ctx, longConnectionChatdOpenTimeout)
		defer cancel()
		return e.ensureSessionLocked(openCtx, input)
	}
	openCtx, cancel := context.WithTimeout(ctx, longConnectionChatdOpenTimeout)
	defer cancel()
	session, err := e.openSessionWithEngine(openCtx, e.NativeEngine, input, state)
	if err == nil {
		e.session = session
		return session, nil
	}
	if reason := longConnectionProxySessionFallbackReason(err); reason != "" && e.fallback != nil {
		if session, fallbackErr := e.openSessionWithEngine(openCtx, e.fallback, input, state); fallbackErr == nil {
			e.releaseProxyRoute()
			e.NativeEngine = e.fallback
			e.fallback = nil
			e.session = session
			logLongConnectionProxySessionFallback(reason)
			return session, nil
		}
	}
	return nil, err
}

func (e *longConnectionNativeEngine) drainPendingLocked(input EngineMessageInput) ([]*waappv1.InboundMessage, []chatdEncPayload, []*waappv1.OtpMessage, chatdSessionUpdate, bool) {
	if len(e.pending) == 0 && !hasChatdSessionUpdate(e.pendingUp) {
		return nil, nil, nil, chatdSessionUpdate{}, false
	}
	limit := input.MaxMessages
	if limit <= 0 {
		limit = 10
	}
	count := len(e.pending)
	if count > limit {
		count = limit
	}
	items := append([]chatdReceivedItem(nil), e.pending[:count]...)
	e.pending = append([]chatdReceivedItem(nil), e.pending[count:]...)
	update := e.pendingUp
	e.pendingUp = chatdSessionUpdate{}
	messages, payloads, otps := splitReceivedItems(items)
	return messages, payloads, otps, update, true
}

func (e *longConnectionNativeEngine) bufferPendingLocked(items []chatdReceivedItem, update chatdSessionUpdate) {
	if len(items) == 0 && len(update.ContactHints) == 0 && update.AccountLogout == nil {
		return
	}
	e.pending = append(e.pending, items...)
	e.pendingUp = mergeChatdSessionUpdate(e.pendingUp, update)
}

func (e *longConnectionNativeEngine) receiveBatchWithActiveReadLocked(ctx context.Context, session *chatdSession, input EngineMessageInput, now time.Time) ([]*waappv1.InboundMessage, []chatdEncPayload, []*waappv1.OtpMessage, chatdSessionUpdate, error, bool) {
	read, readCtx := e.startActiveReadLocked(ctx, session)
	e.mu.Unlock()
	messages, payloads, otps, update, err := receiveChatdBatchWithContext(readCtx, session, input, now)
	e.mu.Lock()
	preempted := e.finishActiveReadLocked(read)
	return messages, payloads, otps, update, err, preempted
}

func (e *longConnectionNativeEngine) startActiveReadLocked(ctx context.Context, session *chatdSession) (*longConnectionActiveRead, context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	readCtx, cancel := context.WithCancel(ctx)
	read := &longConnectionActiveRead{cancel: cancel, done: make(chan struct{}), session: session}
	e.activeRead = read
	return read, readCtx
}

func (e *longConnectionNativeEngine) finishActiveReadLocked(read *longConnectionActiveRead) bool {
	if read == nil {
		return false
	}
	read.cancel()
	preempted := read.preempted
	if e.activeRead == read {
		e.activeRead = nil
	}
	close(read.done)
	e.broadcastLocked()
	return preempted
}

func (e *longConnectionNativeEngine) preemptActiveReadLocked() {
	if e.activeRead == nil {
		return
	}
	e.activeRead.preempted = true
	if e.activeRead.session != nil && e.activeRead.session.conn != nil {
		_ = e.activeRead.session.conn.SetReadDeadline(time.Now())
	}
}

func (e *longConnectionNativeEngine) waitForInteractiveIQLocked(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	stop := context.AfterFunc(ctx, func() {
		e.mu.Lock()
		e.broadcastLocked()
		e.mu.Unlock()
	})
	defer stop()
	for e.iqWaiters > 0 || e.iqInFlight {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.closed {
			return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "WA long connection runner is closed", true)
		}
		e.conditionLocked().Wait()
	}
	return ctx.Err()
}

func (e *longConnectionNativeEngine) lockInteractiveIQLocked(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	stop := context.AfterFunc(ctx, func() {
		e.mu.Lock()
		e.broadcastLocked()
		e.mu.Unlock()
	})
	e.mu.Lock()
	e.iqWaiters++
	for {
		if err := ctx.Err(); err != nil {
			e.iqWaiters--
			e.broadcastLocked()
			e.mu.Unlock()
			stop()
			return err
		}
		if e.closed {
			e.iqWaiters--
			e.broadcastLocked()
			e.mu.Unlock()
			stop()
			return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "WA long connection runner is closed", true)
		}
		if e.activeRead != nil {
			read := e.activeRead
			read.preempted = true
			read.cancel()
			done := read.done
			e.mu.Unlock()
			select {
			case <-done:
			case <-ctx.Done():
				e.mu.Lock()
				e.iqWaiters--
				e.broadcastLocked()
				e.mu.Unlock()
				stop()
				return ctx.Err()
			}
			e.mu.Lock()
			continue
		}
		if !e.iqInFlight {
			e.iqWaiters--
			e.iqInFlight = true
			e.broadcastLocked()
			stop()
			return nil
		}
		e.conditionLocked().Wait()
	}
}

func (e *longConnectionNativeEngine) unlockInteractiveIQLocked() {
	e.iqInFlight = false
	e.broadcastLocked()
	e.mu.Unlock()
}

func (e *longConnectionNativeEngine) conditionLocked() *sync.Cond {
	if e.cond == nil {
		e.cond = sync.NewCond(&e.mu)
	}
	return e.cond
}

func (e *longConnectionNativeEngine) broadcastLocked() {
	if e.cond != nil {
		e.cond.Broadcast()
	}
}

func contextBoundTimeout(ctx context.Context, fallback time.Duration) time.Duration {
	if fallback <= 0 {
		fallback = defaultChatdReadWindow
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < fallback {
			return remaining
		}
	}
	return fallback
}

func (e *longConnectionNativeEngine) ensureSessionWithTimeoutLocked(ctx context.Context, input EngineMessageInput) (*chatdSession, error) {
	if e.session != nil {
		return e.session, nil
	}
	openCtx, cancel := context.WithTimeout(ctx, longConnectionChatdOpenTimeout)
	defer cancel()
	return e.ensureSessionLocked(openCtx, input)
}

func (e *longConnectionNativeEngine) ensureSessionLocked(ctx context.Context, input EngineMessageInput) (*chatdSession, error) {
	if e.session != nil {
		return e.session, nil
	}
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return nil, err
	}
	state.ensureMaps()
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = ensureChatStatic(state.ChatStatic)
		if err := e.saveState(ctx, input.ClientProfileID, state); err != nil {
			return nil, err
		}
	}
	session, err := e.openSessionWithEngine(ctx, e.NativeEngine, input, state)
	if err == nil {
		e.session = session
		return session, nil
	}
	if reason := longConnectionProxySessionFallbackReason(err); reason != "" && e.fallback != nil {
		if session, fallbackErr := e.openSessionWithEngine(ctx, e.fallback, input, state); fallbackErr == nil {
			e.releaseProxyRoute()
			e.NativeEngine = e.fallback
			e.fallback = nil
			e.session = session
			logLongConnectionProxySessionFallback(reason)
			return session, nil
		}
	}
	return nil, err
}

func (e *longConnectionNativeEngine) openSessionWithEngine(ctx context.Context, engine *NativeEngine, input EngineMessageInput, state nativeState) (*chatdSession, error) {
	if engine == nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "native engine is required", false)
	}
	proxyURL, err := engine.proxyURL()
	if err != nil {
		return nil, err
	}
	cfg := chatdConfigForState(proxyURL, state, longConnectionChatdAttemptTimeout)
	cfg.Endpoints = longConnectionChatdEndpoints(state)
	client := newChatdClient(cfg)
	session, err := client.openSession(ctx, state, input.RegisteredIdentityID, defaultLoginPayload, input.AppVersion)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (e *longConnectionNativeEngine) releaseProxyRoute() {
	if e == nil {
		return
	}
	e.releaseOnce.Do(e.release)
}

func longConnectionProxySessionFallbackReason(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "connection reset by peer"):
		return "connection_reset"
	case strings.Contains(text, "i/o timeout") || strings.Contains(text, "deadline") || strings.Contains(text, "timeout"):
		return "timeout"
	case strings.Contains(text, "socks5"):
		return "socks5_failed"
	case strings.Contains(text, "proxy"):
		return "proxy_failed"
	case strings.Contains(text, "connection refused"):
		return "connection_refused"
	case strings.Contains(text, "eof"):
		return "eof"
	default:
		return ""
	}
}

func logLongConnectionProxySessionFallback(reason string) {
	reason = safeProxyLogToken(reason, "session_failed")
	if !longConnectionProxySessionFallbackLogs.allow("wa_long_connection_session", reason, time.Now().UTC()) {
		return
	}
	log.Printf("WA long connection proxy session fallback reason=%s", reason)
}

func longConnectionChatdEndpoints(state nativeState) []chatdEndpoint {
	endpoints := []chatdEndpoint{}
	seen := map[string]struct{}{}
	add := func(host string, port int) {
		endpoint := chatdEndpoint{Host: host, Port: port}.normalized(defaultChatdHost, defaultChatdPort)
		if endpoint.Host == "" || endpoint.Port != defaultChatdPort {
			return
		}
		key := endpoint.address()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		endpoints = append(endpoints, endpoint)
	}
	if state.ChatConnection.LastHost != "" {
		add(state.ChatConnection.LastHost, state.ChatConnection.LastPort)
	}
	add(defaultChatdHost, defaultChatdPort)
	add(chatdFallbackHost, defaultChatdPort)
	return endpoints
}

func (e *longConnectionNativeEngine) closeLocked() error {
	if e.session == nil {
		return nil
	}
	err := e.session.Close()
	e.session = nil
	return err
}

func receiveChatdBatchWithContext(ctx context.Context, session *chatdSession, input EngineMessageInput, now time.Time) ([]*waappv1.InboundMessage, []chatdEncPayload, []*waappv1.OtpMessage, chatdSessionUpdate, error) {
	stopContextClose := closeChatdSessionOnContext(ctx, session)
	defer stopContextClose()
	return session.receiveBatch(input, now)
}

func sendChatdIQWithContext(ctx context.Context, session *chatdSession, input EngineMessageInput, request chatdNode, timeout time.Duration, timeoutMessage string) (chatdNode, []chatdReceivedItem, chatdSessionUpdate, error) {
	stopContextClose := closeChatdSessionOnContext(ctx, session)
	defer stopContextClose()
	return session.sendIQ(ctx, input, request, timeout, timeoutMessage)
}

// closeChatdSessionOnContext 用于长连接的接收循环与交互式 IQ —— 二者共享同一条 conn。
// ctx 取消(IQ 抢占接收读、或 IQ 自身超时/取消)时,只用读截止时间打断阻塞读,让
// receiveBatch/sendIQ 优雅返回,绝不 Close() 这条共享连接;否则一次账号设置同步(2FA)
// 就会把长连接整条关掉,触发重连掉线。真正的关闭只在 runEntry 拆链时经 Close() 完成。
func closeChatdSessionOnContext(ctx context.Context, session *chatdSession) func() {
	if ctx == nil || session == nil || session.conn == nil {
		return func() {}
	}
	conn := session.conn
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.SetReadDeadline(time.Now())
		case <-done:
		}
	}()
	return func() { close(done) }
}

var _ ProtocolEngine = (*longConnectionNativeEngine)(nil)
var _ interface{ Close() error } = (*longConnectionNativeEngine)(nil)
