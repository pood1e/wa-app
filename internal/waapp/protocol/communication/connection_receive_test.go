package communication

import (
	"bufio"
	"bytes"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestReceiverRetainsReceiptsWhenInteractiveWorkPreemptsRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine, _ := newHistoricalMessageEngine(t, newMessageParty(t), newMessageParty(t))
		client, peer := net.Pipe()
		defer peer.Close()
		keyA, keyB := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
		transport := func(conn net.Conn, send, receive []byte) ChatdTransport {
			return ChatdTransport{Rw: bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)), Codec: wire.NewBinaryNodeCodec(), MaxFrameBytes: DefaultChatdMaxFrame, Keys: &wire.ChatdTransportKeys{SendKey: send, RecvKey: receive}}
		}
		connection := NewConnection(engine, ConnectionOptions{Context: t.Context()})
		defer connection.Close()
		connection.installSession(&ChatdSession{Conn: client, PhoneJID: "12025550123@s.whatsapp.net", Transport: transport(client, keyA, keyB)})
		done := make(chan wacore.EngineMessageBatchResult, 1)
		go func() {
			done <- connection.ReceiveMessageBatch(t.Context(), wacore.EngineMessageInput{ClientProfileID: "current", WaitTimeout: time.Minute})
		}()
		server := transport(peer, keyB, keyA)
		sent := make(chan error, 1)
		go func() {
			err := server.SendNode(wire.ChatdNode{Tag: "receipt", Attrs: map[string]string{"from": "12025550124@s.whatsapp.net", "type": "read", "id": "fixture", "t": "1700000000"}})
			if err == nil {
				_, err = server.ReadNode()
			}
			sent <- err
		}()
		if err := <-sent; err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		read := connection.activeRead.Load()
		if read == nil {
			t.Fatal("receiver completed before the interactive operation arrived")
		}
		if err := connection.LockInteractiveIQLocked(t.Context()); err != nil {
			t.Fatal(err)
		}
		connection.UnlockInteractiveIQLocked()
		result := <-done
		if result.Err != nil || len(result.Receipts) != 1 || result.Receipts[0].GetProviderMessageId() != "fixture" {
			t.Fatal("preempting the next read lost an already received receipt", result.Err)
		}
		if !read.Preempted.Load() || connection.Session == nil || connection.ownerCtx.Err() != nil {
			t.Fatal("preempting the receiver revoked a healthy account connection")
		}
	})
}

func TestInteractiveIQResumesFrameInterruptedByReceiverPreemption(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, peer := net.Pipe()
		defer peer.Close()
		keyA, keyB := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
		transport := func(conn net.Conn, send, receive []byte) ChatdTransport {
			return ChatdTransport{Rw: bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)), Codec: wire.NewBinaryNodeCodec(), MaxFrameBytes: DefaultChatdMaxFrame, Keys: &wire.ChatdTransportKeys{SendKey: send, RecvKey: receive}}
		}
		connection := NewConnection(&Engine{engineCore: &engineCore{clock: shared.SystemClock{}}}, ConnectionOptions{Context: t.Context()})
		defer connection.Close()
		session := &ChatdSession{Conn: client, Transport: transport(client, keyA, keyB)}
		connection.installSession(session)
		if err := connection.operations.Acquire(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		received := make(chan error, 1)
		go func() {
			_, _, _, _, err := connection.ReceiveBatchWithActiveReadLocked(t.Context(), session, wacore.EngineMessageInput{WaitTimeout: time.Minute})
			connection.operations.Release(1)
			received <- err
		}()
		server := transport(peer, keyB, keyA)
		encoded, err := server.Codec.EncodeNode(wire.ChatdNode{Tag: "presence", Attrs: map[string]string{"from": "12025550124@s.whatsapp.net", "type": "available"}})
		if err != nil {
			t.Fatal(err)
		}
		ciphertext, err := server.Keys.Encrypt(append([]byte{0}, encoded...))
		if err != nil {
			t.Fatal(err)
		}
		header, err := wire.ChatdInt24(len(ciphertext))
		if err != nil {
			t.Fatal(err)
		}
		frame := append(header, ciphertext...)
		partial, sent := make(chan struct{}), make(chan error, 1)
		go func() {
			_, err := peer.Write(frame[:2])
			close(partial)
			if err == nil {
				_, err = server.ReadNode()
			}
			if err == nil {
				_, err = peer.Write(frame[2:])
			}
			if err == nil {
				err = server.SendNode(wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": "fixture", "type": "result"}})
			}
			sent <- err
		}()
		<-partial
		response, _, err := connection.SendIQ(t.Context(), state.NativeState{}, "identity", "version", wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": "fixture", "type": "get"}}, "fixture timeout")
		if err != nil || response.Attrs["id"] != "fixture" || len(connection.PendingUp.Presences) != 1 {
			t.Fatal("interactive IQ lost the partial frame, cipher counter or presence update", err)
		}
		if err := <-received; err != nil {
			t.Fatal("preempted receiver reported an operation failure", err)
		}
		if err := <-sent; err != nil {
			t.Fatal(err)
		}
	})
}
