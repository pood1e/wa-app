package session

import (
	"context"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (m *Manager) runEntry(ctx context.Context, login *waappv1.LoginState, key string) {
	defer m.markStopped(key)
	backoff := longConnectionInitialBackoff
	var reconnects int32
	for ctx.Err() == nil {
		m.update(key, func(s *waappv1.LongConnectionState) {
			s.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STARTING
			if reconnects > 0 {
				s.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_RECONNECTING
			}
			s.ReconnectCount = reconnects
		})
		received, err := m.runConnection(ctx, login, key)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			m.recordLoopError(key, reconnects, err)
			if longConnectionTerminalError(err) || communication.IsAccountTakeoverError(err) || communication.IsAuthenticationFailure(err) {
				m.persistIfAccountTakeover(ctx, login, err)
				if communication.IsAuthenticationFailure(err) {
					cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					m.host.AccountLoginRejected(cleanup, login, err)
					cancel()
				}
				return
			}
		}
		if received {
			backoff = longConnectionInitialBackoff
		}
		if !shared.SleepContext(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff)
		reconnects++
	}
}

func (m *Manager) runConnection(ctx context.Context, login *waappv1.LoginState, key string) (bool, error) {
	owned, release, err := m.own(ctx, login.GetWaAccountId())
	if err != nil {
		return false, err
	}
	defer release()
	current, err := m.openSession(owned, login)
	if err != nil {
		return false, err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_, _ = m.host.CloseMessageSession(cleanup, &waappv1.CloseMessageSessionRequest{MessageSessionId: current.GetMessageSessionId(), Reason: "session connection ended"})
	}()
	m.update(key, func(s *waappv1.LongConnectionState) {
		s.MessageSessionId = current.GetMessageSessionId()
		s.LastError = nil
	})
	runner, err := m.host.NewSessionProtocol(owned, login, current)
	if err != nil {
		return false, err
	}
	var closeOnce sync.Once
	closeRunner := func() { closeOnce.Do(func() { closeLongConnectionRunner(runner) }) }
	stopClose := context.AfterFunc(owned, closeRunner)
	defer func() { stopClose(); m.clearRunner(key); closeRunner() }()
	m.setRunner(key, runner)
	m.decryptPendingMessages(owned, current, runner)
	received := false
	for owned.Err() == nil {
		response, err := m.host.ReceiveWithProtocol(owned, &waappv1.ReceiveMessageBatchRequest{
			Context:          &waappv1.RequestContext{RequestId: m.ids.NewID("wa-rx_"), CorrelationId: login.GetLoginStateId()},
			MessageSessionId: current.GetMessageSessionId(), MaxMessages: 10, WaitTimeout: durationpb.New(WaitTimeout),
		}, runner)
		if err != nil {
			return received, err
		}
		if response.GetError() != nil {
			return received, shared.ErrorFromProto(response.GetError())
		}
		now := timestamppb.New(m.clock.Now())
		messages := response.GetMessages()
		m.update(key, func(s *waappv1.LongConnectionState) {
			if s.LastConnectedAt == nil {
				s.LastConnectedAt = now
			}
			s.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_HEARTBEAT_WAITING
			s.LastHeartbeatAt = now
			s.LastError = nil
			if len(messages) > 0 {
				s.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_CONNECTED
				s.LastMessageAt = now
			}
		})
		received = true
		m.decryptMessageBatch(owned, current, messages, runner)
	}
	return received, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "account session ownership ended", true)
}

// Acquire returns the account's owned connection for both interactive and background work.
func (m *Manager) Acquire(ctx context.Context, login *waappv1.LoginState) (wacore.Communication, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	m.Ensure(ctx, login)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		entry := m.entries[longConnectionKey(login)]
		running := m.rootCtx != nil && m.rootCtx.Err() == nil
		var runner wacore.Communication
		var lastError *waappv1.WaError
		if entry != nil && !entry.revoked {
			runner = entry.runner
		}
		if entry != nil && entry.snapshot.GetLastError() != nil {
			lastError = shared.ToProtoError(shared.ErrorFromProto(entry.snapshot.GetLastError()))
		}
		revoked := entry != nil && entry.revoked
		m.mu.Unlock()
		if runner != nil {
			return runner, nil
		}
		if !running || revoked {
			if lastError != nil {
				return nil, shared.ErrorFromProto(lastError)
			}
			return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "account session is unavailable", false)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
