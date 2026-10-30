package wire

import (
	"bytes"
)

func Pkcs7Pad(raw []byte, blockSize int) []byte {
	if blockSize <= 0 {
		return append([]byte{}, raw...)
	}
	pad := blockSize - len(raw)%blockSize
	if pad == 0 {
		pad = blockSize
	}
	out := make([]byte, 0, len(raw)+pad)
	out = append(out, raw...)
	return append(out, bytes.Repeat([]byte{byte(pad)}, pad)...)
}
