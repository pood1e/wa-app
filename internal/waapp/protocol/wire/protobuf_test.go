package wire_test

import (
	"testing"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func FuzzProtobufFields(f *testing.F) {
	f.Add([]byte{0x12, 1, 0})
	f.Add([]byte{0x12, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1})
	f.Add([]byte{0, 0})
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = wire.ParsePBFields(data)
	})
}
