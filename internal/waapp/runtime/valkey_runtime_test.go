package runtime

import (
	"os"
	"testing"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestValkeyOwnershipAndExpiry(t *testing.T) {
	address := os.Getenv("WA_TEST_VALKEY_URL")
	if address == "" {
		t.Skip("WA_TEST_VALKEY_URL is not set")
	}
	first, err := NewValkeyRuntime(t.Context(), address)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewValkeyRuntime(t.Context(), address)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	key := shared.RandomIDGenerator{}.NewID("test_")
	defer first.ReleaseLease(t.Context(), key, "second")
	granted, err := first.ClaimLease(t.Context(), key, "first", time.Second)
	if err != nil || !granted {
		t.Fatal("initial lease was not acquired")
	}
	granted, err = second.ClaimLease(t.Context(), key, "second", time.Second)
	if err != nil || granted {
		t.Fatal("concurrent owner acquired a live lease")
	}
	if err := second.ReleaseLease(t.Context(), key, "second"); err != nil {
		t.Fatal(err)
	}
	renewed, err := second.RenewLease(t.Context(), key, "second", time.Second)
	if err != nil || renewed {
		t.Fatal("different holder renewed the lease")
	}
	renewed, err = first.RenewLease(t.Context(), key, "first", time.Second)
	if err != nil || !renewed {
		t.Fatal("different holder released the lease")
	}
	if err := first.ReleaseLease(t.Context(), key, "first"); err != nil {
		t.Fatal(err)
	}
	granted, err = second.ClaimLease(t.Context(), key, "second", time.Second)
	if err != nil || !granted {
		t.Fatal("released lease could not change owners")
	}
	ref := key + "/transient"
	if err := first.SaveTransientState(t.Context(), ref, []byte("fixture"), 40*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	data, err := second.GetTransientState(t.Context(), ref)
	if err != nil || string(data) != "fixture" {
		t.Fatal("transient state did not cross clients")
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := second.GetTransientState(t.Context(), ref); err == nil {
		t.Fatal("expired state remained visible")
	}
	if granted, err := first.ClaimLease(t.Context(), "", "", time.Second); err == nil || granted {
		t.Fatal("empty lease was accepted")
	}
}
