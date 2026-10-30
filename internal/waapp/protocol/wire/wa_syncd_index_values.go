package wire

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/waproto"
)

func WaSyncdIndexValues(raw []byte) []string {
	if len(raw) == 0 || len(raw) > 16<<10 || !utf8.Valid(raw) {
		return nil
	}
	text := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(text, "[") || !strings.HasSuffix(text, "]") {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(text), &values); err != nil {
		return nil
	}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func WaAppStateActionHintsForIndex(index []string, raw []byte) []wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 256)
	if !ok {
		return nil
	}
	indexLID := WaSyncdIndexJID(index, "@lid")
	indexPN := WaSyncdIndexJID(index, "@s.whatsapp.net")
	hints := []wacore.WAContactHint{}
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		switch field.Number {
		case 3:
			if hint := WaIndexedAppStateContactActionHint(field.Value, indexLID, indexPN); hint.Valid() {
				hints = append(hints, hint)
			}
		case 37:
			if hint := WaIndexedPNForLIDChatActionHint(field.Value, indexLID); hint.Valid() {
				hints = append(hints, hint)
			}
		case 61:
			if hint := WaIndexedLIDContactActionHint(field.Value, indexLID, indexPN); hint.Valid() {
				hints = append(hints, hint)
			}
		case 79:
			if hint := WaIndexedOutContactActionHint(field.Value, indexLID, indexPN); hint.Valid() {
				hints = append(hints, hint)
			}
		}
	}
	return DedupeWAContactHints(hints)
}

func WaSyncdIndexJID(index []string, suffix string) string {
	for i := 1; i < len(index); i++ {
		jid := wacore.NormalizeWAJID(index[i])
		if strings.HasSuffix(jid, suffix) {
			return jid
		}
	}
	return ""
}

func WaIndexedAppStateContactActionHint(raw []byte, indexLID string, indexPN string) wacore.WAContactHint {
	hint := WaAppStateContactActionHint(raw)
	hint.LIDJID = shared.FirstNonEmpty(hint.LIDJID, indexLID)
	hint.PNJID = shared.FirstNonEmpty(hint.PNJID, indexPN)
	return hint.Normalized()
}

func WaIndexedPNForLIDChatActionHint(raw []byte, indexLID string) wacore.WAContactHint {
	hint := WaPNForLIDChatActionHint(raw)
	hint.LIDJID = shared.FirstNonEmpty(hint.LIDJID, indexLID)
	return hint.Normalized()
}

func WaIndexedLIDContactActionHint(raw []byte, indexLID string, indexPN string) wacore.WAContactHint {
	hint := WaLIDContactActionHint(raw)
	hint.LIDJID = shared.FirstNonEmpty(hint.LIDJID, indexLID)
	hint.PNJID = shared.FirstNonEmpty(hint.PNJID, indexPN)
	return hint.Normalized()
}

func WaIndexedOutContactActionHint(raw []byte, indexLID string, indexPN string) wacore.WAContactHint {
	hint := WaOutContactActionHint(raw)
	hint.LIDJID = shared.FirstNonEmpty(hint.LIDJID, indexLID)
	hint.PNJID = shared.FirstNonEmpty(hint.PNJID, indexPN)
	return hint.Normalized()
}

func WaAppStateContactActionHint(raw []byte) wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 32)
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
			hint.DisplayName = WaContactNameString(field.Value)
		case 2:
			hint.WAName = WaContactNameString(field.Value)
		case 3:
			hint.LIDJID = wacore.NormalizeWAJID(WaProtoPlainString(field.Value))
		case 5:
			hint.PNJID = wacore.NormalizeWAJID(WaProtoPlainString(field.Value))
		case 6:
			hint.Username = WaContactNameString(field.Value)
		}
	}
	return hint.Normalized()
}

func WaLIDContactActionHint(raw []byte) wacore.WAContactHint {
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
			hint.DisplayName = WaContactNameString(field.Value)
		case 2:
			hint.WAName = WaContactNameString(field.Value)
		case 3:
			hint.Username = WaContactNameString(field.Value)
		}
	}
	return hint.Normalized()
}

func WaOutContactActionHint(raw []byte) wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 8)
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
			hint.DisplayName = WaContactNameString(field.Value)
		case 2:
			hint.WAName = WaContactNameString(field.Value)
		}
	}
	return hint.Normalized()
}

func WaPNForLIDChatActionHint(raw []byte) wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 8)
	if !ok {
		return wacore.WAContactHint{}
	}
	var hint wacore.WAContactHint
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		if field.Number == 1 {
			hint.PNJID = wacore.NormalizeWAJID(WaProtoPlainString(field.Value))
		}
	}
	return hint.Normalized()
}

func WaLIDMigrationMappingPayloadHints(raw []byte) []wacore.WAContactHint {
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
			if field.Kind != protowire.BytesType || field.Number != 1 {
				continue
			}
			hints = append(hints, WaNumericLIDMappingRecordHints(field.Value)...)
		}
		if len(hints) > 0 {
			break
		}
	}
	return DedupeWAContactHints(hints)
}

func WaNumericLIDMappingRecordHints(raw []byte) []wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return nil
	}
	var pn uint64
	lids := []uint64{}
	for _, field := range fields {
		if field.Kind != protowire.VarintType {
			continue
		}
		switch field.Number {
		case 1:
			pn = field.Varint
		case 2, 3:
			if field.Varint > 0 {
				lids = append(lids, field.Varint)
			}
		}
	}
	if pn == 0 || len(lids) == 0 {
		return nil
	}
	pnJID := NumericWAJID(pn, "s.whatsapp.net")
	if pnJID == "" {
		return nil
	}
	hints := []wacore.WAContactHint{}
	for _, lid := range lids {
		if lidJID := NumericWAJID(lid, "lid"); lidJID != "" {
			hints = append(hints, wacore.WAContactHint{LIDJID: lidJID, PNJID: pnJID})
		}
	}
	return DedupeWAContactHints(hints)
}
