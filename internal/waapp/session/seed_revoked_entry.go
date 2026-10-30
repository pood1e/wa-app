package session

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	protocolcommunication "github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	protocolstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (m *Manager) seedStoppedEntry(loginState *waappv1.LoginState) {
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
	m.entries[key] = &longConnectionEntry{
		revoked: true,
		snapshot: &waappv1.LongConnectionState{
			LoginStateId:         loginState.GetLoginStateId(),
			WaAccountId:          loginState.GetWaAccountId(),
			ClientProfileId:      loginState.GetClientProfileId(),
			RegisteredIdentityId: loginState.GetRegisteredIdentityId(),
			Status:               waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STOPPED,
			LastError:            loginState.GetLastError(),
		},
	}
}

func (m *Manager) cleanupStaleMessageSessions(ctx context.Context) {
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

func (m *Manager) closeStaleMessageSessions(ctx context.Context) {
	if m == nil || m.host == nil || m.repository == nil {
		return
	}
	closed, err := m.repository.CloseStaleOpenMessageSessions(ctx, m.clock.Now().Add(-staleMessageSessionTTL))
	if err != nil {
		log.Printf("WA stale message session cleanup failed: %v", shared.SanitizeLogError(err))
		return
	}
	if closed > 0 {
		log.Printf("WA stale message session cleanup closed=%d", closed)
	}
}

func (m *Manager) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(longConnectionReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reconcileStoppedAccounts(ctx)
		}
	}
}

// reconcileStoppedAccounts 对“账号仍 ACTIVE、但长连接已彻底停止(非重连中)”的号做一次真实登录探测,
// 让登录态与对外展示和服务端实况一致:能恢复的(探测 ACTIVE)重新拉起长连接;已被服务端拒登的标记登录态
// INVALID(前端据此显示离线)。只探测无活跃连接的号,绝不在长连接存活/重连时另开连接,避免自我 replaced。
func (m *Manager) reconcileStoppedAccounts(ctx context.Context) {
	if m == nil || m.host == nil {
		return
	}
	records, err := m.repository.ListActiveLoginStates(ctx)
	if err != nil {
		log.Printf("WA long connection reconcile list failed: %v", shared.SanitizeLogError(err))
		return
	}
	for _, record := range records {
		if ctx.Err() != nil {
			return
		}
		loginState := record
		if !m.longConnectionStopped(loginState) {
			continue
		}
		req := &waappv1.CheckLoginStateRequest{
			Context:      &waappv1.RequestContext{RequestId: m.ids.NewID("wa-reconcile_"), CorrelationId: loginState.GetLoginStateId()},
			LoginStateId: loginState.GetLoginStateId(),
		}
		if _, err := m.host.CheckLoginState(ctx, req); err != nil {
			log.Printf("WA long connection reconcile check failed: registered_identity=%s error=%v", loginState.GetRegisteredIdentityId(), shared.SanitizeLogError(err))
		}
	}
}

// longConnectionStopped 判断该账号当前是否没有在运行的长连接(无 entry,或 entry 已 markStopped 把 cancel
// 置空)。只有真正停止的号才允许 reconcile 探测——长连接存活/重连中(cancel != nil)时不探测,避免另开
// 一条并发连接触发服务端 replaced。
func (m *Manager) longConnectionStopped(loginState *waappv1.LoginState) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[longConnectionKey(loginState)]
	return entry == nil || entry.cancel == nil
}

func closeLongConnectionRunner(runner wacore.Communication) {
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
	protoErr := shared.ToProtoError(err)
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

// persistIfAccountTakeover 在长连接因账号被接管终止时把登录态持久化为 REVOKED 并停连,
// 复用 device_logout 的"已转出"终态语义(重启后 restore 只拉 ACTIVE,不再被拉起)。
func (m *Manager) persistIfAccountTakeover(ctx context.Context, loginState *waappv1.LoginState, err error) {
	if protocolcommunication.IsAccountTakeoverError(err) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		m.host.AccountTransferredOut(cleanup, loginState, err)
	}
}

func (m *Manager) openSession(ctx context.Context, loginState *waappv1.LoginState) (*waappv1.MessageSession, error) {
	resp, err := m.host.OpenMessageSession(ctx, &waappv1.OpenMessageSessionRequest{
		Context:              &waappv1.RequestContext{RequestId: m.ids.NewID("wa-open_"), CorrelationId: loginState.GetLoginStateId()},
		WaAccountId:          loginState.GetWaAccountId(),
		ClientProfileId:      loginState.GetClientProfileId(),
		RegisteredIdentityId: loginState.GetRegisteredIdentityId(),
	})
	if err != nil {
		return nil, err
	}
	if resp.GetError() != nil {
		return nil, shared.ErrorFromProto(resp.GetError())
	}
	return resp.GetSession(), nil
}

func (m *Manager) decryptPendingMessages(ctx context.Context, session *waappv1.MessageSession, runner wacore.Communication) {
	for ctx.Err() == nil {
		messages, err := m.repository.ListPendingEncryptedInboundMessages(ctx, session.GetWaAccountId(), session.GetClientProfileId(), longConnectionDecryptLimit)
		if err != nil {
			log.Printf("WA long connection pending decrypt load failed: %v", shared.SanitizeLogError(err))
			return
		}
		if len(messages) == 0 {
			return
		}
		log.Printf("WA long connection retry pending decrypt: count=%d", len(messages))
		if !m.decryptReceivedMessages(ctx, session, messages, runner) {
			return
		}
	}
}

func (m *Manager) decryptMessageBatch(ctx context.Context, session *waappv1.MessageSession, messages []*waappv1.InboundMessage, runner wacore.Communication) {
	if m.decryptReceivedMessages(ctx, session, messages, runner) {
		m.decryptPendingMessages(ctx, session, runner)
	}
}

func (m *Manager) decryptReceivedMessages(ctx context.Context, session *waappv1.MessageSession, messages []*waappv1.InboundMessage, runner wacore.Communication) bool {
	learned := false
	for _, msg := range messages {
		if msg.GetEncryptionState() == waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_PLAINTEXT && !strings.HasPrefix(msg.GetPayloadRef(), "plaintext:") {
			continue
		}
		resp, err := m.host.DecryptWithProtocol(ctx, &waappv1.DecryptMessageRequest{Context: &waappv1.RequestContext{RequestId: m.ids.NewID("wa-dec_"), CorrelationId: session.GetRegisteredIdentityId()}, MessageId: msg.GetMessageId(), SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE, IncludeSensitivePlaintext: true}, runner, waappv1.WaOtpSource_WA_OTP_SOURCE_LONG_CONNECTION)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("WA long connection decrypt failed: message_id=%s error=%v", msg.GetMessageId(), shared.SanitizeLogError(err))
		}
		if resp != nil && resp.GetError() != nil {
			log.Printf("WA long connection decrypt failed: message_id=%s code=%s retryable=%t", msg.GetMessageId(), resp.GetError().GetCode().String(), resp.GetError().GetRetryable())
		}
		if err == nil && resp.GetError() == nil && resp.GetDecryptedMessage().GetStatus() == waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED {
			learned = true
		}
	}
	return learned
}

func (m *Manager) recordLoopError(key string, reconnects int32, err error) {
	protoErr := shared.ToProtoError(err)
	var accountID string
	m.update(key, func(snapshot *waappv1.LongConnectionState) {
		accountID = snapshot.GetWaAccountId()
		snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_RECONNECTING
		snapshot.ReconnectCount = reconnects
		snapshot.LastError = protoErr
	})
	if reconnects < 5 || reconnects%20 == 0 {
		log.Printf("WA long connection reconnecting account_id=%s count=%d code=%s retryable=%t message=%s", accountID, reconnects, protoErr.GetCode().String(), protoErr.GetRetryable(), longConnectionLogErrorMessage(protoErr.GetMessage()))
	}
}

func longConnectionLogErrorMessage(message string) string {
	if strings.HasPrefix(message, "native chatd receive failed:") || strings.HasPrefix(message, "login state remote check failed:") {
		return message
	}
	return protocolstate.SafeResponseSnippet(message)
}

func (m *Manager) update(key string, mutate func(*waappv1.LongConnectionState)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[key]
	if entry == nil || entry.snapshot == nil || entry.revoked {
		return
	}
	mutate(entry.snapshot)
}
