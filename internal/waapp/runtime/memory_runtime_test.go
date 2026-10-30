package runtime

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestMemoryRuntimeTransientState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		r := NewMemoryRuntime()
		defer r.Close()
		data := []byte("fixture")
		if err := r.SaveTransientState(ctx, "avatar", data, time.Second); err != nil {
			t.Fatal(err)
		}
		data[0] = 'X'
		got, err := r.GetTransientState(ctx, "avatar")
		if err != nil || string(got) != "fixture" {
			t.Fatalf("retained caller buffer: data=%q, err=%v", got, err)
		}
		got[0] = 'Y'
		got, err = r.GetTransientState(ctx, "avatar")
		if err != nil || string(got) != "fixture" {
			t.Fatalf("returned internal buffer: data=%q, err=%v", got, err)
		}
		if err := r.SaveTransientState(ctx, "default", nil, 0); err != nil {
			t.Fatal(err)
		}
		if err := r.OpenSessionLease(ctx, "session", time.Second); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		time.Sleep(time.Second)
		if _, err := r.GetTransientState(ctx, "avatar"); err == nil {
			t.Fatal("expired state remained visible before cleanup")
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		r.mu.Lock()
		retained := len(r.values)
		r.mu.Unlock()
		if retained != 1 {
			t.Fatalf("expired data retained after cleanup: got %d entries, want 1", retained)
		}
		if _, err := r.GetTransientState(ctx, "default"); err != nil {
			t.Fatalf("default TTL expired early: %v", err)
		}
		if err := r.DeleteTransientState(ctx, "default"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetTransientState(ctx, "default"); err == nil {
			t.Fatal("deleted state remained visible")
		}
	})
}

func TestMemoryRuntimeLeaseOwnership(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		r := NewMemoryRuntime()
		defer r.Close()
		claim := func(holder string, ttl time.Duration, want bool) {
			t.Helper()
			got, err := r.ClaimLease(ctx, "account", holder, ttl)
			if err != nil || got != want {
				t.Fatalf("claim %s: got %v, want %v, err=%v", holder, got, want, err)
			}
		}
		renew := func(holder string, ttl time.Duration, want bool) {
			t.Helper()
			got, err := r.RenewLease(ctx, "account", holder, ttl)
			if err != nil || got != want {
				t.Fatalf("renew %s: got %v, want %v, err=%v", holder, got, want, err)
			}
		}
		claim("first", time.Second, true)
		claim("first", time.Second, true)
		claim("second", time.Second, false)
		renew("second", time.Second, false)
		if err := r.ReleaseLease(ctx, "account", "second"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(750 * time.Millisecond)
		renew("first", 2*time.Second, true)
		time.Sleep(750 * time.Millisecond)
		claim("second", time.Second, false)
		time.Sleep(1250 * time.Millisecond)
		renew("first", time.Second, false)
		claim("second", time.Second, true)
		if err := r.ReleaseLease(ctx, "account", "first"); err != nil {
			t.Fatal(err)
		}
		renew("first", time.Second, false)
		claim("first", time.Second, false)
		if err := r.ReleaseLease(ctx, "account", "second"); err != nil {
			t.Fatal(err)
		}
		claim("first", 0, true)
		time.Sleep(29 * time.Second)
		claim("second", time.Second, false)
		time.Sleep(time.Second)
		claim("second", time.Second, true)
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		r.mu.Lock()
		retained := len(r.leases)
		r.mu.Unlock()
		if retained != 0 {
			t.Fatalf("expired leases retained after cleanup: got %d, want 0", retained)
		}
		if got, err := r.ClaimLease(ctx, "", "", time.Second); err == nil || got {
			t.Fatal("empty lease was accepted")
		}
	})
}

func TestMemoryRuntimeConcurrentOwnership(t *testing.T) {
	r := NewMemoryRuntime()
	defer r.Close()
	var workers sync.WaitGroup
	start := make(chan struct{})
	winners := make(chan string, 32)
	for i := range cap(winners) {
		workers.Go(func() {
			<-start
			holder := strconv.Itoa(i)
			granted, err := r.ClaimLease(t.Context(), "account", holder, time.Minute)
			if err != nil {
				t.Errorf("claim: %v", err)
			}
			if granted {
				winners <- holder
			}
		})
	}
	close(start)
	workers.Wait()
	if len(winners) != 1 {
		t.Fatalf("concurrent holders: got %d, want 1", len(winners))
	}
}

func TestMemoryRuntimeCancellationAndClose(t *testing.T) {
	r := NewMemoryRuntime()
	defer r.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.SaveTransientState(ctx, "avatar", []byte("fixture"), time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled write: %v", err)
	}
	if got, err := r.ClaimLease(ctx, "account", "owner", time.Minute); got || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled claim: granted=%v, err=%v", got, err)
	}
	if _, err := r.GetTransientState(t.Context(), "avatar"); err == nil {
		t.Fatal("canceled write changed state")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveTransientState(t.Context(), "avatar", nil, time.Minute); !errors.Is(err, errMemoryRuntimeClosed) {
		t.Fatalf("write after close: %v", err)
	}
	if got, err := r.ClaimLease(t.Context(), "account", "owner", time.Minute); got || !errors.Is(err, errMemoryRuntimeClosed) {
		t.Fatalf("claim after close: granted=%v, err=%v", got, err)
	}
}
