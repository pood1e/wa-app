package communication

import (
	"context"
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"

	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type blockedState struct{ started chan struct{} }

func (blockedState) ListRetiredProtocolProfiles(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s blockedState) GetProtocolState(ctx context.Context, _ string) (*protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState, error) {
	close(s.started)
	<-ctx.Done()
	return nil, nil, ctx.Err()
}
func (s blockedState) SaveProtocolState(context.Context, string, *protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState) error {
	return errors.New("unexpected save")
}

func TestConnectionCloseCancelsInteractiveOperation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		engine, err := New(blockedState{started}, "", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer engine.CloseIdleConnections()
		connection := NewConnection(engine, ConnectionOptions{Context: t.Context()})
		done := make(chan wacore.EngineMessageSendResult, 1)
		go func() { done <- connection.SendMessage(context.Background(), wacore.EngineMessageSendInput{}) }()
		<-started
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
		if result := <-done; result.Err == nil {
			t.Fatal("closing ownership left an interactive operation running")
		}
	})
}

func TestInteractiveOperationPreemptsRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		connection := NewConnection(&Engine{}, ConnectionOptions{})
		reading, cancelRead := context.WithCancel(t.Context())
		done := make(chan struct{})
		if err := connection.operations.Acquire(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		read := &LongConnectionActiveRead{Cancel: cancelRead}
		connection.activeRead.Store(read)
		go func() {
			<-reading.Done()
			connection.FinishActiveReadLocked(read)
			close(done)
			connection.operations.Release(1)
		}()
		if err := connection.LockInteractiveIQLocked(t.Context()); err != nil {
			t.Fatal(err)
		}
		if connection.activeRead.Load() != nil {
			t.Fatal("interactive operation raced with a network reader")
		}
		connection.UnlockInteractiveIQLocked()
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
		if err := connection.LockInteractiveIQLocked(t.Context()); err == nil {
			connection.UnlockInteractiveIQLocked()
			t.Fatal("closed connection accepted new work")
		}
	})
}

func TestClosedConnectionDoesNotReopen(t *testing.T) {
	connection := NewConnection(&Engine{}, ConnectionOptions{})
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if result := connection.SendMessage(t.Context(), wacore.EngineMessageSendInput{}); result.Err == nil {
		t.Fatal("closed sender accepted work")
	}
	if result := connection.SendMessageReceipts(t.Context(), wacore.EngineMessageReceiptInput{}); result.Err == nil {
		t.Fatal("read receipt opened an independent connection")
	}
	if result := connection.DecryptMessage(t.Context(), wacore.EngineDecryptInput{}); result.Err == nil {
		t.Fatal("closed connection mutated protocol state")
	}
}

func TestConnectionCloseInterruptsBlockedWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, peer := net.Pipe()
		defer peer.Close()
		connection := NewConnection(&Engine{}, ConnectionOptions{})
		connection.installSession(&ChatdSession{Conn: client})
		entered := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			if err := connection.operations.Acquire(t.Context(), 1); err != nil {
				done <- err
				return
			}
			defer connection.operations.Release(1)
			close(entered)
			_, err := client.Write([]byte{1})
			done <- err
		}()
		<-entered
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err == nil {
			t.Fatal("ownership release left a blocked socket write running")
		}
	})
}

type deadlineConnection struct {
	net.Conn
	entered, release chan struct{}
}

func (c deadlineConnection) SetDeadline(deadline time.Time) error {
	if !deadline.IsZero() && !deadline.After(time.Now()) {
		close(c.entered)
		<-c.release
	}
	return nil
}

func TestReadCancellationJoinsBeforeNextOperation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := deadlineConnection{entered: make(chan struct{}), release: make(chan struct{})}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		stop, err := (&ChatdSession{Conn: conn}).bindDeadline(ctx, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		<-conn.entered
		stopped := make(chan struct{})
		go func() { stop(); close(stopped) }()
		synctest.Wait()
		select {
		case <-stopped:
			t.Fatal("next operation can race with the previous read deadline")
		default:
		}
		close(conn.release)
		<-stopped
	})
}
