package wire

import (
	"crypto/sha256"
	"encoding/hex"
)

func (b *NativeResponseBody) Close() error {
	err := b.ReadCloser.Close()
	if b.Conn != nil {
		if closeErr := b.Conn.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func EncHash(enc string) string {
	sum := sha256.Sum256([]byte(enc))
	return hex.EncodeToString(sum[:])
}
