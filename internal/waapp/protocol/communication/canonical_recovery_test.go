package communication

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"testing/synctest"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestCanonicalRecoveryNonceRequiresServerAndUseCase(t *testing.T) {
	for _, tag := range []string{"recovery_nonce", "account_recovery_nonce"} {
		t.Run(tag, func(t *testing.T) {
			node := wire.ChatdNode{Tag: "notification", Attrs: map[string]string{"from": "s.whatsapp.net"}, Content: []wire.ChatdNode{
				{Tag: tag, Attrs: map[string]string{"use_case": "11", "code": "synthetic-nonce"}, Content: []byte("synthetic-nonce")},
				{Tag: "use_case", Content: []byte("11")},
			}}
			if canonicalNonceFromNode(node) != "synthetic-nonce" {
				t.Fatal("native recovery notification was discarded")
			}
			node.Attrs["from"] = "12345@s.whatsapp.net"
			if canonicalNonceFromNode(node) != "" {
				t.Fatal("contact-supplied recovery nonce was accepted")
			}
			node.Attrs["from"] = "s.whatsapp.net"
			children := node.Content.([]wire.ChatdNode)
			children[0].Attrs["use_case"] = "547"
			children[1].Content = []byte("547")
			if canonicalNonceFromNode(node) != "" {
				t.Fatal("another use case's nonce was accepted")
			}
		})
	}
}

func TestCanonicalRecoveryWaitPreservesInterleavedReceipts(t *testing.T) {
	client, peer := net.Pipe()
	defer peer.Close()
	keyA, keyB := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	transport := func(conn net.Conn, send, receive []byte) ChatdTransport {
		return ChatdTransport{Rw: bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)), Codec: wire.NewBinaryNodeCodec(), MaxFrameBytes: DefaultChatdMaxFrame, Keys: &wire.ChatdTransportKeys{SendKey: send, RecvKey: receive}}
	}
	connection := NewConnection(&Engine{engineCore: &engineCore{clock: shared.SystemClock{}}}, ConnectionOptions{Context: t.Context()})
	defer connection.Close()
	session := &ChatdSession{Conn: client, PhoneJID: "12025550123@s.whatsapp.net", Transport: transport(client, keyA, keyB)}
	connection.installSession(session)
	server := transport(peer, keyB, keyA)
	done := make(chan error, 1)
	go func() {
		for _, node := range []wire.ChatdNode{
			{Tag: "receipt", Attrs: map[string]string{"from": "12025550124@s.whatsapp.net", "type": "read", "id": "fixture-receipt", "t": "1700000000"}},
			{Tag: "notification", Attrs: map[string]string{"from": "s.whatsapp.net", "type": "account_sync", "id": "fixture-nonce"}, Content: []wire.ChatdNode{{Tag: "recovery_nonce", Attrs: map[string]string{"code": "synthetic-nonce", "use_case": "11"}}}},
		} {
			if err := server.SendNode(node); err != nil {
				done <- err
				return
			}
			if _, err := server.ReadNode(); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	nonce, err := (lockedIQSender{connection}).WaitCanonicalNonce(t.Context())
	if err != nil || nonce != "synthetic-nonce" || len(connection.PendingUp.Receipts) != 1 || connection.Session != session || session.canonicalNonce != "" {
		t.Fatal("recovery lost a receipt, reused the nonce or replaced the account connection", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalRecoveryWaitIsCancelable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, peer := net.Pipe()
		defer peer.Close()
		connection := NewConnection(&Engine{engineCore: &engineCore{clock: shared.SystemClock{}}}, ConnectionOptions{Context: t.Context()})
		defer connection.Close()
		connection.installSession(&ChatdSession{Conn: client, Transport: ChatdTransport{Rw: bufio.NewReadWriter(bufio.NewReader(client), bufio.NewWriter(client)), MaxFrameBytes: DefaultChatdMaxFrame}})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, err := (lockedIQSender{connection}).WaitCanonicalNonce(ctx); done <- err }()
		synctest.Wait()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("recovery nonce wait ignored cancellation", err)
		}
	})
}
