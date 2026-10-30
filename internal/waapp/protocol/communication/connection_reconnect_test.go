package communication

import (
	"context"
	"net"
	"testing"
	"testing/synctest"
)

func TestReconnectPreemptsReaderWithoutRevokingOwnership(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, peer := net.Pipe()
		defer peer.Close()
		connection := NewConnection(&Engine{}, ConnectionOptions{Context: t.Context()})
		connection.installSession(&ChatdSession{Conn: client})
		defer connection.Close()
		reading, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan struct{})
		if err := connection.operations.Acquire(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		read := &LongConnectionActiveRead{Cancel: cancel}
		connection.activeRead.Store(read)
		go func() {
			<-reading.Done()
			connection.FinishActiveReadLocked(read)
			close(done)
			connection.operations.Release(1)
		}()
		if err := connection.Reconnect(t.Context()); err != nil {
			t.Fatal(err)
		}
		if connection.Session != nil || connection.Closed || connection.ownerCtx.Err() != nil {
			t.Fatal("proxy refresh retained transport or revoked account ownership")
		}
		if _, err := peer.Write([]byte{1}); err == nil {
			t.Fatal("old transport remained open")
		}
		if err := connection.LockInteractiveIQLocked(t.Context()); err != nil {
			t.Fatal("proxy refresh prevented subsequent interactive work")
		}
		connection.UnlockInteractiveIQLocked()
	})
}
