package communication

import (
	"google.golang.org/protobuf/encoding/protowire"

	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/waproto"
)

func WaAppStateSnapshotContactHints(raw []byte) []wacore.WAContactHint {
	payloads := [][]byte{raw}
	if snapshot, ok := WaAppStateSnapshotEnvelopePayload(raw); ok {
		payloads = append([][]byte{snapshot}, payloads...)
	}
	hints := []wacore.WAContactHint{}
	for _, payload := range payloads {
		fields, ok := waproto.ParseWAProtoFieldsWithLimit(payload, WaAppStateProtoMaxFields)
		if !ok {
			continue
		}
		collectionName := ""
		records := [][]byte{}
		for _, field := range fields {
			switch {
			case field.Kind == protowire.BytesType && field.Number == 2:
				collectionName = WaAppStateCollectionName(field.Value)
			case field.Kind == protowire.BytesType && field.Number == 3:
				records = append(records, field.Value)
			}
		}
		if collectionName == "" || len(records) == 0 {
			continue
		}
		for _, record := range records {
			hints = append(hints, WaAppStateSnapshotRecordContactHints(record)...)
		}
		if len(hints) > 0 {
			break
		}
	}
	return pwire.DedupeWAContactHints(hints)
}

func WaAppStateSnapshotEnvelopePayload(raw []byte) ([]byte, bool) {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 8)
	if !ok {
		return nil, false
	}
	var payload []byte
	compressed := false
	for _, field := range fields {
		switch {
		case field.Kind == protowire.BytesType && field.Number == 1:
			payload = field.Value
		case field.Kind == protowire.VarintType && field.Number == 2:
			compressed = field.Varint != 0
		}
	}
	if len(payload) == 0 {
		return nil, false
	}
	if compressed {
		if inflated, ok := WaAppStateInflatePayload(payload); ok {
			payload = inflated
		}
	}
	return payload, true
}

func WaAppStateSnapshotRecordContactHints(raw []byte) []wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return nil
	}
	for _, field := range fields {
		if field.Kind == protowire.BytesType && field.Number == 1 {
			return pwire.WaSyncdIndexedContactHints(field.Value)
		}
	}
	return nil
}

func WaAppStatePatchContactHints(state *pstate.NativeState, raw []byte) []wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, WaAppStateProtoMaxFields)
	if !ok {
		return nil
	}
	patch := NativeAppStatePatch{CollectionName: "", KeyID: nil, Mutations: []NativeAppStateMutation{}}
	fieldOneMutations := []NativeAppStateMutation{}
	for _, field := range fields {
		switch {
		case field.Kind == protowire.BytesType && field.Number == 2:
			if mutation, ok := ParseNativeAppStateMutation(field.Value); ok {
				patch.Mutations = append(patch.Mutations, mutation)
			}
		case field.Kind == protowire.BytesType && field.Number == 1:
			if mutation, ok := ParseNativeAppStateMutation(field.Value); ok {
				fieldOneMutations = append(fieldOneMutations, mutation)
			}
		case field.Kind == protowire.BytesType && field.Number == 6:
			patch.KeyID = ParseNativeAppStateRawKeyID(field.Value)
		case field.Kind == protowire.BytesType && field.Number == 9:
			patch.CollectionName = shared.FirstNonEmpty(patch.CollectionName, WaAppStatePatchDebugCollectionName(field.Value))
		}
	}
	if len(patch.Mutations) == 0 {
		patch.Mutations = fieldOneMutations
	}
	if len(patch.Mutations) == 0 {
		return nil
	}
	hints := []wacore.WAContactHint{}
	for _, mutation := range patch.Mutations {
		if len(mutation.KeyID) == 0 {
			mutation.KeyID = patch.KeyID
		}
		hints = append(hints, NativeAppStateMutationContactHints(state, patch.CollectionName, mutation)...)
	}
	return pwire.DedupeWAContactHints(hints)
}

type NativeAppStatePatch struct {
	CollectionName string
	KeyID          []byte
	Mutations      []NativeAppStateMutation
}

type NativeAppStateMutation struct {
	Operation uint64
	Record    NativeAppStateMutationRecord
	KeyID     []byte
}

type NativeAppStateMutationRecord struct {
	Index []byte
	Value []byte
	KeyID []byte
}

func ParseNativeAppStateMutation(raw []byte) (NativeAppStateMutation, bool) {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return NativeAppStateMutation{}, false
	}
	mutation := NativeAppStateMutation{}
	hasOperation := false
	hasRecord := false
	for _, field := range fields {
		switch {
		case field.Kind == protowire.VarintType && field.Number == 1:
			if field.Varint > 1 {
				return NativeAppStateMutation{}, false
			}
			mutation.Operation = field.Varint
			hasOperation = true
		case field.Kind == protowire.BytesType && field.Number == 2:
			if record, ok := ParseNativeAppStateMutationRecord(field.Value); ok {
				mutation.Record = record
				mutation.KeyID = record.KeyID
				hasRecord = true
			}
		}
	}
	if !hasOperation || !hasRecord {
		return NativeAppStateMutation{}, false
	}
	return mutation, true
}

func ParseNativeAppStateMutationRecord(raw []byte) (NativeAppStateMutationRecord, bool) {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return NativeAppStateMutationRecord{}, false
	}
	record := NativeAppStateMutationRecord{}
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		switch field.Number {
		case 1:
			record.Index = ParseNativeAppStateBlob(field.Value)
		case 2:
			record.Value = ParseNativeAppStateBlob(field.Value)
		case 3:
			record.KeyID = ParseNativeAppStateRawKeyID(field.Value)
		}
	}
	if len(record.Value) == 0 || len(record.Index) == 0 {
		return NativeAppStateMutationRecord{}, false
	}
	return record, true
}

func ParseNativeAppStateBlob(raw []byte) []byte {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 4)
	if !ok {
		return nil
	}
	for _, field := range fields {
		if field.Kind == protowire.BytesType && field.Number == 1 {
			return append([]byte{}, field.Value...)
		}
	}
	return nil
}

func ParseNativeAppStateRawKeyID(raw []byte) []byte {
	if pstate.ValidNativeAppStateKeyID(raw) {
		return append([]byte{}, raw...)
	}
	return ParseNativeAppStateKeyID(raw)
}

func NativeAppStateMutationContactHints(state *pstate.NativeState, collectionName string, mutation NativeAppStateMutation) []wacore.WAContactHint {
	if hints := pwire.WaSyncdIndexedContactHints(mutation.Record.Value); len(hints) > 0 {
		return hints
	}
	if state == nil || collectionName == "" || len(mutation.KeyID) == 0 {
		return nil
	}
	decrypted, ok := DecryptNativeAppStateMutation(state, collectionName, mutation)
	if !ok {
		return nil
	}
	return pwire.WaSyncdIndexedContactHints(decrypted)
}
