package session

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type connectionRefresher interface {
	Reconnect(context.Context) error
}

// Reconnect refreshes the owned transport without replacing the login identity.
func (m *Manager) Reconnect(ctx context.Context, login *waappv1.LoginState) error {
	m.mu.Lock()
	entry := m.entries[longConnectionKey(login)]
	if entry == nil || entry.revoked || entry.runner == nil {
		m.mu.Unlock()
		return nil
	}
	runner := entry.runner
	m.mu.Unlock()
	refresher, ok := runner.(connectionRefresher)
	if !ok {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "account connection cannot be refreshed", false)
	}
	if err := refresher.Reconnect(ctx); err != nil {
		return err
	}
	m.update(longConnectionKey(login), func(state *waappv1.LongConnectionState) {
		if state.GetStatus() != waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STOPPED {
			state.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_RECONNECTING
		}
	})
	return nil
}
