package communication

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"testing"
	"testing/synctest"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestSessionCancellationInterruptsBlockedReadsAndWrites(t *testing.T) {
	operations := []struct {
		name string
		run  func(context.Context, *ChatdSession) error
	}{
		{"receive", func(ctx context.Context, session *ChatdSession) error {
			_, _, _, _, err := session.ReceiveBatch(ctx, wacore.EngineMessageInput{WaitTimeout: time.Minute}, shared.SystemClock{})
			return err
		}},
		{"iq", func(ctx context.Context, session *ChatdSession) error {
			_, _, _, err := session.SendIQ(ctx, wacore.EngineMessageInput{}, wire.ChatdNode{Tag: "iq"}, time.Minute, "fixture timeout")
			return err
		}},
		{"message", func(ctx context.Context, session *ChatdSession) error {
			_, _, err := session.SendMessageWithAck(ctx, wacore.EngineMessageInput{}, wire.ChatdNode{Tag: "message"}, "fixture", time.Minute)
			return err
		}},
		{"receipt", func(ctx context.Context, session *ChatdSession) error {
			return session.SendMessageReceipts(ctx, []wacore.EngineMessageReceipt{{ChatJID: "12025550124@s.whatsapp.net", ProviderMessageID: "fixture"}}, waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_READ).Err
		}},
		{"protocol_node", func(ctx context.Context, session *ChatdSession) error {
			return sendProtocolNode(ctx, session, wire.ChatdNode{Tag: "receipt"})
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				conn, peer := net.Pipe()
				defer conn.Close()
				defer peer.Close()
				session := &ChatdSession{Conn: conn, Transport: ChatdTransport{
					Rw: bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)), Codec: wire.NewBinaryNodeCodec(), MaxFrameBytes: DefaultChatdMaxFrame,
					Keys: &wire.ChatdTransportKeys{SendKey: bytes.Repeat([]byte{1}, 32), RecvKey: bytes.Repeat([]byte{2}, 32)},
				}}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- operation.run(ctx, session) }()
				synctest.Wait()
				cancel()
				if err := <-done; err == nil {
					t.Fatal("cancelled operation completed its blocked socket I/O")
				}
			})
		})
	}
}
