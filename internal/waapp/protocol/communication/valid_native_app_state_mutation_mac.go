package communication

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"io"
	"strings"

	"golang.org/x/crypto/hkdf"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func ValidNativeAppStateMutationMAC(key []byte, opByte byte, data []byte, expected []byte, keyID []byte, collectionName string) bool {
	prefixes := [][]byte{}
	if len(keyID) > 0 {
		prefixes = append(prefixes, append([]byte{opByte}, keyID...))
	}
	if strings.TrimSpace(collectionName) != "" {
		prefixes = append(prefixes, append([]byte{opByte}, []byte(collectionName)...))
	}
	for _, prefix := range prefixes {
		if hmac.Equal(expected, NativeAppStateMAC(key, prefix, data)) {
			return true
		}
	}
	return false
}

func NativeAppStateMAC(key []byte, prefix []byte, data []byte) []byte {
	macInput := make([]byte, 0, len(prefix)+len(data)+8)
	macInput = append(macInput, prefix...)
	macInput = append(macInput, data...)
	macInput = binary.BigEndian.AppendUint64(macInput, uint64(len(prefix)))
	mac := hmac.New(sha512.New, key)
	_, _ = mac.Write(macInput)
	return mac.Sum(nil)[:WaAppStateMACBytes]
}

func NativeAppStateSnapshotMAC(hash []byte, version uint64, name string, key []byte) []byte {
	payload := make([]byte, 0, len(hash)+8+len(name))
	payload = append(payload, hash...)
	payload = binary.BigEndian.AppendUint64(payload, version)
	payload = append(payload, []byte(name)...)
	return NativeAppStateHMACSHA256(payload, key)
}

func NativeAppStatePatchMAC(snapshotMAC []byte, valueMACs [][]byte, version uint64, name string, key []byte) []byte {
	payload := append([]byte{}, snapshotMAC...)
	for _, valueMAC := range valueMACs {
		payload = append(payload, valueMAC...)
	}
	payload = binary.BigEndian.AppendUint64(payload, version)
	payload = append(payload, []byte(name)...)
	return NativeAppStateHMACSHA256(payload, key)
}

func NativeAppStateHMACSHA256(data []byte, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func NativeAppStateLTHash(hash []byte, subtract [][]byte, add [][]byte) ([]byte, error) {
	if len(hash) != WaAppStateHashBytes {
		return nil, AppStatePatchError("WA app-state hash is invalid")
	}
	out := append([]byte{}, hash...)
	var err error
	for _, item := range subtract {
		out, err = NativeAppStateLTHashApply(out, item, -1)
		if err != nil {
			return nil, err
		}
	}
	for _, item := range add {
		out, err = NativeAppStateLTHashApply(out, item, 1)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func NativeAppStateLTHashApply(hash []byte, item []byte, sign int) ([]byte, error) {
	derived := make([]byte, WaAppStateHashBytes)
	reader := hkdf.New(sha256.New, item, nil, []byte(WaAppStateIntegrityInfo))
	if _, err := io.ReadFull(reader, derived); err != nil {
		return nil, err
	}
	out := append([]byte{}, hash...)
	for offset := 0; offset < len(out); offset += 2 {
		left := binary.LittleEndian.Uint16(out[offset:])
		right := binary.LittleEndian.Uint16(derived[offset:])
		if sign < 0 {
			left -= right
		} else {
			left += right
		}
		binary.LittleEndian.PutUint16(out[offset:], left)
	}
	return out, nil
}

func AppStatePatchError(message string) *shared.AppError {
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, message, false)
}
