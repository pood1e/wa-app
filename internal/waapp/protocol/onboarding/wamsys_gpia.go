package onboarding

import (
	"encoding/base64"

	"golang.org/x/crypto/curve25519"

	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

func NativeGPIARequestHash(state pstate.NativeState) string {
	keySource := pstate.NativeGPIAKeySource(state)
	raw, err := base64.StdEncoding.DecodeString(keySource)
	if err == nil && len(raw) == curve25519.PointSize {
		return base64.RawStdEncoding.EncodeToString(raw)
	}
	return ""
}
