package wire

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strconv"
	"testing"
)

type interruptedFrameReader struct {
	*bytes.Reader
	position, interruptAt int
	interrupted           bool
}

func (r *interruptedFrameReader) Read(p []byte) (int, error) {
	if !r.interrupted {
		if r.position == r.interruptAt {
			r.interrupted = true
			return 0, os.ErrDeadlineExceeded
		}
		p = p[:min(len(p), r.interruptAt-r.position)]
	}
	n, err := r.Reader.Read(p)
	r.position += n
	return n, err
}

func TestFrameReaderResumesPartialHeaderAndPayload(t *testing.T) {
	first, second := []byte("first ciphertext"), []byte("next ciphertext")
	var raw bytes.Buffer
	for _, payload := range [][]byte{first, second} {
		if err := ChatdWriteFrame(bufio.NewWriter(&raw), payload); err != nil {
			t.Fatal(err)
		}
	}
	for _, position := range []int{1, 2, 3, 4, len(first) + 2} {
		t.Run(strconv.Itoa(position), func(t *testing.T) {
			reader := bufio.NewReader(&interruptedFrameReader{Reader: bytes.NewReader(raw.Bytes()), interruptAt: position})
			var frames ChatdFrameReader
			if _, err := frames.Read(reader, 1024); !errors.Is(err, os.ErrDeadlineExceeded) {
				t.Fatal("partial frame did not report the interrupted read")
			}
			for _, want := range [][]byte{first, second} {
				got, err := frames.Read(reader, 1024)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("resumed frame = %q, error = %v, want %q", got, err, want)
				}
			}
		})
	}
}
