package app

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	longConnectionWaitTimeout          = 25 * time.Second
	longConnectionInitialBackoff       = time.Second
	longConnectionMaxBackoff           = 30 * time.Second
	longConnectionDecryptLimit         = 100
	staleMessageSessionTTL             = 10 * time.Minute
	staleMessageSessionCleanupInterval = 5 * time.Minute
)

type LongConnectionManager struct {
	server *Server

	mu      sync.Mutex
	rootCtx context.Context
	cancel  context.CancelFunc
	entries map[string]*longConnectionEntry
}

type longConnectionEntry struct {
	cancel   context.CancelFunc
	runner   ProtocolEngine
	snapshot *waappv1.LongConnectionState
	revoked  bool
}

type longConnectionStopItem struct {
	cancel context.CancelFunc
	runner ProtocolEngine
}

func NewLongConnectionManager(server *Server) *LongConnectionManager {
	return &LongConnectionManager{server: server, entries: map[string]*longConnectionEntry{}}
}

func (m *LongConnectionManager) Run(ctx context.Context) error {
	if m == nil || m.server == nil {
		return nil
	}
	rootCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.rootCtx = rootCtx
	m.cancel = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.stopAll()
	}()
	if err := m.restore(rootCtx); err != nil {
		return err
	}
	m.closeStaleMessageSessions(rootCtx)
	go m.cleanupStaleMessageSessions(rootCtx)
	<-rootCtx.Done()
	return nil
}

func (m *LongConnectionManager) Ensure(ctx context.Context, loginState *waappv1.LoginState) {
	if m == nil || loginState == nil || loginState.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
		return
	}
	m.mu.Lock()
	rootCtx := m.rootCtx
	if rootCtx == nil {
		m.mu.Unlock()
		return
	}
	key := longConnectionKey(loginState)
	if existing, ok := m.entries[key]; ok && existing.cancel != nil {
		m.mu.Unlock()
		return
	}
	entryCtx, cancel := context.WithCancel(rootCtx)
	snapshot := &waappv1.LongConnectionState{
		LoginStateId:         loginState.GetLoginStateId(),
		WaAccountId:          loginState.GetWaAccountId(),
		ClientProfileId:      loginState.GetClientProfileId(),
		RegisteredIdentityId: loginState.GetRegisteredIdentityId(),
		Status:               waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STARTING,
		HeartbeatSupported:   true,
		StartedAt:            timestamppb.New(m.server.clock.Now()),
	}
	m.entries[key] = &longConnectionEntry{cancel: cancel, snapshot: snapshot}
	m.mu.Unlock()
	go m.runEntry(entryCtx, proto.Clone(loginState).(*waappv1.LoginState), key)
	_ = ctx
}

func (m *LongConnectionManager) Snapshots(req *waappv1.GetLongConnectionStatusRequest) []*waappv1.LongConnectionState {
	if m == nil || req == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*waappv1.LongConnectionState{}
	for _, entry := range m.entries {
		if entry == nil || entry.snapshot == nil {
			continue
		}
		s := entry.snapshot
		if req.GetLoginStateId() != "" && s.GetLoginStateId() != req.GetLoginStateId() {
			continue
		}
		if req.GetRegisteredIdentityId() != "" && s.GetRegisteredIdentityId() != req.GetRegisteredIdentityId() {
			continue
		}
		if req.GetWaAccountId() != "" && s.GetWaAccountId() != req.GetWaAccountId() {
			continue
		}
		if req.GetClientProfileId() != "" && s.GetClientProfileId() != req.GetClientProfileId() {
			continue
		}
		out = append(out, proto.Clone(s).(*waappv1.LongConnectionState))
	}
	return out
}

func (m *LongConnectionManager) Runner(loginState *waappv1.LoginState) ProtocolEngine {
	if m == nil || loginState == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[longConnectionKey(loginState)]
	if entry == nil || entry.cancel == nil {
		return nil
	}
	return entry.runner
}

func (m *LongConnectionManager) ActiveRunner(loginState *waappv1.LoginState) ProtocolEngine {
	if m == nil || loginState == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[longConnectionKey(loginState)]
	if entry == nil || entry.cancel == nil || entry.runner == nil || entry.snapshot == nil {
		return nil
	}
	switch entry.snapshot.GetStatus() {
	case waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_CONNECTED,
		waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_HEARTBEAT_WAITING:
		return entry.runner
	default:
		return nil
	}
}

func (m *LongConnectionManager) MessageSessionID(loginState *waappv1.LoginState) string {
	if m == nil || loginState == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[longConnectionKey(loginState)]
	if entry == nil || entry.snapshot == nil {
		return ""
	}
	return entry.snapshot.GetMessageSessionId()
}

func (m *LongConnectionManager) setRunner(key string, runner ProtocolEngine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry := m.entries[key]; entry != nil {
		entry.runner = runner
	}
}

func (m *LongConnectionManager) clearRunner(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry := m.entries[key]; entry != nil {
		entry.runner = nil
	}
}

func (m *LongConnectionManager) restore(ctx context.Context) error {
	records, err := m.server.store.ListActiveLoginStates(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if ctx.Err() != nil {
			return nil
		}
		m.Ensure(ctx, record.LoginState)
	}
	m.seedRevoked(ctx)
	return nil
}

// seedRevoked 在启动时把已作废(转出/远程登出)的登录态喂回一个终态 STOPPED 快照,
// 使「已转出」在进程重启后仍持续展示。这些 entry 不持有连接(cancel/runner 为空),
// 也不会被 restore 的 active 循环或 Ensure 拉起(只拉 ACTIVE 登录态)。
func (m *LongConnectionManager) seedRevoked(ctx context.Context) {
	records, err := m.server.store.ListRevokedLoginStates(ctx)
	if err != nil {
		log.Printf("WA long connection restore revoked failed: %v", sanitizeLogError(err))
		return
	}
	for _, record := range records {
		if ctx.Err() != nil {
			return
		}
		m.seedRevokedEntry(record.LoginState)
	}
}

func (m *LongConnectionManager) seedRevokedEntry(loginState *waappv1.LoginState) {
	if loginState == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rootCtx == nil {
		return
	}
	key := longConnectionKey(loginState)
	if _, ok := m.entries[key]; ok {
		return
	}
	lastErr := loginState.GetLastError()
	if lastErr == nil {
		lastErr = ToProtoError(accountLoggedOutError(""))
	}
	m.entries[key] = &longConnectionEntry{
		revoked: true,
		snapshot: &waappv1.LongConnectionState{
			LoginStateId:         loginState.GetLoginStateId(),
			WaAccountId:          loginState.GetWaAccountId(),
			ClientProfileId:      loginState.GetClientProfileId(),
			RegisteredIdentityId: loginState.GetRegisteredIdentityId(),
			Status:               waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STOPPED,
			LastError:            lastErr,
		},
	}
}

func (m *LongConnectionManager) cleanupStaleMessageSessions(ctx context.Context) {
	ticker := time.NewTicker(staleMessageSessionCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.closeStaleMessageSessions(ctx)
		}
	}
}

func (m *LongConnectionManager) closeStaleMessageSessions(ctx context.Context) {
	if m == nil || m.server == nil || m.server.store == nil {
		return
	}
	closed, err := m.server.store.CloseStaleOpenMessageSessions(ctx, m.server.clock.Now().Add(-staleMessageSessionTTL))
	if err != nil {
		log.Printf("WA stale message session cleanup failed: %v", sanitizeLogError(err))
		return
	}
	if closed > 0 {
		log.Printf("WA stale message session cleanup closed=%d", closed)
	}
}

func (m *LongConnectionManager) runEntry(ctx context.Context, loginState *waappv1.LoginState, key string) {
	backoff := longConnectionInitialBackoff
	reconnects := int32(0)
	defer m.markStopped(key)
	for ctx.Err() == nil {
		m.update(key, func(snapshot *waappv1.LongConnectionState) {
			if reconnects > 0 {
				snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_RECONNECTING
			} else {
				snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STARTING
			}
			snapshot.ReconnectCount = reconnects
		})
		connectionCtx, stopConnection := context.WithCancel(ctx)
		session, err := m.openSession(connectionCtx, loginState)
		if err != nil {
			stopConnection()
			if ctx.Err() != nil {
				return
			}
			m.recordLoopError(key, reconnects, err)
			if longConnectionTerminalError(err) || isAccountTakeoverError(err) {
				m.persistIfAccountTakeover(ctx, loginState, err)
				return
			}
			if !sleepContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			reconnects++
			continue
		}
		m.update(key, func(snapshot *waappv1.LongConnectionState) {
			snapshot.MessageSessionId = session.GetMessageSessionId()
			snapshot.LastError = nil
		})
		runner, err := m.server.longConnectionRunner(connectionCtx, loginState, session)
		if err != nil {
			stopConnection()
			if ctx.Err() != nil {
				return
			}
			m.recordLoopError(key, reconnects, err)
			_, _ = m.server.CloseMessageSession(context.WithoutCancel(ctx), &waappv1.CloseMessageSessionRequest{Context: &waappv1.RequestContext{}, MessageSessionId: session.GetMessageSessionId(), Reason: "long connection runner unavailable"})
			if longConnectionTerminalError(err) || isAccountTakeoverError(err) {
				m.persistIfAccountTakeover(ctx, loginState, err)
				return
			}
			if !sleepContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			reconnects++
			continue
		}
		m.setRunner(key, runner)
		m.decryptPendingMessages(connectionCtx, session, runner)
		receivedHeartbeat := false
		terminal := false
		var lastErr error
		for connectionCtx.Err() == nil {
			resp, err := m.server.receiveMessageBatch(connectionCtx, &waappv1.ReceiveMessageBatchRequest{Context: &waappv1.RequestContext{RequestId: m.server.ids.NewID("wa-rx_"), CorrelationId: loginState.GetLoginStateId()}, MessageSessionId: session.GetMessageSessionId(), MaxMessages: 10, WaitTimeout: durationpb.New(longConnectionWaitTimeout)}, runner)
			if err != nil {
				if ctx.Err() != nil {
					break
				}
				lastErr = err
				m.recordLoopError(key, reconnects, err)
				terminal = longConnectionTerminalError(err) || isAccountTakeoverError(err)
				break
			}
			if resp.GetError() != nil {
				respErr := errorFromProto(resp.GetError())
				lastErr = respErr
				m.recordLoopError(key, reconnects, respErr)
				terminal = longConnectionTerminalError(respErr) || isAccountTakeoverError(respErr)
				break
			}
			now := m.server.clock.Now()
			messages := resp.GetMessages()
			m.update(key, func(snapshot *waappv1.LongConnectionState) {
				if snapshot.GetStatus() != waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_CONNECTED && snapshot.GetStatus() != waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_HEARTBEAT_WAITING {
					snapshot.LastConnectedAt = timestamppb.New(now)
				}
				snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_HEARTBEAT_WAITING
				snapshot.LastHeartbeatAt = timestamppb.New(now)
				snapshot.LastError = nil
				if len(messages) > 0 {
					snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_CONNECTED
					snapshot.LastMessageAt = timestamppb.New(now)
				}
			})
			receivedHeartbeat = true
			backoff = longConnectionInitialBackoff
			m.decryptReceivedMessages(connectionCtx, session, messages, runner)
		}
		stopConnection()
		if ctx.Err() != nil {
			m.clearRunner(key)
			closeLongConnectionRunner(runner)
			return
		}
		m.clearRunner(key)
		closeLongConnectionRunner(runner)
		if terminal {
			_, _ = m.server.CloseMessageSession(context.WithoutCancel(ctx), &waappv1.CloseMessageSessionRequest{Context: &waappv1.RequestContext{}, MessageSessionId: session.GetMessageSessionId(), Reason: "long connection account terminal"})
			m.persistIfAccountTakeover(ctx, loginState, lastErr)
			return
		}
		if !receivedHeartbeat {
			backoff = nextBackoff(backoff)
		}
		reconnects++
		_, _ = m.server.CloseMessageSession(context.WithoutCancel(ctx), &waappv1.CloseMessageSessionRequest{Context: &waappv1.RequestContext{}, MessageSessionId: session.GetMessageSessionId(), Reason: "long connection reconnect"})
		if !sleepContext(ctx, backoff) {
			return
		}
	}
}

func closeLongConnectionRunner(runner ProtocolEngine) {
	if closer, ok := runner.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

// longConnectionTerminalError 判断错误是否为"账号/资料已不存在"的不可重试终态。
// 命中时长连接应停止重连(否则像已删除账号那样无限重连泄漏)。
func longConnectionTerminalError(err error) bool {
	if err == nil {
		return false
	}
	protoErr := ToProtoError(err)
	if protoErr.GetRetryable() {
		return false
	}
	switch protoErr.GetCode() {
	case waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND,
		waappv1.WaErrorCode_WA_ERROR_CODE_PROFILE_NOT_FOUND:
		return true
	default:
		return false
	}
}

// isAccountTakeoverError 判断错误是否为 chatd 下发的"账号被接管"登出信号:服务端在
// <stream:error>/<failure> 里带 <conflict type="device_removed"|"replaced">,表示本设备登录态已失效
// (号码已在其他设备注册)。对齐 APK ErrorStanzaHandler(X.1FJ)对 conflict type 的登出判定。
// 经 chatdReceiveError 保留为非可重试 CONFLICT,消息含 account_takeover 标记,区别于 generic failure。
func isAccountTakeoverError(err error) bool {
	if err == nil {
		return false
	}
	protoErr := ToProtoError(err)
	return protoErr.GetCode() == waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT &&
		strings.Contains(protoErr.GetMessage(), chatdAccountTakeoverMarker)
}

// persistIfAccountTakeover 在长连接因账号被接管终止时把登录态持久化为 REVOKED 并停连,
// 复用 device_logout 的"已转出"终态语义(重启后 restore 只拉 ACTIVE,不再被拉起)。
func (m *LongConnectionManager) persistIfAccountTakeover(ctx context.Context, loginState *waappv1.LoginState, err error) {
	if isAccountTakeoverError(err) {
		m.server.markLoginTransferredOut(context.WithoutCancel(ctx), loginState, err)
	}
}

func (m *LongConnectionManager) openSession(ctx context.Context, loginState *waappv1.LoginState) (*waappv1.MessageSession, error) {
	resp, err := m.server.OpenMessageSession(ctx, &waappv1.OpenMessageSessionRequest{
		Context:              &waappv1.RequestContext{RequestId: m.server.ids.NewID("wa-open_"), CorrelationId: loginState.GetLoginStateId()},
		WaAccountId:          loginState.GetWaAccountId(),
		ClientProfileId:      loginState.GetClientProfileId(),
		RegisteredIdentityId: loginState.GetRegisteredIdentityId(),
	})
	if err != nil {
		return nil, err
	}
	if resp.GetError() != nil {
		return nil, errorFromProto(resp.GetError())
	}
	return resp.GetSession(), nil
}

func (m *LongConnectionManager) decryptPendingMessages(ctx context.Context, session *waappv1.MessageSession, runner ProtocolEngine) {
	messages, err := m.server.store.ListPendingEncryptedInboundMessages(ctx, session.GetWaAccountId(), session.GetClientProfileId(), longConnectionDecryptLimit)
	if err != nil {
		log.Printf("WA long connection pending decrypt load failed: %v", sanitizeLogError(err))
		return
	}
	if len(messages) == 0 {
		return
	}
	log.Printf("WA long connection retry pending decrypt: count=%d", len(messages))
	m.decryptReceivedMessages(ctx, session, messages, runner)
}

func (m *LongConnectionManager) decryptReceivedMessages(ctx context.Context, session *waappv1.MessageSession, messages []*waappv1.InboundMessage, runner ProtocolEngine) {
	for _, msg := range messages {
		if msg.GetEncryptionState() == waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_PLAINTEXT && !strings.HasPrefix(msg.GetPayloadRef(), "plaintext:") {
			continue
		}
		resp, err := m.server.decryptMessage(ctx, &waappv1.DecryptMessageRequest{Context: &waappv1.RequestContext{RequestId: m.server.ids.NewID("wa-dec_"), CorrelationId: session.GetRegisteredIdentityId()}, MessageId: msg.GetMessageId(), SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE, IncludeSensitivePlaintext: true}, runner, waappv1.WaOtpSource_WA_OTP_SOURCE_LONG_CONNECTION)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("WA long connection decrypt failed: message_id=%s error=%v", msg.GetMessageId(), sanitizeLogError(err))
		}
		if resp != nil && resp.GetError() != nil {
			log.Printf("WA long connection decrypt failed: message_id=%s code=%s retryable=%t", msg.GetMessageId(), resp.GetError().GetCode().String(), resp.GetError().GetRetryable())
		}
	}
}

func (m *LongConnectionManager) recordLoopError(key string, reconnects int32, err error) {
	protoErr := ToProtoError(err)
	m.update(key, func(snapshot *waappv1.LongConnectionState) {
		snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_RECONNECTING
		snapshot.ReconnectCount = reconnects
		snapshot.LastError = protoErr
	})
	if reconnects < 5 || reconnects%20 == 0 {
		log.Printf("WA long connection reconnecting count=%d code=%s retryable=%t message=%s", reconnects, protoErr.GetCode().String(), protoErr.GetRetryable(), longConnectionLogErrorMessage(protoErr.GetMessage()))
	}
}

func longConnectionLogErrorMessage(message string) string {
	if strings.HasPrefix(message, "native chatd receive failed:") || strings.HasPrefix(message, "login state remote check failed:") {
		return message
	}
	return safeResponseSnippet(message)
}

func (m *LongConnectionManager) update(key string, mutate func(*waappv1.LongConnectionState)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[key]
	if entry == nil || entry.snapshot == nil || entry.revoked {
		return
	}
	mutate(entry.snapshot)
}

// Revoke 在账号被服务端登出(号码已在其他设备注册/被接管)时调用:把快照置为终态
// STOPPED 并附作废原因,然后取消该 entry,使长连接停止且不再重连。restore 只拉取
// ACTIVE 登录态,作废后的账号不会被重新拉起。
func (m *LongConnectionManager) Revoke(registeredIdentityID string, cause error) {
	if m == nil || strings.TrimSpace(registeredIdentityID) == "" {
		return
	}
	m.mu.Lock()
	entry := m.entries[registeredIdentityID]
	if entry == nil || entry.revoked {
		m.mu.Unlock()
		return
	}
	entry.revoked = true
	cancel := entry.cancel
	entry.cancel = nil
	if entry.snapshot != nil {
		entry.snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STOPPED
		entry.snapshot.LastError = ToProtoError(cause)
	}
	m.mu.Unlock()
	log.Printf("WA long connection revoked: registered_identity=%s reason=%s", registeredIdentityID, longConnectionLogErrorMessage(ToProtoError(cause).GetMessage()))
	if cancel != nil {
		cancel()
	}
}

func (m *LongConnectionManager) markStopped(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[key]
	if entry == nil || entry.snapshot == nil {
		return
	}
	entry.cancel = nil
	entry.runner = nil
	entry.snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STOPPED
}

func (m *LongConnectionManager) stopAll() {
	m.mu.Lock()
	items := []longConnectionStopItem{}
	for _, entry := range m.entries {
		if entry != nil && entry.cancel != nil {
			items = append(items, longConnectionStopItem{cancel: entry.cancel, runner: entry.runner})
			entry.cancel = nil
			entry.runner = nil
			if entry.snapshot != nil {
				entry.snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STOPPED
			}
		}
	}
	m.mu.Unlock()
	for _, item := range items {
		item.cancel()
	}
	var wg sync.WaitGroup
	for _, item := range items {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			closeLongConnectionRunner(item.runner)
		}()
	}
	wg.Wait()
}

func (s *Server) ensureLongConnection(ctx context.Context, loginState *waappv1.LoginState) {
	if s != nil && s.longConnections != nil {
		s.longConnections.Ensure(ctx, loginState)
	}
}

func (s *Server) revokeLongConnection(registeredIdentityID string, cause error) {
	if s != nil && s.longConnections != nil {
		s.longConnections.Revoke(registeredIdentityID, cause)
	}
}

// markLoginTransferredOut 在长连接判定账号已被接管/转出(chatd 持续登录被拒)时调用:
// 把登录态持久化为 REVOKED 并停连,复用 device_logout 的终态语义,使重启后不再被拉起
// (restore 只拉 ACTIVE),仪表盘呈现"已转出"终态而非无限重连。
func (s *Server) markLoginTransferredOut(ctx context.Context, loginState *waappv1.LoginState, cause error) {
	if s == nil || loginState == nil {
		return
	}
	registeredIdentityID := loginState.GetRegisteredIdentityId()
	now := s.clock.Now()
	if fresh, err := s.store.GetLoginStateByRegisteredIdentity(ctx, registeredIdentityID); err == nil && fresh != nil {
		loginState = fresh
	}
	if loginState.Audit == nil {
		loginState.Audit = &waappv1.AuditStamp{CreatedAt: timestamppb.New(now)}
	}
	loginState.Status = waappv1.LoginStateStatus_LOGIN_STATE_STATUS_REVOKED
	loginState.LastError = ToProtoError(cause)
	loginState.Audit.UpdatedAt = timestamppb.New(now)
	if err := s.store.SaveLoginState(ctx, loginState, "native-db:"+loginState.GetClientProfileId()); err != nil {
		log.Printf("WA long connection persist transferred-out failed: registered_identity=%s error=%v", registeredIdentityID, sanitizeLogError(err))
	}
	s.markWAAccountTransferredOut(ctx, loginState.GetWaAccountId())
	s.revokeLongConnection(registeredIdentityID, cause)
}

func (s *Server) longConnectionRunner(ctx context.Context, loginState *waappv1.LoginState, session *waappv1.MessageSession) (ProtocolEngine, error) {
	engine, ok := s.runner.(*NativeEngine)
	if !ok {
		return s.runner, nil
	}
	input := longConnectionEngineInput(session)
	input.AppVersion = s.protocolIDAppVersion(ctx, input.ProtocolProfileID)
	return newLongConnectionNativeEngine(engine, longConnectionNativeEngineOptions{Input: input}), nil
}

func longConnectionEngineInput(session *waappv1.MessageSession) EngineMessageInput {
	if session == nil {
		return EngineMessageInput{}
	}
	return EngineMessageInput{
		WAAccountID:          session.GetWaAccountId(),
		ClientProfileID:      session.GetClientProfileId(),
		RegisteredIdentityID: session.GetRegisteredIdentityId(),
		ProtocolProfileID:    session.GetProtocolProfileId(),
		MessageSessionID:     session.GetMessageSessionId(),
	}
}

func longConnectionKey(loginState *waappv1.LoginState) string {
	return firstNonEmpty(loginState.GetRegisteredIdentityId(), loginState.GetLoginStateId())
}

func nextBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return 2 * time.Second
	}
	current *= 2
	if current > longConnectionMaxBackoff {
		return longConnectionMaxBackoff
	}
	return current
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
