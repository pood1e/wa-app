package session

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type lifecycleTrace struct {
	mu       sync.Mutex
	events   []string
	released chan struct{}
}

func (t *lifecycleTrace) add(event string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
}

type lostLease struct{ trace *lifecycleTrace }

func (l lostLease) ClaimLease(context.Context, string, string, time.Duration) (bool, error) {
	l.trace.add("claim")
	return true, nil
}
func (l lostLease) RenewLease(context.Context, string, string, time.Duration) (bool, error) {
	l.trace.add("lost")
	return false, nil
}
func (l lostLease) ReleaseLease(context.Context, string, string) error {
	l.trace.add("release")
	close(l.trace.released)
	return nil
}

type fixtureConnection struct {
	wacore.Communication
	trace *lifecycleTrace
}

func (c fixtureConnection) Close() error { c.trace.add("connection closed"); return nil }

type lifecycleHost struct {
	Host
	trace   *lifecycleTrace
	started chan struct{}
}

func (h lifecycleHost) OpenMessageSession(context.Context, *waappv1.OpenMessageSessionRequest) (*waappv1.OpenMessageSessionResponse, error) {
	return &waappv1.OpenMessageSessionResponse{Session: &waappv1.MessageSession{MessageSessionId: "fixture-session"}}, nil
}
func (h lifecycleHost) CloseMessageSession(context.Context, *waappv1.CloseMessageSessionRequest) (*waappv1.CloseMessageSessionResponse, error) {
	h.trace.add("session closed")
	return &waappv1.CloseMessageSessionResponse{}, nil
}
func (h lifecycleHost) NewSessionProtocol(context.Context, *waappv1.LoginState, *waappv1.MessageSession) (wacore.Communication, error) {
	return fixtureConnection{trace: h.trace}, nil
}
func (h lifecycleHost) ReceiveWithProtocol(ctx context.Context, _ *waappv1.ReceiveMessageBatchRequest, _ wacore.Communication) (*waappv1.ReceiveMessageBatchResponse, error) {
	close(h.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

type lifecycleRepository struct {
	Repository
	login *waappv1.LoginState
}

func (r lifecycleRepository) ListActiveLoginStates(context.Context) ([]*waappv1.LoginState, error) {
	return []*waappv1.LoginState{r.login}, nil
}
func (r lifecycleRepository) ListStoppedLoginStates(context.Context) ([]*waappv1.LoginState, error) {
	return nil, nil
}
func (r lifecycleRepository) CloseStaleOpenMessageSessions(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r lifecycleRepository) ListPendingEncryptedInboundMessages(context.Context, string, string, int) ([]*waappv1.InboundMessage, error) {
	return nil, nil
}

func TestLeaseLossClosesConnectionBeforeRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager, trace, started := newLifecycleManager()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan struct{})
		go func() { defer close(done); _ = manager.Run(ctx) }()
		<-started
		<-trace.released
		cancel()
		<-done
		assertLifecycle(t, trace, "lost")
	})
}

func TestStopAccountJoinsAndPreventsRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager, trace, started := newLifecycleManager()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan struct{})
		go func() { defer close(done); _ = manager.Run(ctx) }()
		<-started
		if err := manager.StopAccount(t.Context(), "fixture-account"); err != nil {
			t.Fatal(err)
		}
		manager.Ensure(t.Context(), fixtureLogin())
		if manager.Runner(fixtureLogin()) != nil {
			t.Fatal("stopped account restarted")
		}
		cancel()
		<-done
		assertLifecycle(t, trace, "")
	})
}

func newLifecycleManager() (*Manager, *lifecycleTrace, <-chan struct{}) {
	trace := &lifecycleTrace{released: make(chan struct{})}
	started := make(chan struct{})
	manager := New(lifecycleHost{trace: trace, started: started}, lifecycleRepository{login: fixtureLogin()}, lostLease{trace}, shared.SystemClock{}, shared.RandomIDGenerator{})
	return manager, trace, started
}
func fixtureLogin() *waappv1.LoginState {
	return &waappv1.LoginState{WaAccountId: "fixture-account", RegisteredIdentityId: "fixture-identity", Status: waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE}
}
func assertLifecycle(t *testing.T, trace *lifecycleTrace, required string) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	positions := map[string]int{}
	counts := map[string]int{}
	for i, event := range trace.events {
		positions[event] = i
		counts[event]++
	}
	for _, event := range []string{"claim", "connection closed", "session closed", "release"} {
		if counts[event] != 1 {
			t.Fatalf("unexpected lifecycle events: %v", trace.events)
		}
	}
	if required != "" && counts[required] != 1 {
		t.Fatalf("missing %s", required)
	}
	if positions["connection closed"] > positions["release"] || positions["session closed"] > positions["release"] {
		t.Fatal("ownership released before cleanup completed")
	}
}
