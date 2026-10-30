package communication

import (
	"context"
	"net"
	"testing"
	"testing/synctest"
)

type handshakeClosingConnection struct {
	net.Conn
	entered, release chan struct{}
}

func (c handshakeClosingConnection) Close() error {
	close(c.entered)
	<-c.release
	return nil
}

func TestHandshakeCancellationJoinsBeforeSessionReuse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := handshakeClosingConnection{entered: make(chan struct{}), release: make(chan struct{})}
		ctx, cancel := context.WithCancel(t.Context())
		stop := CloseChatdConnOnContext(ctx, conn)
		cancel()
		<-conn.entered
		finished := make(chan struct{})
		go func() { stop(); close(finished) }()
		synctest.Wait()
		select {
		case <-finished:
			close(conn.release)
			t.Fatal("session reused while handshake cancellation could still close its socket")
		default:
		}
		close(conn.release)
		<-finished
	})
}
