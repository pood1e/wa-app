package session

import (
	"context"
	"log"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

// Revoke 在账号被服务端登出(号码已在其他设备注册/被接管)时调用:把快照置为终态
// STOPPED 并附作废原因,然后取消该 entry,使长连接停止且不再重连。restore 只拉取
// ACTIVE 登录态,作废后的账号不会被重新拉起。
func (m *Manager) Revoke(registeredIdentityID string, cause error) {
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
		entry.snapshot.LastError = shared.ToProtoError(cause)
	}
	m.mu.Unlock()
	log.Printf("WA long connection revoked: registered_identity=%s reason=%s", registeredIdentityID, longConnectionLogErrorMessage(shared.ToProtoError(cause).GetMessage()))
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) markStopped(key string) {
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

func (m *Manager) stopAll() {
	m.mu.Lock()
	m.rootCtx = nil
	if m.cancel != nil {
		m.cancel()
	}
	for _, entry := range m.entries {
		if entry != nil && entry.cancel != nil {
			entry.cancel()
		}
	}
	m.mu.Unlock()
	m.workers.Wait()
}

// StopAccount joins all of an account's connections before durable data is removed.
func (m *Manager) StopAccount(ctx context.Context, accountID string) error {
	return m.stopAccount(ctx, accountID, "")
}

// Handoff closes previous account generations before starting the confirmed login.
func (m *Manager) Handoff(ctx context.Context, login *waappv1.LoginState) error {
	if err := m.stopAccount(ctx, login.GetWaAccountId(), longConnectionKey(login)); err != nil {
		return err
	}
	m.Ensure(ctx, login)
	return nil
}

func (m *Manager) stopAccount(ctx context.Context, accountID, keepKey string) error {
	m.mu.Lock()
	var pending []<-chan struct{}
	for key, entry := range m.entries {
		if entry.snapshot.GetWaAccountId() != accountID || (keepKey != "" && key == keepKey) {
			continue
		}
		entry.revoked = true
		if entry.cancel != nil {
			entry.cancel()
		}
		if entry.done != nil {
			pending = append(pending, entry.done)
		}
	}
	m.mu.Unlock()
	for _, done := range pending {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func longConnectionKey(loginState *waappv1.LoginState) string {
	return shared.FirstNonEmpty(loginState.GetRegisteredIdentityId(), loginState.GetLoginStateId())
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
