package session

import (
	"context"
	"log"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

const (
	WaitTimeout                        = 25 * time.Second
	longConnectionInitialBackoff       = time.Second
	longConnectionMaxBackoff           = 30 * time.Second
	longConnectionDecryptLimit         = 100
	staleMessageSessionTTL             = 10 * time.Minute
	staleMessageSessionCleanupInterval = 5 * time.Minute
	longConnectionReconcileInterval    = 5 * time.Minute
)

// Host is the narrow surface the long-connection manager needs from
// the service, so the manager depends on this port rather than the concrete
// *Server (breaks the Server<->manager reference cycle).
type Host interface {
	OpenMessageSession(context.Context, *waappv1.OpenMessageSessionRequest) (*waappv1.OpenMessageSessionResponse, error)
	CloseMessageSession(context.Context, *waappv1.CloseMessageSessionRequest) (*waappv1.CloseMessageSessionResponse, error)
	CheckLoginState(context.Context, *waappv1.CheckLoginStateRequest) (*waappv1.CheckLoginStateResponse, error)
	DecryptWithProtocol(context.Context, *waappv1.DecryptMessageRequest, wacore.Communication, waappv1.WaOtpSource) (*waappv1.DecryptMessageResponse, error)
	ReceiveWithProtocol(context.Context, *waappv1.ReceiveMessageBatchRequest, wacore.Communication) (*waappv1.ReceiveMessageBatchResponse, error)
	NewSessionProtocol(context.Context, *waappv1.LoginState, *waappv1.MessageSession) (wacore.Communication, error)
	AccountTransferredOut(context.Context, *waappv1.LoginState, error)
	AccountLoginRejected(context.Context, *waappv1.LoginState, error)
}
type Repository interface {
	ListActiveLoginStates(context.Context) ([]*waappv1.LoginState, error)
	ListStoppedLoginStates(context.Context) ([]*waappv1.LoginState, error)
	CloseStaleOpenMessageSessions(context.Context, time.Time) (int64, error)
	ListPendingEncryptedInboundMessages(context.Context, string, string, int) ([]*waappv1.InboundMessage, error)
}

type Manager struct {
	host       Host
	repository Repository
	leases     LeaseStore
	clock      shared.Clock
	ids        shared.IDGenerator

	mu      sync.Mutex
	rootCtx context.Context
	cancel  context.CancelFunc
	entries map[string]*longConnectionEntry
	workers sync.WaitGroup
}

type longConnectionEntry struct {
	cancel   context.CancelFunc
	runner   wacore.Communication
	snapshot *waappv1.LongConnectionState
	revoked  bool
	done     chan struct{}
}

func New(host Host, repository Repository, leases LeaseStore, clock shared.Clock, ids shared.IDGenerator) *Manager {
	return &Manager{host: host, repository: repository, leases: leases, clock: clock, ids: ids, entries: map[string]*longConnectionEntry{}}
}

func (m *Manager) Run(ctx context.Context) error {
	if m == nil || m.host == nil {
		return nil
	}
	rootCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.rootCtx = rootCtx
	m.cancel = cancel
	m.mu.Unlock()
	defer m.stopAll()
	if err := m.restore(rootCtx); err != nil {
		return err
	}
	m.closeStaleMessageSessions(rootCtx)
	m.workers.Add(2)
	go func() { defer m.workers.Done(); m.cleanupStaleMessageSessions(rootCtx) }()
	go func() { defer m.workers.Done(); m.reconcileLoop(rootCtx) }()
	<-rootCtx.Done()
	return nil
}

func (m *Manager) Ensure(ctx context.Context, loginState *waappv1.LoginState) {
	if m == nil || loginState == nil || loginState.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
		return
	}
	m.mu.Lock()
	rootCtx := m.rootCtx
	if rootCtx == nil || rootCtx.Err() != nil || ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	key := longConnectionKey(loginState)
	if existing, ok := m.entries[key]; ok && (existing.cancel != nil || existing.revoked) {
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
		StartedAt:            timestamppb.New(m.clock.Now()),
	}
	done := make(chan struct{})
	m.entries[key] = &longConnectionEntry{cancel: cancel, snapshot: snapshot, done: done}
	m.workers.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.workers.Done()
		defer close(done)
		m.runEntry(entryCtx, proto.Clone(loginState).(*waappv1.LoginState), key)
	}()
}

func (m *Manager) Snapshots(req *waappv1.GetLongConnectionStatusRequest) []*waappv1.LongConnectionState {
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

func (m *Manager) Runner(loginState *waappv1.LoginState) wacore.Communication {
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

func (m *Manager) ActiveRunner(loginState *waappv1.LoginState) wacore.Communication {
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

func (m *Manager) MessageSessionID(loginState *waappv1.LoginState) string {
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

func (m *Manager) setRunner(key string, runner wacore.Communication) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry := m.entries[key]; entry != nil {
		entry.runner = runner
	}
}

func (m *Manager) clearRunner(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry := m.entries[key]; entry != nil {
		entry.runner = nil
	}
}

func (m *Manager) restore(ctx context.Context) error {
	records, err := m.repository.ListActiveLoginStates(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if ctx.Err() != nil {
			return nil
		}
		m.Ensure(ctx, record)
	}
	m.seedStopped(ctx)
	return nil
}

// seedStopped restores terminal snapshots without starting account connections.
func (m *Manager) seedStopped(ctx context.Context) {
	records, err := m.repository.ListStoppedLoginStates(ctx)
	if err != nil {
		log.Printf("WA long connection restore stopped failed: %v", shared.SanitizeLogError(err))
		return
	}
	for _, record := range records {
		if ctx.Err() != nil {
			return
		}
		m.seedStoppedEntry(record)
	}
}
