package wire

import (
	"strings"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/waproto"
)

const (
	WaContactProtoMaxDepth       = 8
	WaContactProtoMaxFields      = 8192
	WaContactDecodedPayloadLimit = 4 << 20
)

func NativeContactHints(raw []byte) []wacore.WAContactHint {
	if len(raw) == 0 {
		return nil
	}
	hints := WaKnownContactRecordHints(raw)
	CollectWAContactHints(raw, nil, 0, &hints)
	return DedupeWAContactHints(hints)
}

func ContactHintsFromChatdNode(node ChatdNode) []wacore.WAContactHint {
	hints := []wacore.WAContactHint{}
	var walk func(ChatdNode)
	walk = func(current ChatdNode) {
		hints = append(hints, ContactHintsFromChatdAttrs(current.Attrs)...)
		if raw, ok := current.Content.([]byte); ok && ShouldScanChatdBinaryContactPayload(current.Tag) {
			hints = append(hints, NativeContactHints(raw)...)
		}
		for _, child := range ChatdChildren(current) {
			walk(child)
		}
	}
	walk(node)
	return DedupeWAContactHints(hints)
}

func ShouldScanChatdBinaryContactPayload(tag string) bool {
	switch tag {
	case "enc",
		"routing_info",
		"edge_routing",
		"media",
		"picture",
		"preview",
		"thumbnail-image",
		"thumbnail-video",
		"thumbnail-document",
		"thumbnail-link",
		"download",
		"key",
		"identity",
		"device-identity",
		"privacy_token":
		return false
	default:
		return true
	}
}

func ContactHintsFromChatdAttrs(attrs map[string]string) []wacore.WAContactHint {
	if len(attrs) == 0 {
		return nil
	}
	displayName := wacore.WAContactName(FirstChatdAttr(attrs, "notify", "notify_name", "display_name"))
	username := WaContactUsername(FirstChatdAttr(attrs, "participant_username", "peer_recipient_username", "author_username", "username"))
	verifiedName := wacore.WAContactName(FirstChatdAttr(attrs, "verified_name", "business_verified_name", "verified_business_name"))
	peerLIDKeys := []string{"peer_recipient_lid", "recipient_latest_lid", "recipient_lid", "peer_lid"}
	peerPNKeys := []string{"peer_recipient_pn", "peer_recipient_pn_jid", "recipient_pn", "recipient_pn_jid", "peer_pn", "peer_pn_jid"}
	actorLIDKeys := []string{"author", "author_lid", "creator_lid"}
	actorPNKeys := []string{"author_pn", "author_pn_jid", "creator_pn", "creator_pn_jid", "pn_jid"}
	contactLIDKeys := []string{"contact_lid"}
	contactPNKeys := []string{"contact_pn", "contact_pn_jid", "pn_jid", "new_jid", "number", "phone", "phone_number", "business_phone_number", "wa_id"}
	fallbackLIDKeys := []string{
		"sender_lid", "participant_lid", "peer_recipient_lid", "recipient_latest_lid", "recipient_lid", "peer_lid",
		"contact_lid", "author_lid", "creator_lid", "caller_lid", "invitee_lid", "lid", "jid", "participant", "author", "from",
	}
	fallbackPNKeys := []string{
		"sender_pn", "sender_pn_jid", "participant_pn", "participant_pn_jid", "peer_recipient_pn", "peer_recipient_pn_jid",
		"recipient_pn", "recipient_pn_jid", "peer_pn", "peer_pn_jid", "contact_pn", "contact_pn_jid", "author_pn", "author_pn_jid",
		"creator_pn", "creator_pn_jid", "caller_pn", "caller_pn_jid", "invitee_pn", "invitee_pn_jid", "from_pn", "from_pn_jid",
		"pn", "pn_jid", "new_jid", "number", "phone", "phone_number", "business_phone_number", "wa_id", "jid", "participant", "author", "from",
	}
	hints := []wacore.WAContactHint{}
	AppendChatdAttrHints(&hints, attrs, displayName, username, verifiedName, []string{"sender_lid"}, []string{"sender_pn", "sender_pn_jid"})
	AppendChatdAttrHints(&hints, attrs, displayName, username, verifiedName, []string{"participant_lid", "participant", "jid"}, []string{"participant_pn", "participant_pn_jid", "pn_jid", "jid"})
	AppendChatdAttrHints(&hints, attrs, displayName, WaContactUsername(FirstChatdAttr(attrs, "peer_recipient_username", "username")), verifiedName, peerLIDKeys, peerPNKeys)
	AppendChatdAttrHints(&hints, attrs, displayName, WaContactUsername(FirstChatdAttr(attrs, "author_username", "username")), verifiedName, actorLIDKeys, actorPNKeys)
	AppendChatdAttrHints(&hints, attrs, displayName, username, verifiedName, []string{"from"}, []string{"from_pn", "from_pn_jid", "pn_jid", "new_jid"})
	AppendChatdAttrHints(&hints, attrs, wacore.WAContactName(shared.FirstNonEmpty(FirstChatdAttr(attrs, "contact_push_name"), displayName)), WaContactUsername(FirstChatdAttr(attrs, "contact_username", "username")), verifiedName, contactLIDKeys, contactPNKeys)
	AppendChatdAttrHints(&hints, attrs, displayName, username, verifiedName, []string{"caller_lid"}, []string{"caller_pn", "caller_pn_jid"})
	AppendChatdAttrHints(&hints, attrs, displayName, username, verifiedName, []string{"invitee_lid"}, []string{"invitee_pn", "invitee_pn_jid"})
	if len(hints) == 0 {
		lids := ChatdAttrJIDs(attrs, "@lid", fallbackLIDKeys...)
		pns := ChatdAttrJIDs(attrs, "@s.whatsapp.net", fallbackPNKeys...)
		if len(lids) == 1 && len(pns) <= 1 {
			hint := wacore.WAContactHint{LIDJID: lids[0], DisplayName: displayName, Username: username, VerifiedName: verifiedName}
			if len(pns) == 1 {
				hint.PNJID = pns[0]
			}
			if hint.Valid() {
				hints = append(hints, hint)
			}
		}
	}
	return DedupeWAContactHints(hints)
}

func FirstChatdAttr(attrs map[string]string, keys ...string) string {
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, attrs[key])
	}
	return shared.FirstNonEmpty(values...)
}

func AppendChatdAttrHints(hints *[]wacore.WAContactHint, attrs map[string]string, displayName string, username string, verifiedName string, lidKeys []string, pnKeys []string) {
	lids := ChatdAttrJIDs(attrs, "@lid", lidKeys...)
	if len(lids) == 0 {
		return
	}
	pns := ChatdAttrJIDs(attrs, "@s.whatsapp.net", pnKeys...)
	if len(pns) == 0 {
		if len(lids) != 1 {
			return
		}
		hint := wacore.WAContactHint{LIDJID: lids[0], DisplayName: displayName, Username: username, VerifiedName: verifiedName}
		if hint.Valid() {
			*hints = append(*hints, hint)
		}
		return
	}
	for _, lid := range lids {
		for _, pn := range pns {
			hint := wacore.WAContactHint{LIDJID: lid, PNJID: pn, DisplayName: displayName, Username: username, VerifiedName: verifiedName}
			if hint.Valid() {
				*hints = append(*hints, hint)
			}
		}
	}
}

func ChatdAttrJIDs(attrs map[string]string, suffix string, keys ...string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, key := range keys {
		jid := wacore.NormalizeWAJID(attrs[key])
		if !strings.HasSuffix(jid, suffix) {
			continue
		}
		if _, ok := seen[jid]; ok {
			continue
		}
		seen[jid] = struct{}{}
		out = append(out, jid)
	}
	return out
}

func CollectWAContactHints(raw []byte, path []protowire.Number, depth int, hints *[]wacore.WAContactHint) {
	if depth > WaContactProtoMaxDepth || len(raw) == 0 {
		return
	}
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, WaContactProtoMaxFields)
	if !ok {
		return
	}
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		fieldPath := waproto.AppendWAPath(path, field.Number)
		normalized := waproto.NormalizeWAMessagePath(fieldPath)
		switch {
		case waproto.SameWAPath(normalized, 12, 23, 1):
			*hints = append(*hints, WaLIDMigrationMappingPayloadHints(field.Value)...)
		case waproto.SameWAPath(normalized, 12, 6, 11):
			*hints = append(*hints, WaHistorySyncContactHints(field.Value)...)
		}
		*hints = append(*hints, WaKnownContactRecordHints(field.Value)...)
		CollectWAContactHints(field.Value, fieldPath, depth+1, hints)
	}
}

func WaKnownContactRecordHints(raw []byte) []wacore.WAContactHint {
	hints := []wacore.WAContactHint{}
	hints = append(hints, WaSyncdIndexedContactHints(raw)...)
	if hint := WaInlineContactRecordHint(raw); hint.Valid() {
		hints = append(hints, hint)
	}
	if hint := WaHistorySyncConversationRecordHint(raw); hint.Valid() {
		hints = append(hints, hint)
	}
	if hint := WaHistorySyncMessageRecordHint(raw); hint.Valid() {
		hints = append(hints, hint)
	}
	if hint := WaContactMetadataRecordHint(raw); hint.Valid() {
		hints = append(hints, hint)
	}
	if hint := WaAppStateContactActionHint(raw); hint.Valid() {
		hints = append(hints, hint)
	}
	if hint := WaLIDContactActionHint(raw); hint.Valid() {
		hints = append(hints, hint)
	}
	return DedupeWAContactHints(hints)
}

func WaSyncdIndexedContactHints(raw []byte) []wacore.WAContactHint {
	fields, ok := waproto.ParseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return nil
	}
	var indexRaw []byte
	var valueRaw []byte
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		switch field.Number {
		case 1:
			indexRaw = field.Value
		case 2:
			valueRaw = field.Value
		}
	}
	index := WaSyncdIndexValues(indexRaw)
	if len(index) == 0 || len(valueRaw) == 0 {
		return nil
	}
	return WaAppStateActionHintsForIndex(index, valueRaw)
}
