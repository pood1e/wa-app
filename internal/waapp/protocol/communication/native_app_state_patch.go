package communication

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"

	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

const (
	WaAppStatePushNameCollection = "critical_block"
	WaAppStatePushNameIndex      = "setting_pushName"
	WaAppStatePushNameAPIVersion = 1
	WaAppStateHashBytes          = 128
	WaAppStateMACBytes           = 32
	WaAppStateIntegrityInfo      = "WhatsApp Patch Integrity"
	WaAppStateKeyUnavailable     = "WA app-state key is not available; reconnect the account before changing profile name"
)

func BuildNativePushNamePatch(state *pstate.NativeState, displayName string, timestampMS uint64) (pwire.ChatdNode, pstate.NativeAppStateCollection, error) {
	keyID, keyData, err := SelectedNativeAppStateKey(state)
	if err != nil {
		return pwire.ChatdNode{}, pstate.NativeAppStateCollection{}, err
	}
	keys, ok := DeriveNativeAppStateMutationKeys(keyData)
	if !ok {
		return pwire.ChatdNode{}, pstate.NativeAppStateCollection{}, AppStatePatchError("WA app-state mutation key is invalid")
	}
	current := NormalizedNativeAppStateCollection(state, WaAppStatePushNameCollection)
	index := []byte(`["` + WaAppStatePushNameIndex + `"]`)
	encodedAction := EncodeNativePushNameSyncAction(index, displayName, timestampMS)
	encryptedAction, err := EncryptNativeAppStateValue(encodedAction, keys.ValueEncryption)
	if err != nil {
		return pwire.ChatdNode{}, pstate.NativeAppStateCollection{}, err
	}
	valueMAC, ok := NativeAppStateMutationMAC(keys.ValueMAC, 0, encryptedAction, keyID)
	if !ok {
		return pwire.ChatdNode{}, pstate.NativeAppStateCollection{}, AppStatePatchError("WA app-state mutation operation is invalid")
	}
	indexMAC := NativeAppStateHMACSHA256(index, keys.IndexMAC)
	next, hash, err := NextNativeAppStateCollection(current, indexMAC, valueMAC)
	if err != nil {
		return pwire.ChatdNode{}, pstate.NativeAppStateCollection{}, err
	}
	snapshotMAC := NativeAppStateSnapshotMAC(hash, next.Version, WaAppStatePushNameCollection, keys.SnapshotMAC)
	patchMAC := NativeAppStatePatchMAC(snapshotMAC, [][]byte{valueMAC}, next.Version, WaAppStatePushNameCollection, keys.PatchMAC)
	patch := EncodeNativeSyncdPatch(NativeSyncdPatch{
		KeyID:       keyID,
		IndexMAC:    indexMAC,
		Value:       append(append([]byte{}, encryptedAction...), valueMAC...),
		SnapshotMAC: snapshotMAC,
		PatchMAC:    patchMAC,
	})
	return BuildNativeAppStatePatchIQ("", WaAppStatePushNameCollection, current.Version, patch), next, nil
}

func BuildNativeAppStatePatchIQ(id string, collectionName string, version uint64, patch []byte) pwire.ChatdNode {
	attrs := map[string]string{
		"to":    "s.whatsapp.net",
		"xmlns": "w:sync:app:state",
		"type":  "set",
	}
	if strings.TrimSpace(id) != "" {
		attrs["id"] = id
	}
	return pwire.ChatdNode{
		Tag:   "iq",
		Attrs: attrs,
		Content: []pwire.ChatdNode{{
			Tag: "sync",
			Content: []pwire.ChatdNode{{
				Tag: "collection",
				Attrs: map[string]string{
					"name":            collectionName,
					"version":         fmt.Sprintf("%d", version),
					"return_snapshot": "false",
				},
				Content: []pwire.ChatdNode{{Tag: "patch", Content: patch}},
			}},
		}},
	}
}

func SelectedNativeAppStateKey(state *pstate.NativeState) ([]byte, []byte, error) {
	if state == nil || len(state.AppState.Keys) == 0 {
		return nil, nil, AppStatePatchError(WaAppStateKeyUnavailable)
	}
	keys := make([]pstate.NativeAppStateKey, 0, len(state.AppState.Keys))
	for _, key := range state.AppState.Keys {
		key = key.Normalized()
		if key.Valid() {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil, nil, AppStatePatchError(WaAppStateKeyUnavailable)
	}
	slices.SortFunc(keys, func(left pstate.NativeAppStateKey, right pstate.NativeAppStateKey) int {
		if left.Timestamp != right.Timestamp {
			if left.Timestamp > right.Timestamp {
				return -1
			}
			return 1
		}
		return strings.Compare(left.KeyID, right.KeyID)
	})
	key := keys[0]
	keyID, err := pwire.DecodeB64Any(key.KeyID)
	if err != nil || !pstate.ValidNativeAppStateKeyID(keyID) {
		return nil, nil, AppStatePatchError("WA app-state key id is invalid")
	}
	keyData, err := pwire.DecodeB64Any(key.KeyData)
	if err != nil || !pstate.ValidNativeAppStateKeyData(keyData) {
		return nil, nil, AppStatePatchError("WA app-state key data is invalid")
	}
	return keyID, keyData, nil
}

func NormalizedNativeAppStateCollection(state *pstate.NativeState, name string) pstate.NativeAppStateCollection {
	if state == nil {
		return pstate.NativeAppStateCollection{}
	}
	collection := state.AppState.Collections[name]
	if collection.IndexValueMACs == nil {
		collection.IndexValueMACs = map[string]string{}
	}
	if hash, err := pwire.DecodeB64Any(collection.Hash); err != nil || len(hash) != WaAppStateHashBytes {
		collection.Hash = pwire.B64u(make([]byte, WaAppStateHashBytes))
	}
	for indexMAC, valueMAC := range collection.IndexValueMACs {
		indexRaw, indexErr := pwire.DecodeB64Any(indexMAC)
		valueRaw, valueErr := pwire.DecodeB64Any(valueMAC)
		if indexErr != nil || valueErr != nil || len(indexRaw) != WaAppStateMACBytes || len(valueRaw) != WaAppStateMACBytes {
			delete(collection.IndexValueMACs, indexMAC)
		}
	}
	return collection
}

func NextNativeAppStateCollection(collection pstate.NativeAppStateCollection, indexMAC []byte, valueMAC []byte) (pstate.NativeAppStateCollection, []byte, error) {
	hash, err := pwire.DecodeB64Any(collection.Hash)
	if err != nil || len(hash) != WaAppStateHashBytes {
		hash = make([]byte, WaAppStateHashBytes)
	}
	var subtract [][]byte
	key := pwire.B64u(indexMAC)
	if previous := collection.IndexValueMACs[key]; previous != "" {
		previousMAC, err := pwire.DecodeB64Any(previous)
		if err == nil && len(previousMAC) == WaAppStateMACBytes {
			subtract = append(subtract, previousMAC)
		}
	}
	nextHash, err := NativeAppStateLTHash(hash, subtract, [][]byte{valueMAC})
	if err != nil {
		return pstate.NativeAppStateCollection{}, nil, err
	}
	next := pstate.NativeAppStateCollection{
		Version:        collection.Version + 1,
		Hash:           pwire.B64u(nextHash),
		IndexValueMACs: map[string]string{},
	}
	for currentIndex, currentValue := range collection.IndexValueMACs {
		next.IndexValueMACs[currentIndex] = currentValue
	}
	next.IndexValueMACs[key] = pwire.B64u(valueMAC)
	return next, nextHash, nil
}

func EncodeNativePushNameSyncAction(index []byte, displayName string, timestampMS uint64) []byte {
	pushName := ProtoBytes(1, []byte(displayName))
	value := ProtoVarint(1, timestampMS)
	value = ProtoBytesInto(value, 7, pushName)
	out := []byte{}
	out = ProtoBytesInto(out, 1, index)
	out = ProtoBytesInto(out, 2, value)
	out = ProtoBytesInto(out, 3, nil)
	return ProtoVarintInto(out, 4, WaAppStatePushNameAPIVersion)
}

type NativeSyncdPatch struct {
	KeyID       []byte
	IndexMAC    []byte
	Value       []byte
	SnapshotMAC []byte
	PatchMAC    []byte
}

func EncodeNativeSyncdPatch(patch NativeSyncdPatch) []byte {
	keyID := ProtoBytes(1, patch.KeyID)
	index := ProtoBytes(1, patch.IndexMAC)
	value := ProtoBytes(1, patch.Value)
	record := []byte{}
	record = ProtoBytesInto(record, 1, index)
	record = ProtoBytesInto(record, 2, value)
	record = ProtoBytesInto(record, 3, keyID)
	mutation := ProtoVarint(1, 0)
	mutation = ProtoBytesInto(mutation, 2, record)
	out := ProtoBytes(2, mutation)
	out = ProtoBytesInto(out, 4, patch.SnapshotMAC)
	out = ProtoBytesInto(out, 5, patch.PatchMAC)
	return ProtoBytesInto(out, 6, keyID)
}

func ProtoBytes(number protowire.Number, value []byte) []byte {
	return ProtoBytesInto(nil, number, value)
}

func ProtoBytesInto(out []byte, number protowire.Number, value []byte) []byte {
	out = protowire.AppendTag(out, number, protowire.BytesType)
	return protowire.AppendBytes(out, value)
}

func ProtoVarint(number protowire.Number, value uint64) []byte {
	return ProtoVarintInto(nil, number, value)
}

func ProtoVarintInto(out []byte, number protowire.Number, value uint64) []byte {
	out = protowire.AppendTag(out, number, protowire.VarintType)
	return protowire.AppendVarint(out, value)
}

func EncryptNativeAppStateValue(plaintext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	iv := pwire.RandomBytes(aes.BlockSize)
	padded := pwire.Pkcs7Pad(plaintext, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	return append(append([]byte{}, iv...), ciphertext...), nil
}

func NativeAppStateMutationMAC(key []byte, operation uint64, data []byte, keyID []byte) ([]byte, bool) {
	opByte, ok := NativeAppStateOperationByte(operation)
	if !ok || len(keyID) == 0 {
		return nil, false
	}
	return NativeAppStateMAC(key, append([]byte{opByte}, keyID...), data), true
}
