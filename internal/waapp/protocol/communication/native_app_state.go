package communication

import (
	"bytes"

	"google.golang.org/protobuf/encoding/protowire"

	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/waproto"
)

const (
	WaAppStateProtoMaxDepth       = 6
	WaAppStateProtoMaxFields      = 256
	WaAppStateCollectionNameLimit = 96
)

const WaAppStateMutationKeyInfo = "WhatsApp Mutation Keys"

func ApplyNativeAppStateKeys(state *pstate.NativeState, raw []byte) bool {
	if state == nil || len(raw) == 0 {
		return false
	}
	keys := NativeAppStateKeys(raw)
	if len(keys) == 0 {
		return false
	}
	state.EnsureMaps()
	changed := false
	for _, key := range keys {
		key = key.Normalized()
		if !key.Valid() {
			continue
		}
		current, exists := state.AppState.Keys[key.KeyID]
		if exists && current.KeyData == key.KeyData && current.Fingerprint == key.Fingerprint && current.Timestamp == key.Timestamp {
			continue
		}
		state.AppState.Keys[key.KeyID] = key
		changed = true
	}
	return changed
}

func NativeAppStateKeys(raw []byte) []pstate.NativeAppStateKey {
	keys := []pstate.NativeAppStateKey{}
	CollectNativeAppStateKeys(raw, nil, 0, &keys)
	return DedupeNativeAppStateKeys(keys)
}

func CollectNativeAppStateKeys(raw []byte, path []protowire.Number, depth int, keys *[]pstate.NativeAppStateKey) {
	if depth > WaAppStateProtoMaxDepth || len(raw) == 0 {
		return
	}
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, WaAppStateProtoMaxFields)
	if !ok {
		return
	}
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		fieldPath := waproto.AppendWAPath(path, field.Number)
		normalized := waproto.NormalizeWAMessagePath(fieldPath)
		if waproto.SameWAPath(normalized, 12, 7) {
			*keys = append(*keys, ParseNativeAppStateKeyShare(field.Value)...)
			continue
		}
		if MaybeNativeAppStateKeyShare(field.Value) {
			*keys = append(*keys, ParseNativeAppStateKeyShare(field.Value)...)
		}
		CollectNativeAppStateKeys(field.Value, fieldPath, depth+1, keys)
	}
}

func MaybeNativeAppStateKeyShare(raw []byte) bool {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 8)
	if !ok {
		return false
	}
	keyRecords := 0
	for _, field := range fields {
		if field.Kind == protowire.BytesType && field.Number == 1 && ParseNativeAppStateKeyRecord(field.Value).Valid() {
			keyRecords++
		}
	}
	return keyRecords > 0
}

func ParseNativeAppStateKeyShare(raw []byte) []pstate.NativeAppStateKey {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, WaAppStateProtoMaxFields)
	if !ok {
		return nil
	}
	keys := []pstate.NativeAppStateKey{}
	for _, field := range fields {
		if field.Kind != protowire.BytesType || field.Number != 1 {
			continue
		}
		if key := ParseNativeAppStateKeyRecord(field.Value); key.Valid() {
			keys = append(keys, key)
		}
	}
	return DedupeNativeAppStateKeys(keys)
}

func ParseNativeAppStateKeyRecord(raw []byte) pstate.NativeAppStateKey {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 8)
	if !ok {
		return pstate.NativeAppStateKey{}
	}
	var keyID []byte
	var keyData NativeAppStateKeyData
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		switch field.Number {
		case 1:
			keyID = ParseNativeAppStateKeyID(field.Value)
		case 2:
			keyData = ParseNativeAppStateKeyData(field.Value)
		}
	}
	out := pstate.NativeAppStateKey{
		KeyID:       pwire.B64u(keyID),
		KeyData:     pwire.B64u(keyData.KeyData),
		Fingerprint: pwire.B64u(keyData.Fingerprint),
		Timestamp:   keyData.Timestamp,
	}
	return out.Normalized()
}

func ParseNativeAppStateKeyID(raw []byte) []byte {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 4)
	if !ok {
		return nil
	}
	for _, field := range fields {
		if field.Kind == protowire.BytesType && field.Number == 1 && pstate.ValidNativeAppStateKeyID(field.Value) {
			return append([]byte{}, field.Value...)
		}
	}
	return nil
}

type NativeAppStateKeyData struct {
	KeyData     []byte
	Fingerprint []byte
	Timestamp   int64
}

func ParseNativeAppStateKeyData(raw []byte) NativeAppStateKeyData {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return NativeAppStateKeyData{}
	}
	var out NativeAppStateKeyData
	for _, field := range fields {
		switch {
		case field.Kind == protowire.BytesType && field.Number == 1:
			out.KeyData = append([]byte{}, field.Value...)
		case field.Kind == protowire.BytesType && field.Number == 2:
			out.Fingerprint = append([]byte{}, field.Value...)
		case field.Kind == protowire.VarintType && field.Number == 3:
			out.Timestamp = int64(field.Varint)
		}
	}
	if !pstate.ValidNativeAppStateKeyData(out.KeyData) {
		out.KeyData = nil
	}
	return out
}

func DedupeNativeAppStateKeys(keys []pstate.NativeAppStateKey) []pstate.NativeAppStateKey {
	if len(keys) == 0 {
		return nil
	}
	merged := map[string]pstate.NativeAppStateKey{}
	order := []string{}
	for _, key := range keys {
		key = key.Normalized()
		if !key.Valid() {
			continue
		}
		current, exists := merged[key.KeyID]
		if !exists {
			order = append(order, key.KeyID)
			merged[key.KeyID] = key
			continue
		}
		if current.Timestamp <= key.Timestamp || !bytes.Equal(MustDecodeNativeAppStateValue(current.KeyData), MustDecodeNativeAppStateValue(key.KeyData)) {
			merged[key.KeyID] = key
		}
	}
	out := make([]pstate.NativeAppStateKey, 0, len(order))
	for _, keyID := range order {
		out = append(out, merged[keyID])
	}
	return out
}

func MustDecodeNativeAppStateValue(value string) []byte {
	out, err := pwire.DecodeB64Any(value)
	if err != nil {
		return nil
	}
	return out
}

func NativeAppStateContactHints(state *pstate.NativeState, raw []byte) []wacore.WAContactHint {
	if len(raw) == 0 {
		return nil
	}
	hints := []wacore.WAContactHint{}
	CollectNativeAppStateContactHints(state, raw, 0, &hints)
	return pwire.DedupeWAContactHints(hints)
}

func CollectNativeAppStateContactHints(state *pstate.NativeState, raw []byte, depth int, hints *[]wacore.WAContactHint) {
	if depth > WaAppStateProtoMaxDepth || len(raw) == 0 {
		return
	}
	*hints = append(*hints, WaAppStateSnapshotContactHints(raw)...)
	*hints = append(*hints, WaAppStatePatchContactHints(state, raw)...)
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, WaAppStateProtoMaxFields)
	if !ok {
		return
	}
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		CollectNativeAppStateContactHints(state, field.Value, depth+1, hints)
	}
}
