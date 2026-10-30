package wire

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

func RandomBytes(length int) []byte {
	out := make([]byte, length)
	_, _ = rand.Read(out)
	return out
}

func NewUUIDString() string {
	raw := RandomBytes(16)
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:])
}

func B64u(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func NativeAppVersionFromUserAgent(userAgent string) string {
	userAgent = strings.TrimSpace(userAgent)
	const prefix = "WhatsApp/"
	if !strings.HasPrefix(userAgent, prefix) {
		return ""
	}
	version := strings.TrimPrefix(userAgent, prefix)
	if idx := strings.IndexByte(version, ' '); idx >= 0 {
		version = version[:idx]
	}
	return strings.TrimSpace(version)
}
