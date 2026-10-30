package communication

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

type failedLoginReader struct{ err error }

func (r failedLoginReader) Read([]byte) (int, error) { return 0, r.err }

func TestChatdLoginRequiresExplicitSuccess(t *testing.T) {
	for _, test := range []struct {
		name  string
		nodes []wire.ChatdNode
		end   error
		want  bool
	}{
		{"success", []wire.ChatdNode{{Tag: "success"}}, io.EOF, true},
		{"features then success", []wire.ChatdNode{{Tag: "stream:features"}, {Tag: "success"}}, io.EOF, true},
		{"authentication failure", []wire.ChatdNode{{Tag: "failure", Attrs: map[string]string{"reason": "401"}}}, io.EOF, false},
		{"stream rejection", []wire.ChatdNode{{Tag: "stream:error", Attrs: map[string]string{"code": "515"}}}, io.EOF, false},
		{"closed stream", []wire.ChatdNode{{Tag: "xmlstreamend"}}, io.EOF, false},
		{"features without success", []wire.ChatdNode{{Tag: "stream:features"}}, io.EOF, false},
		{"timeout without success", nil, os.ErrDeadlineExceeded, false},
		{"unconfirmed frame limit", repeatLoginFeatures(32), io.EOF, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			codec := wire.NewBinaryNodeCodec()
			key := bytes.Repeat([]byte{7}, 32)
			sender := &wire.ChatdTransportKeys{SendKey: key}
			var frames bytes.Buffer
			writer := bufio.NewWriter(&frames)
			for _, node := range test.nodes {
				payload, err := codec.EncodeNode(node)
				if err != nil {
					t.Fatal(err)
				}
				ciphertext, err := sender.Encrypt(append([]byte{0}, payload...))
				if err != nil {
					t.Fatal(err)
				}
				if err := wire.ChatdWriteFrame(writer, ciphertext); err != nil {
					t.Fatal(err)
				}
			}
			reader := io.MultiReader(&frames, failedLoginReader{test.end})
			session := &ChatdSession{Transport: ChatdTransport{Rw: bufio.NewReadWriter(bufio.NewReader(reader), bufio.NewWriter(io.Discard)),
				Keys: &wire.ChatdTransportKeys{RecvKey: key}, Codec: codec, MaxFrameBytes: DefaultChatdMaxFrame}}
			err := session.authenticate()
			if (err == nil) != test.want {
				t.Fatalf("login confirmation: got %v, want success=%t", err, test.want)
			}
			if errors.Is(test.end, os.ErrDeadlineExceeded) && !errors.Is(err, os.ErrDeadlineExceeded) {
				t.Fatal("login confirmation discarded the read timeout")
			}
		})
	}
}

func repeatLoginFeatures(count int) []wire.ChatdNode {
	nodes := make([]wire.ChatdNode, count)
	for i := range nodes {
		nodes[i].Tag = "stream:features"
	}
	return nodes
}
