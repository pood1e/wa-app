package communication

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/hkdf"
	"google.golang.org/protobuf/encoding/protowire"

	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/waproto"
)

func DecryptNativeAppStateMutation(state *pstate.NativeState, collectionName string, mutation NativeAppStateMutation) ([]byte, bool) {
	if state == nil || len(mutation.Record.Value) < 48 || collectionName == "" {
		return nil, false
	}
	keyData, ok := NativeAppStateKeyDataForID(state, mutation.KeyID)
	if !ok {
		return nil, false
	}
	keys, ok := DeriveNativeAppStateMutationKeys(keyData)
	if !ok {
		return nil, false
	}
	cipherValue := mutation.Record.Value
	macOffset := len(cipherValue) - 32
	iv := cipherValue[:16]
	ciphertext := cipherValue[16:macOffset]
	dataMAC := cipherValue[macOffset:]
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, false
	}
	opByte, ok := NativeAppStateOperationByte(mutation.Operation)
	if !ok {
		return nil, false
	}
	macPayload := cipherValue[:macOffset]
	if !ValidNativeAppStateMutationMAC(keys.ValueMAC, opByte, macPayload, dataMAC, mutation.KeyID, collectionName) {
		return nil, false
	}
	block, err := aes.NewCipher(keys.ValueEncryption)
	if err != nil {
		return nil, false
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	plaintext, ok = Pkcs7Unpad(plaintext, aes.BlockSize)
	if !ok {
		return nil, false
	}
	index := NativeAppStateSyncActionIndex(plaintext)
	if len(index) == 0 {
		return nil, false
	}
	mac := hmac.New(sha256.New, keys.IndexMAC)
	_, _ = mac.Write(index)
	if !hmac.Equal(mutation.Record.Index, mac.Sum(nil)) {
		return nil, false
	}
	return plaintext, true
}

type NativeAppStateMutationKeys struct {
	IndexMAC        []byte
	ValueEncryption []byte
	ValueMAC        []byte
	SnapshotMAC     []byte
	PatchMAC        []byte
}

func DeriveNativeAppStateMutationKeys(keyData []byte) (NativeAppStateMutationKeys, bool) {
	if !pstate.ValidNativeAppStateKeyData(keyData) {
		return NativeAppStateMutationKeys{}, false
	}
	reader := hkdf.New(sha256.New, keyData, nil, []byte(WaAppStateMutationKeyInfo))
	out := make([]byte, 160)
	if _, err := io.ReadFull(reader, out); err != nil {
		return NativeAppStateMutationKeys{}, false
	}
	return NativeAppStateMutationKeys{
		IndexMAC:        append([]byte{}, out[:32]...),
		ValueEncryption: append([]byte{}, out[32:64]...),
		ValueMAC:        append([]byte{}, out[64:96]...),
		SnapshotMAC:     append([]byte{}, out[96:128]...),
		PatchMAC:        append([]byte{}, out[128:160]...),
	}, true
}

func NativeAppStateKeyDataForID(state *pstate.NativeState, keyID []byte) ([]byte, bool) {
	if state == nil || len(keyID) == 0 {
		return nil, false
	}
	key := state.AppState.Keys[pwire.B64u(keyID)]
	if !key.Valid() {
		return nil, false
	}
	keyData, err := pwire.DecodeB64Any(key.KeyData)
	if err != nil || !pstate.ValidNativeAppStateKeyData(keyData) {
		return nil, false
	}
	return keyData, true
}

func NativeAppStateOperationByte(operation uint64) (byte, bool) {
	switch operation {
	case 0:
		return 1, true
	case 1:
		return 2, true
	default:
		return 0, false
	}
}

func NativeAppStateSyncActionIndex(raw []byte) []byte {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 8)
	if !ok {
		return nil
	}
	for _, field := range fields {
		if field.Kind == protowire.BytesType && field.Number == 1 && len(pwire.WaSyncdIndexValues(field.Value)) > 0 {
			return append([]byte{}, field.Value...)
		}
	}
	return nil
}

func Pkcs7Unpad(raw []byte, blockSize int) ([]byte, bool) {
	if len(raw) == 0 || blockSize <= 0 || len(raw)%blockSize != 0 {
		return nil, false
	}
	pad := int(raw[len(raw)-1])
	if pad == 0 || pad > blockSize || pad > len(raw) {
		return nil, false
	}
	for _, value := range raw[len(raw)-pad:] {
		if int(value) != pad {
			return nil, false
		}
	}
	return raw[:len(raw)-pad], true
}

func WaAppStatePatchDebugCollectionName(raw []byte) string {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 32)
	if !ok {
		return ""
	}
	for _, field := range fields {
		if field.Kind == protowire.BytesType && field.Number == 4 {
			if value := WaAppStateCollectionName(field.Value); value != "" {
				return value
			}
		}
	}
	return ""
}

func WaAppStateCollectionName(raw []byte) string {
	if len(raw) == 0 || len(raw) > WaAppStateCollectionNameLimit || !utf8.Valid(raw) {
		return ""
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || strings.ContainsRune(value, 0) || strings.ContainsAny(value, "[]{}\n\r\t") {
		return ""
	}
	return value
}

func WaAppStateInflatePayload(raw []byte) ([]byte, bool) {
	if decoded, ok := pwire.WaGunzipPayload(raw); ok {
		return decoded, true
	}
	reader, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, pwire.WaContactDecodedPayloadLimit+1))
	if err != nil || len(data) == 0 || len(data) > pwire.WaContactDecodedPayloadLimit {
		return nil, false
	}
	return data, true
}
