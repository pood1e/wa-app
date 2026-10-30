package runtime

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/shared"
)

var errMemoryRuntimeClosed = errors.New("memory runtime is closed")

// MemoryRuntime owns process-local caches, liveness markers and account leases.
type MemoryRuntime struct {
	mu     sync.Mutex
	values map[memoryRef]memoryValue
	leases map[string]memoryLease
	closed bool
	stop   chan struct{}
	done   chan struct{}
}

type memoryRef struct{ scope, ref string }

type memoryValue struct {
	data      []byte
	expiresAt time.Time
}

type memoryLease struct {
	holder    string
	expiresAt time.Time
}

// NewMemoryRuntime starts expiration cleanup. The caller must call Close.
func NewMemoryRuntime() *MemoryRuntime {
	r := &MemoryRuntime{
		values: make(map[memoryRef]memoryValue),
		leases: make(map[string]memoryLease),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go r.cleanExpired()
	return r
}

func (r *MemoryRuntime) Close() error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		clear(r.values)
		clear(r.leases)
		close(r.stop)
	}
	r.mu.Unlock()
	<-r.done
	return nil
}

func (r *MemoryRuntime) SaveTransientState(ctx context.Context, ref string, data []byte, ttl time.Duration) error {
	if strings.TrimSpace(ref) == "" {
		return errors.New("transient state ref is required")
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return r.save(ctx, memoryRef{"transient", strings.TrimSpace(ref)}, data, ttl)
}

func (r *MemoryRuntime) GetTransientState(ctx context.Context, ref string) ([]byte, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, errors.New("transient state ref is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	key := memoryRef{"transient", strings.TrimSpace(ref)}
	value, ok := r.values[key]
	if !ok || !time.Now().Before(value.expiresAt) {
		delete(r.values, key)
		return nil, errors.New("transient state ref not found")
	}
	return bytes.Clone(value.data), nil
}

func (r *MemoryRuntime) DeleteTransientState(ctx context.Context, ref string) error {
	return r.delete(ctx, memoryRef{"transient", strings.TrimSpace(ref)})
}

func (r *MemoryRuntime) ClaimLease(ctx context.Context, key, holder string, ttl time.Duration) (bool, error) {
	key, holder = strings.TrimSpace(key), strings.TrimSpace(holder)
	if key == "" || holder == "" {
		return false, errors.New("lease key and holder are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.check(ctx); err != nil {
		return false, err
	}
	now := time.Now()
	lease, ok := r.leases[key]
	if ok && now.Before(lease.expiresAt) && lease.holder != holder {
		return false, nil
	}
	r.leases[key] = memoryLease{holder, now.Add(shared.NormalizeLeaseTTL(ttl))}
	return true, nil
}

func (r *MemoryRuntime) RenewLease(ctx context.Context, key, holder string, ttl time.Duration) (bool, error) {
	key, holder = strings.TrimSpace(key), strings.TrimSpace(holder)
	if key == "" || holder == "" {
		return false, errors.New("lease key and holder are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.check(ctx); err != nil {
		return false, err
	}
	now := time.Now()
	lease, ok := r.leases[key]
	if !ok || !now.Before(lease.expiresAt) || lease.holder != holder {
		return false, nil
	}
	r.leases[key] = memoryLease{holder, now.Add(shared.NormalizeLeaseTTL(ttl))}
	return true, nil
}

func (r *MemoryRuntime) ReleaseLease(ctx context.Context, key, holder string) error {
	key, holder = strings.TrimSpace(key), strings.TrimSpace(holder)
	if key == "" || holder == "" {
		return errors.New("lease key and holder are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.check(ctx); err != nil {
		return err
	}
	if r.leases[key].holder == holder {
		delete(r.leases, key)
	}
	return nil
}

func (r *MemoryRuntime) OpenSessionLease(ctx context.Context, sessionID string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return r.save(ctx, memoryRef{"session", strings.TrimSpace(sessionID)}, []byte("open"), ttl)
}

func (r *MemoryRuntime) CloseSessionLease(ctx context.Context, sessionID string) error {
	return r.delete(ctx, memoryRef{"session", strings.TrimSpace(sessionID)})
}

func (r *MemoryRuntime) save(ctx context.Context, key memoryRef, data []byte, ttl time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.check(ctx); err != nil {
		return err
	}
	r.values[key] = memoryValue{bytes.Clone(data), time.Now().Add(ttl)}
	return nil
}

func (r *MemoryRuntime) delete(ctx context.Context, key memoryRef) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.check(ctx); err != nil {
		return err
	}
	delete(r.values, key)
	return nil
}

// check requires mu to be held.
func (r *MemoryRuntime) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.closed {
		return errMemoryRuntimeClosed
	}
	return nil
}

func (r *MemoryRuntime) cleanExpired() {
	defer close(r.done)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			r.mu.Lock()
			now := time.Now()
			for key, value := range r.values {
				if !now.Before(value.expiresAt) {
					delete(r.values, key)
				}
			}
			for key, lease := range r.leases {
				if !now.Before(lease.expiresAt) {
					delete(r.leases, key)
				}
			}
			r.mu.Unlock()
		}
	}
}
