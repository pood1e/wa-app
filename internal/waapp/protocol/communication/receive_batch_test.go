package communication

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestReceiveBatchUsesFrameArrivalTime(t *testing.T) {
	start := time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		message   bool
		expiresIn time.Duration
		wantOTP   int
	}{
		{name: "fresh old-device code", expiresIn: time.Minute, wantOTP: 1},
		{name: "code expired during read", expiresIn: 2 * time.Second},
		{name: "ordinary message", message: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock := &receiveTestClock{now: start}
			node := wire.ChatdNode{Tag: "notification", Attrs: map[string]string{"id": "otp-stanza", "from": "s.whatsapp.net"}, Content: []wire.ChatdNode{{
				Tag: "wa_old_registration", Attrs: map[string]string{
					"code": "123456", "device_id": "new-device", "expiry_t": strconv.FormatInt(start.Add(test.expiresIn).Unix(), 10),
				},
			}}}
			if test.message {
				node = wire.ChatdNode{Tag: "message", Attrs: map[string]string{"id": "message-stanza", "from": "12345@s.whatsapp.net"}, Content: []wire.ChatdNode{{Tag: "enc", Attrs: map[string]string{"type": "msg", "v": "2"}, Content: []byte{0}}}}
			}
			session := receiveTestSession(t, node, clock)
			messages, _, otps, _, err := session.ReceiveBatch(t.Context(), wacore.EngineMessageInput{
				WAAccountID: "account", ClientProfileID: "profile", RegisteredIdentityID: "identity",
				MessageSessionID: "message-session", WaitTimeout: time.Second, MaxMessages: 1,
			}, clock)
			if err != nil {
				t.Fatal(err)
			}
			if len(otps) != test.wantOTP {
				t.Fatalf("got %d codes, want %d", len(otps), test.wantOTP)
			}
			arrivedAt := start.Add(5 * time.Second)
			if len(otps) > 0 && !otps[0].GetReceivedAt().AsTime().Equal(arrivedAt) {
				t.Fatal("code received_at predates frame arrival")
			}
			if test.message {
				if len(messages) != 1 || !messages[0].GetReceivedAt().AsTime().Equal(arrivedAt) {
					t.Fatal("message received_at does not match frame arrival")
				}
			} else if len(messages) != 0 {
				t.Fatal("code notification produced a chat message")
			}
		})
	}
}

type receiveTestClock struct{ now time.Time }

func (c *receiveTestClock) Now() time.Time { return c.now }

type arrivalReader struct {
	io.Reader
	clock *receiveTestClock
	delay time.Duration
}

func (r *arrivalReader) Read(p []byte) (int, error) {
	r.clock.now = r.clock.now.Add(r.delay)
	r.delay = 0
	return r.Reader.Read(p)
}

func receiveTestSession(t *testing.T, node wire.ChatdNode, clock *receiveTestClock) *ChatdSession {
	t.Helper()
	codec := wire.NewBinaryNodeCodec()
	key := bytes.Repeat([]byte{7}, 32)
	sender := &wire.ChatdTransportKeys{SendKey: key}
	payload, err := codec.EncodeNode(node)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := sender.Encrypt(append([]byte{0}, payload...))
	if err != nil {
		t.Fatal(err)
	}
	var frames bytes.Buffer
	if err := wire.ChatdWriteFrame(bufio.NewWriter(&frames), ciphertext); err != nil {
		t.Fatal(err)
	}
	reader := &arrivalReader{Reader: io.MultiReader(&frames, failedLoginReader{os.ErrDeadlineExceeded}), clock: clock, delay: 5 * time.Second}
	connection, peer := net.Pipe()
	t.Cleanup(func() { _ = connection.Close(); _ = peer.Close() })
	return &ChatdSession{Conn: connection, PhoneJID: "12025550123@s.whatsapp.net", Transport: ChatdTransport{
		Rw:   bufio.NewReadWriter(bufio.NewReader(reader), bufio.NewWriter(io.Discard)),
		Keys: &wire.ChatdTransportKeys{RecvKey: key, SendKey: bytes.Repeat([]byte{8}, 32)}, Codec: codec, MaxFrameBytes: DefaultChatdMaxFrame,
	}}
}
