package communication

import (
	"bufio"
	"errors"
	"net"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

type ChatdTransport struct {
	Rw            *bufio.ReadWriter
	Keys          *wire.ChatdTransportKeys
	Codec         *wire.BinaryNodeCodec
	MaxFrameBytes int
	frames        wire.ChatdFrameReader
	failure       error
}

func (t *ChatdTransport) ReadNode() (wire.ChatdNode, error) {
	if t.failure != nil {
		return wire.ChatdNode{}, t.failure
	}
	for {
		ciphertext, err := t.frames.Read(t.Rw.Reader, t.MaxFrameBytes)
		if err != nil {
			var networkError net.Error
			if errors.As(err, &networkError) && networkError.Timeout() {
				return wire.ChatdNode{}, err
			}
			return wire.ChatdNode{}, t.fail(err)
		}
		plaintext, err := t.Keys.Decrypt(ciphertext)
		if err != nil {
			return wire.ChatdNode{}, t.fail(err)
		}
		if len(plaintext) == 0 {
			continue
		}
		node, err := t.Codec.DecodeNodePayload(plaintext)
		if err != nil {
			return wire.ChatdNode{}, t.fail(err)
		}
		return node, nil
	}
}

func (t *ChatdTransport) SendNode(node wire.ChatdNode) error {
	if t.failure != nil {
		return t.failure
	}
	payload, err := t.Codec.EncodeNode(node)
	if err != nil {
		return err
	}
	ciphertext, err := t.Keys.Encrypt(append([]byte{0}, payload...))
	if err != nil {
		return t.fail(err)
	}
	if err := wire.ChatdWriteFrame(t.Rw.Writer, ciphertext); err != nil {
		// A failed write may have sent a partial frame and has advanced the Noise counter.
		return t.fail(err)
	}
	return nil
}

func (t *ChatdTransport) fail(err error) error {
	if t.failure == nil {
		t.failure = err
	}
	return t.failure
}
