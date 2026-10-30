package state

import (
	"encoding/hex"

	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func ValidNativeAppStateKeyID(raw []byte) bool {
	return len(raw) == 6
}

func ValidNativeAppStateKeyData(raw []byte) bool {
	return len(raw) >= 16 && len(raw) <= 64
}

func (k NativeAppStateKey) Normalized() NativeAppStateKey {
	keyID, err := pwire.DecodeB64Any(k.KeyID)
	if err != nil || !ValidNativeAppStateKeyID(keyID) {
		k.KeyID = ""
	} else {
		k.KeyID = pwire.B64u(keyID)
	}
	keyData, err := pwire.DecodeB64Any(k.KeyData)
	if err != nil || !ValidNativeAppStateKeyData(keyData) {
		k.KeyData = ""
	} else {
		k.KeyData = pwire.B64u(keyData)
	}
	if k.Fingerprint != "" {
		if fingerprint, err := pwire.DecodeB64Any(k.Fingerprint); err == nil {
			k.Fingerprint = pwire.B64u(fingerprint)
		} else if _, err := hex.DecodeString(k.Fingerprint); err != nil {
			k.Fingerprint = ""
		}
	}
	return k
}

func (k NativeAppStateKey) Valid() bool {
	k = k.Normalized()
	return k.KeyID != "" && k.KeyData != ""
}
