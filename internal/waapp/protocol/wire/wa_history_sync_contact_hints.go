package wire

import (
	"strings"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/waproto"
)

func WaHistorySyncContactHints(raw []byte) []wacore.WAContactHint {
	payloads := [][]byte{raw}
	if decoded, ok := WaGunzipPayload(raw); ok {
		payloads = append([][]byte{decoded}, payloads...)
	}
	hints := []wacore.WAContactHint{}
	for _, payload := range payloads {
		fields, ok := waproto.ParseWAProtoFieldsWithLimit(payload, WaContactProtoMaxFields)
		if !ok {
			continue
		}
		for _, field := range fields {
			if field.Kind != protowire.BytesType {
				continue
			}
			switch field.Number {
			case 15:
				if hint := WaStringLIDMappingRecordHint(field.Value); hint.Valid() {
					hints = append(hints, hint)
				}
			case 20:
				if hint := WaInlineContactRecordHint(field.Value); hint.Valid() {
					hints = append(hints, hint)
				}
			}
		}
		if len(hints) > 0 {
			break
		}
	}
	return DedupeWAContactHints(hints)
}

func WaStringLIDMappingRecordHint(raw []byte) wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return wacore.WAContactHint{}
	}
	var hint wacore.WAContactHint
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		switch field.Number {
		case 1:
			hint.PNJID = wacore.NormalizeWAJID(WaProtoPlainString(field.Value))
		case 2:
			hint.LIDJID = wacore.NormalizeWAJID(WaProtoPlainString(field.Value))
		}
	}
	return hint.Normalized()
}

func WaInlineContactRecordHint(raw []byte) wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return wacore.WAContactHint{}
	}
	var hint wacore.WAContactHint
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		switch field.Number {
		case 1:
			hint.PNJID = wacore.NormalizeWAJID(WaProtoPlainString(field.Value))
		case 2:
			hint.LIDJID = wacore.NormalizeWAJID(WaProtoPlainString(field.Value))
		case 3:
			hint.DisplayName = WaContactNameString(field.Value)
		case 4:
			hint.WAName = WaContactNameString(field.Value)
		case 5:
			hint.Username = WaContactNameString(field.Value)
		}
	}
	return hint.Normalized()
}

func WaHistorySyncConversationRecordHint(raw []byte) wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 96)
	if !ok {
		return wacore.WAContactHint{}
	}
	var hint wacore.WAContactHint
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		switch field.Number {
		case 1:
			ApplyContactRecordJID(&hint, WaProtoPlainString(field.Value))
		case 13:
			hint.WAName = WaContactNameString(field.Value)
		case 38:
			hint.DisplayName = WaContactNameString(field.Value)
		case 39:
			hint.PNJID = wacore.NormalizeWAJID(WaProtoPlainString(field.Value))
		case 42:
			hint.LIDJID = wacore.NormalizeWAJID(WaProtoPlainString(field.Value))
		case 43:
			hint.Username = WaContactNameString(field.Value)
		}
	}
	return hint.Normalized()
}

func WaHistorySyncMessageRecordHint(raw []byte) wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 128)
	if !ok {
		return wacore.WAContactHint{}
	}
	var hint wacore.WAContactHint
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		switch field.Number {
		case 1:
			for _, jid := range WaMessageKeyJIDs(field.Value) {
				ApplyContactRecordJID(&hint, jid)
			}
		case 5:
			ApplyContactRecordJID(&hint, WaProtoPlainString(field.Value))
		case 19:
			hint.DisplayName = WaContactNameString(field.Value)
		case 37:
			hint.WAName = WaContactNameString(field.Value)
		}
	}
	return hint.Normalized()
}

func WaContactMetadataRecordHint(raw []byte) wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return wacore.WAContactHint{}
	}
	var firstName string
	var lastName string
	var hasContactMetadataMarker bool
	var hint wacore.WAContactHint
	for _, field := range fields {
		switch {
		case field.Kind == protowire.BytesType:
			switch field.Number {
			case 1:
				firstName = WaContactNameString(field.Value)
			case 2:
				lastName = WaContactNameString(field.Value)
			case 3:
				hint.DisplayName = WaContactNameString(field.Value)
			case 6:
				hint.Username = WaContactNameString(field.Value)
			case 7:
				hint.PNJID = PhoneNumberWAJID(WaProtoPlainString(field.Value))
			}
		case field.Kind == protowire.VarintType:
			switch field.Number {
			case 4, 9:
				hasContactMetadataMarker = true
			case 8:
				hint.LIDJID = NumericWAJID(field.Varint, "lid")
			}
		}
	}
	if !hasContactMetadataMarker {
		return wacore.WAContactHint{}
	}
	if personName := strings.Join(shared.UniqueNonEmptyStrings(firstName, lastName), " "); personName != "" {
		hint.DisplayName = shared.FirstNonEmpty(hint.DisplayName, personName)
	}
	return hint.Normalized()
}

func WaMessageKeyJIDs(raw []byte) []string {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 8)
	if !ok {
		return nil
	}
	out := []string{}
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		switch field.Number {
		case 1, 4:
			if jid := WaProtoPlainString(field.Value); jid != "" {
				out = append(out, jid)
			}
		}
	}
	return out
}

func ApplyContactRecordJID(hint *wacore.WAContactHint, value string) {
	if hint == nil {
		return
	}
	jid := wacore.NormalizeWAJID(value)
	switch {
	case strings.HasSuffix(jid, "@lid"):
		hint.LIDJID = shared.FirstNonEmpty(hint.LIDJID, jid)
	case strings.HasSuffix(jid, "@s.whatsapp.net"):
		hint.PNJID = shared.FirstNonEmpty(hint.PNJID, jid)
	}
}

func DedupeWAContactHints(hints []wacore.WAContactHint) []wacore.WAContactHint {
	if len(hints) == 0 {
		return nil
	}
	merged := map[string]wacore.WAContactHint{}
	order := []string{}
	for _, hint := range hints {
		hint = hint.Normalized()
		if !hint.Valid() {
			continue
		}
		key := hint.LIDJID + "\x00" + hint.PNJID
		current, exists := merged[key]
		if !exists {
			order = append(order, key)
		}
		current.LIDJID = hint.LIDJID
		current.PNJID = hint.PNJID
		current.DisplayName = shared.FirstNonEmpty(current.DisplayName, hint.DisplayName)
		current.WAName = shared.FirstNonEmpty(current.WAName, hint.WAName)
		current.Username = shared.FirstNonEmpty(current.Username, hint.Username)
		current.VerifiedName = shared.FirstNonEmpty(current.VerifiedName, hint.VerifiedName)
		merged[key] = current
	}
	out := make([]wacore.WAContactHint, 0, len(order))
	for _, key := range order {
		out = append(out, merged[key])
	}
	return out
}
