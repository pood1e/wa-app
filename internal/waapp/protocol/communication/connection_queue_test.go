package communication

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestQueuedConnectionOperationsHonorCallerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		connection := NewConnection(&Engine{}, ConnectionOptions{Context: t.Context()})
		if err := connection.operations.Acquire(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		defer connection.operations.Release(1)
		defer connection.cancel()
		operations := []struct {
			name string
			run  func(context.Context) error
		}{
			{"profile", func(ctx context.Context) error {
				return connection.ApplyAccountSettings(ctx, wacore.EngineAccountSettingsInput{}).Err
			}},
			{"picture", func(ctx context.Context) error {
				return connection.ResolveContactProfilePicture(ctx, wacore.EngineContactProfilePictureInput{}).Err
			}},
			{"contacts", func(ctx context.Context) error {
				return connection.ResolveContacts(ctx, wacore.EngineContactResolveInput{}).Err
			}},
			{"send", func(ctx context.Context) error {
				return connection.SendMessage(ctx, wacore.EngineMessageSendInput{}).Err
			}},
			{"decrypt", func(ctx context.Context) error {
				return connection.DecryptMessage(ctx, wacore.EngineDecryptInput{}).Err
			}},
			{"receipt", func(ctx context.Context) error {
				return connection.SendMessageReceipts(ctx, wacore.EngineMessageReceiptInput{}).Err
			}},
			{"heartbeat", func(ctx context.Context) error {
				return connection.CheckLoginState(ctx, wacore.EngineLoginCheckInput{}).Err
			}},
			{"receive", func(ctx context.Context) error {
				return connection.ReceiveMessageBatch(ctx, wacore.EngineMessageInput{}).Err
			}},
			{"group", func(ctx context.Context) error { _, err := connection.GetGroup(ctx, "fixture@g.us"); return err }},
			{"create_group", func(ctx context.Context) error {
				_, err := connection.CreateGroup(ctx, &waappv1.CreateGroupRequest{})
				return err
			}},
			{"presence", func(ctx context.Context) error {
				return connection.SubscribePresence(ctx, "12025550124@s.whatsapp.net", "Fixture")
			}},
			{"reconnect", connection.Reconnect},
		}
		for _, operation := range operations {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			err := operation.run(ctx)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%s: queued operation returned %v, want deadline exceeded", operation.name, err)
			}
			if connection.iqWaiters.Load() != 0 || connection.ownerCtx.Err() != nil {
				t.Fatalf("%s: cancelled waiter retained admission or revoked ownership", operation.name)
			}
		}
	})
}

func TestConnectionBoundsOperationsWithoutCallerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		connection := NewConnection(&Engine{}, ConnectionOptions{Context: t.Context()})
		if err := connection.operations.Acquire(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		defer connection.operations.Release(1)
		defer connection.cancel()
		result := connection.ResolveContacts(context.Background(), wacore.EngineContactResolveInput{})
		if !errors.Is(result.Err, context.DeadlineExceeded) {
			t.Fatal("operation without a caller deadline did not exhaust its finite budget")
		}
	})
}

func TestAlreadyCancelledOperationDoesNotInterruptReceiver(t *testing.T) {
	connection := NewConnection(&Engine{}, ConnectionOptions{Context: t.Context()})
	defer connection.cancel()
	read, readCtx := connection.StartActiveReadLocked(t.Context())
	defer connection.FinishActiveReadLocked(read)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := connection.LockInteractiveIQLocked(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("already cancelled operation acquired admission")
	}
	if readCtx.Err() != nil {
		t.Fatal("already cancelled operation interrupted a healthy receiver")
	}
}

func TestQueuedInteractiveOperationPreemptsReaderStartingAfterAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		connection := NewConnection(&Engine{}, ConnectionOptions{Context: t.Context()})
		defer connection.cancel()
		if err := connection.operations.Acquire(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			err := connection.LockInteractiveIQLocked(t.Context())
			if err == nil {
				connection.UnlockInteractiveIQLocked()
			}
			done <- err
		}()
		synctest.Wait()
		read, readCtx := connection.StartActiveReadLocked(t.Context())
		if readCtx.Err() == nil || !read.Preempted.Load() {
			t.Fatal("a new receive read ignored the queued interactive operation")
		}
		connection.FinishActiveReadLocked(read)
		connection.operations.Release(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}
