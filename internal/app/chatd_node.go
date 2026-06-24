package app

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

type chatdNode struct {
	Tag     string
	Attrs   map[string]string
	Content any
}

type chatdEncPayload struct {
	StanzaID            string
	StanzaTimestamp     string
	Contact             string
	Sender              string
	ContactPN           string
	SenderPN            string
	NotifyName          string
	ParticipantUsername string
	ContactHints        []waContactHint
	EncType             string
	Path                string
	Payload             []byte
}

type tokenDictionary struct {
	primary   []string
	secondary [][]string
	reverse   map[string]tokenRef
}

type tokenRef struct {
	prefix int
	index  int
}

func fallbackTokenDictionary() *tokenDictionary {
	values := make([]string, 236)
	known := map[int]string{
		1:   "xmlstreamstart",
		2:   "xmlstreamend",
		3:   "s.whatsapp.net",
		4:   "type",
		5:   "participant",
		6:   "from",
		7:   "receipt",
		8:   "id",
		9:   "notification",
		10:  "disappearing_mode",
		11:  "status",
		12:  "jid",
		13:  "broadcast",
		14:  "user",
		15:  "devices",
		16:  "device_hash",
		17:  "to",
		18:  "offline",
		19:  "message",
		20:  "result",
		21:  "class",
		22:  "xmlns",
		23:  "duration",
		24:  "notify",
		25:  "iq",
		26:  "t",
		27:  "ack",
		28:  "g.us",
		29:  "enc",
		30:  "urn:xmpp:whatsapp:push",
		31:  "presence",
		32:  "config_value",
		33:  "picture",
		34:  "verified_name",
		35:  "config_code",
		36:  "key-index-list",
		37:  "contact",
		38:  "mediatype",
		39:  "routing_info",
		40:  "edge_routing",
		41:  "get",
		42:  "read",
		43:  "urn:xmpp:ping",
		44:  "fallback_hostname",
		45:  "0",
		46:  "chatstate",
		47:  "business_hours_config",
		48:  "unavailable",
		49:  "download_buckets",
		50:  "skmsg",
		51:  "verified_level",
		52:  "composing",
		53:  "handshake",
		54:  "device-list",
		55:  "media",
		56:  "text",
		57:  "fallback_ip4",
		58:  "media_conn",
		59:  "device",
		60:  "creation",
		61:  "location",
		62:  "config",
		63:  "item",
		64:  "fallback_ip6",
		65:  "count",
		66:  "w:profile:picture",
		67:  "image",
		68:  "business",
		69:  "2",
		70:  "hostname",
		71:  "call-creator",
		72:  "display_name",
		73:  "relaylatency",
		74:  "platform",
		75:  "abprops",
		76:  "success",
		77:  "msg",
		78:  "offline_preview",
		79:  "prop",
		80:  "key-index",
		81:  "v",
		82:  "day_of_week",
		83:  "pkmsg",
		84:  "version",
		85:  "1",
		86:  "ping",
		87:  "w:p",
		88:  "download",
		89:  "video",
		90:  "set",
		91:  "specific_hours",
		92:  "props",
		93:  "primary",
		94:  "unknown",
		95:  "hash",
		96:  "commerce_experience",
		97:  "last",
		98:  "subscribe",
		99:  "max_buckets",
		100: "call",
		101: "profile",
		102: "member_since_text",
		103: "close_time",
		104: "call-id",
		105: "sticker",
		106: "mode",
		107: "participants",
		108: "value",
		109: "query",
		110: "profile_options",
		111: "open_time",
		112: "code",
		113: "list",
		114: "host",
		115: "ts",
		116: "contacts",
		117: "upload",
		118: "lid",
		119: "preview",
		120: "update",
		121: "usync",
		122: "w:stats",
		123: "delivery",
		124: "auth_ttl",
		125: "context",
		126: "fail",
		127: "cart_enabled",
		128: "appdata",
		129: "category",
		130: "atn",
		131: "direct_connection",
		132: "decrypt-fail",
		133: "relay_id",
		134: "mmg-fallback.whatsapp.net",
		135: "target",
		136: "available",
		137: "name",
		138: "last_id",
		139: "mmg.whatsapp.net",
		140: "categories",
		141: "401",
		142: "is_new",
		143: "index",
		144: "tctoken",
		145: "ip4",
		146: "token_id",
		147: "latency",
		148: "recipient",
		149: "edit",
		150: "ip6",
		151: "add",
		152: "thumbnail-document",
		153: "26",
		154: "paused",
		155: "true",
		156: "identity",
		157: "stream:error",
		158: "key",
		159: "sidelist",
		160: "background",
		161: "audio",
		162: "3",
		163: "thumbnail-image",
		164: "biz-cover-photo",
		165: "cat",
		166: "gcm",
		167: "thumbnail-video",
		168: "error",
		169: "auth",
		170: "deny",
		171: "serial",
		172: "in",
		173: "registration",
		174: "thumbnail-link",
		175: "remove",
		176: "00",
		177: "gif",
		178: "thumbnail-gif",
		179: "tag",
		180: "capability",
		181: "multicast",
		182: "item-not-found",
		183: "description",
		184: "business_hours",
		185: "config_expo_key",
		186: "md-app-state",
		187: "expiration",
		188: "fallback",
		189: "ttl",
		190: "300",
		191: "md-msg-hist",
		192: "device_orientation",
		193: "out",
		194: "w:m",
		195: "open_24h",
		196: "side_list",
		197: "token",
		198: "inactive",
		199: "01",
		200: "document",
		201: "te2",
		202: "played",
		203: "encrypt",
		204: "msgr",
		205: "hide",
		206: "direct_path",
		207: "12",
		208: "state",
		209: "not-authorized",
		210: "url",
		211: "terminate",
		212: "signature",
		213: "status-revoke-delay",
		214: "02",
		215: "te",
		216: "linked_accounts",
		217: "trusted_contact",
		218: "timezone",
		219: "ptt",
		220: "kyc-id",
		221: "privacy_token",
		222: "readreceipts",
		223: "appointment_only",
		224: "address",
		225: "expected_ts",
		226: "privacy",
		227: "7",
		228: "android",
		229: "interactive",
		230: "device-identity",
		231: "enabled",
		232: "attribute_padding",
		233: "1080",
		234: "03",
		235: "screen_height",
	}
	reverse := map[string]tokenRef{}
	for idx, value := range known {
		values[idx] = value
		reverse[value] = tokenRef{prefix: -1, index: idx}
	}
	secondary := [][]string{
		tokenDictionaryBucket(map[int]string{
			14:  "conflict",
			50:  "refresh",
			81:  "pn",
			82:  "delete",
			88:  "delta",
			91:  "collection",
			99:  "critical_unblock_low",
			109: "privacy_mode_ts",
			118: "actual_actors",
			123: "notice",
			125: "host_storage",
			156: "w:sync:app:state",
			184: "urn:xmpp:whatsapp:dirty",
			227: "sync",
			237: "regular_high",
			246: "patch",
		}),
		tokenDictionaryBucket(map[int]string{
			1:   "dirty",
			10:  "full",
			30:  "return_snapshot",
			40:  "regular_low",
			99:  "hist_sync",
			211: "action",
			191: "val",
			248: "clean",
		}),
		tokenDictionaryBucket(map[int]string{
			60:  "failure",
			74:  "patches",
			123: "invalid",
			161: "critical_block",
			248: "regular",
		}),
		tokenDictionaryBucket(map[int]string{30: "keys", 184: "533"}),
	}
	for bucket, values := range secondary {
		for idx, value := range values {
			if value != "" {
				reverse[value] = tokenRef{prefix: 236 + bucket, index: idx}
			}
		}
	}
	return &tokenDictionary{primary: values, secondary: secondary, reverse: reverse}
}

func tokenDictionaryBucket(values map[int]string) []string {
	maxIndex := 0
	for idx := range values {
		if idx > maxIndex {
			maxIndex = idx
		}
	}
	out := make([]string, maxIndex+1)
	for idx, value := range values {
		out[idx] = value
	}
	return out
}

func (d *tokenDictionary) get(token int, r *bytes.Reader) (string, error) {
	if token > 0 && token < 236 {
		if token < len(d.primary) && d.primary[token] != "" {
			return d.primary[token], nil
		}
		return fmt.Sprintf("<tok:%d>", token), nil
	}
	if token >= 236 && token <= 239 && r != nil {
		idx, err := r.ReadByte()
		if err != nil {
			return "", newChatdError("truncated secondary token")
		}
		bucket := int(token - 236)
		if bucket < len(d.secondary) && int(idx) < len(d.secondary[bucket]) && d.secondary[bucket][idx] != "" {
			return d.secondary[bucket][idx], nil
		}
		return fmt.Sprintf("<tok:%d:%d>", token, idx), nil
	}
	return fmt.Sprintf("<tok:%d>", token), nil
}

func (d *tokenDictionary) encodeString(out *bytes.Buffer, value string, allowJID bool) error {
	if ref, ok := d.reverse[value]; ok {
		if ref.prefix >= 0 {
			out.WriteByte(byte(ref.prefix))
		}
		out.WriteByte(byte(ref.index))
		return nil
	}
	if allowJID && strings.Contains(value, "@") {
		parts := strings.SplitN(value, "@", 2)
		out.WriteByte(250)
		if err := d.encodeString(out, parts[0], false); err != nil {
			return err
		}
		return d.encodeString(out, parts[1], false)
	}
	return writeBinaryString(out, []byte(value))
}

type binaryNodeCodec struct {
	dictionary *tokenDictionary
}

func newBinaryNodeCodec() *binaryNodeCodec {
	return &binaryNodeCodec{dictionary: fallbackTokenDictionary()}
}

func (c *binaryNodeCodec) decodeNodePayload(plaintext []byte) (chatdNode, error) {
	body, err := compressMaybeDecodeNodePayload(plaintext)
	if err != nil {
		return chatdNode{}, err
	}
	return c.readNode(bytes.NewReader(body))
}

func (c *binaryNodeCodec) encodeNode(node chatdNode) ([]byte, error) {
	out := bytes.NewBuffer(nil)
	if err := c.writeNode(out, node); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (c *binaryNodeCodec) readByte(r *bytes.Reader) (int, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, newChatdError("unexpected end of binary node")
	}
	return int(b), nil
}

func (c *binaryNodeCodec) readListSize(r *bytes.Reader, token int) (int, error) {
	switch token {
	case 0:
		return 0, nil
	case 248:
		return c.readByte(r)
	case 249:
		raw := make([]byte, 2)
		if _, err := io.ReadFull(r, raw); err != nil {
			return 0, newChatdError("truncated list size")
		}
		return int(raw[0])<<8 | int(raw[1]), nil
	default:
		return 0, newChatdError("invalid list-size token %d", token)
	}
}

func (c *binaryNodeCodec) readString(r *bytes.Reader, token int) (string, bool, error) {
	if token == 0 {
		return "", false, nil
	}
	if token > 0 && token < 236 || token >= 236 && token <= 239 {
		value, err := c.dictionary.get(token, r)
		return value, true, err
	}
	switch token {
	case 247:
		flags, err := c.readByte(r)
		if err != nil {
			return "", false, err
		}
		device, err := c.readByte(r)
		if err != nil {
			return "", false, err
		}
		userToken, err := c.readByte(r)
		if err != nil {
			return "", false, err
		}
		user, _, err := c.readString(r, userToken)
		if err != nil {
			return "", false, err
		}
		hosted := flags&128 != 0
		primary := flags&1 == 0
		server := "s.whatsapp.net"
		if hosted && primary {
			server = "hosted"
		} else if hosted {
			server = "hosted.lid"
		} else if !primary {
			server = "lid"
		}
		suffix := ""
		if device != 0 {
			suffix = ":" + strconv.Itoa(device)
		}
		if user == "" {
			return server, true, nil
		}
		return user + suffix + "@" + server, true, nil
	case 250:
		userToken, err := c.readByte(r)
		if err != nil {
			return "", false, err
		}
		user, _, err := c.readString(r, userToken)
		if err != nil {
			return "", false, err
		}
		serverToken, err := c.readByte(r)
		if err != nil {
			return "", false, err
		}
		server, _, err := c.readString(r, serverToken)
		if err != nil {
			return "", false, err
		}
		if user == "" {
			return server, true, nil
		}
		return user + "@" + server, true, nil
	case 251, 255:
		value, err := c.readPackedString(r, token)
		return value, true, err
	case 252, 253, 254:
		raw, err := readBinaryString(r, token)
		if err != nil {
			return "", false, err
		}
		return string(raw), true, nil
	default:
		return "", false, newChatdError("readString could not match token %d", token)
	}
}

func (c *binaryNodeCodec) readPackedString(r *bytes.Reader, token int) (string, error) {
	first, err := c.readByte(r)
	if err != nil {
		return "", err
	}
	byteLen := first & 0x7f
	odd := first&0x80 != 0
	raw := make([]byte, byteLen)
	if _, err := io.ReadFull(r, raw); err != nil {
		return "", newChatdError("truncated packed string")
	}
	alphabet := "0123456789-."
	if token == 255 {
		alphabet = "0123456789ABCDEF"
	}
	wanted := byteLen * 2
	if odd {
		wanted--
	}
	var b strings.Builder
	for _, value := range raw {
		for _, nibble := range []byte{value >> 4, value & 0x0f} {
			if b.Len() >= wanted {
				break
			}
			if int(nibble) < len(alphabet) {
				b.WriteByte(alphabet[nibble])
			} else {
				b.WriteByte('?')
			}
		}
	}
	return b.String(), nil
}

func (c *binaryNodeCodec) readNode(r *bytes.Reader) (chatdNode, error) {
	listToken, err := c.readByte(r)
	if err != nil {
		return chatdNode{}, err
	}
	listSize, err := c.readListSize(r, listToken)
	if err != nil {
		return chatdNode{}, err
	}
	tagToken, err := c.readByte(r)
	if err != nil {
		return chatdNode{}, err
	}
	tag, ok, err := c.readString(r, tagToken)
	if err != nil {
		return chatdNode{}, err
	}
	if listSize == 0 || !ok {
		return chatdNode{}, newChatdError("invalid binary node: empty list or null tag")
	}
	attrCount := ((listSize - 2) + (listSize % 2)) / 2
	attrs := map[string]string{}
	for i := 0; i < attrCount; i++ {
		keyToken, err := c.readByte(r)
		if err != nil {
			return chatdNode{}, err
		}
		key, keyOK, err := c.readString(r, keyToken)
		if err != nil {
			return chatdNode{}, err
		}
		valueToken, err := c.readByte(r)
		if err != nil {
			return chatdNode{}, err
		}
		value, _, err := c.readString(r, valueToken)
		if err != nil {
			return chatdNode{}, err
		}
		if keyOK {
			attrs[key] = value
		}
	}
	if listSize%2 == 1 {
		return chatdNode{Tag: tag, Attrs: attrs}, nil
	}
	contentToken, err := c.readByte(r)
	if err != nil {
		return chatdNode{}, err
	}
	if contentToken == 0 || contentToken == 248 || contentToken == 249 {
		count, err := c.readListSize(r, contentToken)
		if err != nil {
			return chatdNode{}, err
		}
		children := make([]chatdNode, 0, count)
		for i := 0; i < count; i++ {
			child, err := c.readNode(r)
			if err != nil {
				return chatdNode{}, err
			}
			children = append(children, child)
		}
		return chatdNode{Tag: tag, Attrs: attrs, Content: children}, nil
	}
	if contentToken == 252 || contentToken == 253 || contentToken == 254 {
		raw, err := readBinaryString(r, contentToken)
		if err != nil {
			return chatdNode{}, err
		}
		return chatdNode{Tag: tag, Attrs: attrs, Content: raw}, nil
	}
	if contentToken == 251 || contentToken == 255 {
		value, err := c.readPackedString(r, contentToken)
		if err != nil {
			return chatdNode{}, err
		}
		return chatdNode{Tag: tag, Attrs: attrs, Content: value}, nil
	}
	value, _, err := c.readString(r, contentToken)
	if err != nil {
		return chatdNode{}, err
	}
	return chatdNode{Tag: tag, Attrs: attrs, Content: value}, nil
}

func (c *binaryNodeCodec) writeNode(out *bytes.Buffer, node chatdNode) error {
	hasContent := node.Content != nil
	listSize := 1 + len(node.Attrs)*2
	if hasContent {
		listSize++
	}
	if err := writeListSize(out, listSize); err != nil {
		return err
	}
	if err := c.dictionary.encodeString(out, node.Tag, false); err != nil {
		return err
	}
	for _, key := range orderedNodeAttrKeys(node.Attrs) {
		value := node.Attrs[key]
		if err := c.dictionary.encodeString(out, key, false); err != nil {
			return err
		}
		if err := c.dictionary.encodeString(out, value, true); err != nil {
			return err
		}
	}
	switch value := node.Content.(type) {
	case nil:
		return nil
	case []chatdNode:
		if err := writeListSize(out, len(value)); err != nil {
			return err
		}
		for _, child := range value {
			if err := c.writeNode(out, child); err != nil {
				return err
			}
		}
	case []byte:
		return writeBinaryString(out, value)
	case string:
		return c.dictionary.encodeString(out, value, true)
	default:
		return c.dictionary.encodeString(out, fmt.Sprint(value), true)
	}
	return nil
}

func orderedNodeAttrKeys(attrs map[string]string) []string {
	if len(attrs) == 0 {
		return nil
	}
	remaining := map[string]struct{}{}
	for key := range attrs {
		remaining[key] = struct{}{}
	}
	preferred := []string{"id", "type", "to", "xmlns", "from", "participant", "class"}
	out := make([]string, 0, len(attrs))
	for _, key := range preferred {
		if _, ok := remaining[key]; ok {
			out = append(out, key)
			delete(remaining, key)
		}
	}
	rest := make([]string, 0, len(remaining))
	for key := range remaining {
		rest = append(rest, key)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func writeListSize(out *bytes.Buffer, size int) error {
	if size == 0 {
		out.WriteByte(0)
		return nil
	}
	if size < 256 {
		out.WriteByte(248)
		out.WriteByte(byte(size))
		return nil
	}
	if size < 65536 {
		out.WriteByte(249)
		out.WriteByte(byte(size >> 8))
		out.WriteByte(byte(size))
		return nil
	}
	return newChatdError("list too long: %d", size)
}

func writeBinaryString(out *bytes.Buffer, raw []byte) error {
	if len(raw) < 256 {
		out.WriteByte(252)
		out.WriteByte(byte(len(raw)))
	} else if len(raw) < 1<<20 {
		out.WriteByte(253)
		out.Write([]byte{byte(len(raw) >> 16), byte(len(raw) >> 8), byte(len(raw))})
	} else if len(raw) < 1<<32 {
		out.WriteByte(254)
		out.Write([]byte{byte(len(raw) >> 24), byte(len(raw) >> 16), byte(len(raw) >> 8), byte(len(raw))})
	} else {
		return newChatdError("binary string too long: %d", len(raw))
	}
	out.Write(raw)
	return nil
}

func readBinaryString(r *bytes.Reader, token int) ([]byte, error) {
	var size int
	switch token {
	case 252:
		b, err := r.ReadByte()
		if err != nil {
			return nil, newChatdError("truncated binary string length")
		}
		size = int(b)
	case 253:
		raw := make([]byte, 3)
		if _, err := io.ReadFull(r, raw); err != nil {
			return nil, newChatdError("truncated binary string length")
		}
		size = int(raw[0]&0x0f)<<16 | int(raw[1])<<8 | int(raw[2])
	case 254:
		raw := make([]byte, 4)
		if _, err := io.ReadFull(r, raw); err != nil {
			return nil, newChatdError("truncated binary string length")
		}
		size = int(raw[0])<<24 | int(raw[1])<<16 | int(raw[2])<<8 | int(raw[3])
	default:
		return nil, newChatdError("invalid binary string token %d", token)
	}
	out := make([]byte, size)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, newChatdError("truncated binary string")
	}
	return out, nil
}

func buildPingNode() chatdNode {
	stamp := timeNowMillis()
	return chatdNode{Tag: "iq", Attrs: map[string]string{"id": fmt.Sprintf("go-%d", stamp), "type": "get", "to": "s.whatsapp.net", "xmlns": "urn:xmpp:ping"}, Content: []chatdNode{{Tag: "ping"}}}
}

func buildAckForNode(node chatdNode) (chatdNode, bool) {
	return buildAckForNodeWithAttrs(node, nil)
}

func buildAckForNodeWithAttrs(node chatdNode, extra map[string]string) (chatdNode, bool) {
	nodeID := node.Attrs["id"]
	sender := node.Attrs["from"]
	if nodeID == "" || sender == "" {
		return chatdNode{}, false
	}
	switch node.Tag {
	case "notification":
		attrs := map[string]string{"id": nodeID, "to": sender, "class": "notification"}
		if t := node.Attrs["type"]; t != "" {
			attrs["type"] = t
		}
		addAckExtraAttrs(attrs, extra)
		return chatdNode{Tag: "ack", Attrs: attrs}, true
	case "message":
		attrs := map[string]string{"id": nodeID, "to": sender, "class": "message"}
		if t := node.Attrs["type"]; t != "" {
			attrs["type"] = t
		}
		if p := node.Attrs["participant"]; p != "" {
			attrs["participant"] = p
		}
		addAckExtraAttrs(attrs, extra)
		return chatdNode{Tag: "ack", Attrs: attrs}, true
	default:
		return chatdNode{}, false
	}
}

func addAckExtraAttrs(attrs map[string]string, extra map[string]string) {
	for key, value := range extra {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			attrs[key] = value
		}
	}
}

func iterEncPayloads(node chatdNode) []chatdEncPayload {
	out := []chatdEncPayload{}
	var walk func(chatdNode, []string, chatdMessageRefs)
	walk = func(current chatdNode, path []string, refs chatdMessageRefs) {
		currentPath := append(append([]string{}, path...), current.Tag)
		if id := current.Attrs["id"]; id != "" {
			refs.StanzaID = id
		}
		if stanzaTimestamp := current.Attrs["t"]; stanzaTimestamp != "" {
			refs.StanzaTimestamp = stanzaTimestamp
		}
		if current.Tag == "message" {
			senderLID := firstChatdAttr(current.Attrs, "sender_lid", "participant_lid")
			senderPN := firstChatdAttr(current.Attrs, "sender_pn", "sender_pn_jid", "participant_pn", "participant_pn_jid")
			peerLID := firstChatdAttr(current.Attrs, "peer_recipient_lid", "recipient_latest_lid", "recipient_lid", "peer_lid")
			peerPN := firstChatdAttr(current.Attrs, "peer_recipient_pn", "peer_recipient_pn_jid", "recipient_pn", "recipient_pn_jid", "peer_pn", "peer_pn_jid")
			contactLID := firstChatdAttr(current.Attrs, "contact_lid", "author_lid", "creator_lid", "caller_lid", "invitee_lid")
			contactPN := firstChatdAttr(current.Attrs, "contact_pn", "contact_pn_jid", "author_pn", "author_pn_jid", "creator_pn", "creator_pn_jid", "caller_pn", "caller_pn_jid", "invitee_pn", "invitee_pn_jid")
			refs.Contact = firstNonEmpty(firstChatdAttr(current.Attrs, "from"), peerLID, senderLID, firstChatdAttr(current.Attrs, "participant"), refs.Contact)
			refs.Sender = firstNonEmpty(senderLID, contactLID, firstChatdAttr(current.Attrs, "participant", "from"), refs.Sender, refs.Contact)
			refs.ContactPN = firstNonEmpty(firstChatdAttr(current.Attrs, "from_pn", "from_pn_jid", "pn_jid", "new_jid"), peerPN, contactPN, senderPN, refs.ContactPN)
			refs.SenderPN = firstNonEmpty(firstChatdAttr(current.Attrs, "participant_pn", "participant_pn_jid", "sender_pn", "sender_pn_jid"), contactPN, firstChatdAttr(current.Attrs, "from_pn", "from_pn_jid", "pn_jid"), refs.SenderPN, refs.ContactPN)
			refs.NotifyName = firstNonEmpty(firstChatdAttr(current.Attrs, "notify", "notify_name", "display_name", "contact_push_name"), refs.NotifyName)
			refs.ParticipantUsername = firstNonEmpty(firstChatdAttr(current.Attrs, "participant_username", "peer_recipient_username", "contact_username", "username"), refs.ParticipantUsername)
			refs.ContactHints = dedupeWAContactHints(append(refs.ContactHints, contactHintsFromChatdNode(current)...))
		}
		if current.Tag == "enc" {
			if raw, ok := current.Content.([]byte); ok {
				out = append(out, chatdEncPayload{
					StanzaID:            refs.StanzaID,
					StanzaTimestamp:     refs.StanzaTimestamp,
					Contact:             refs.Contact,
					Sender:              refs.Sender,
					ContactPN:           refs.ContactPN,
					SenderPN:            refs.SenderPN,
					NotifyName:          refs.NotifyName,
					ParticipantUsername: refs.ParticipantUsername,
					ContactHints:        refs.ContactHints,
					EncType:             firstNonEmpty(current.Attrs["type"], current.Attrs["v"], "auto"),
					Path:                strings.Join(currentPath, "/"),
					Payload:             raw,
				})
			}
		}
		if children, ok := current.Content.([]chatdNode); ok {
			for _, child := range children {
				walk(child, currentPath, refs)
			}
		}
	}
	walk(node, nil, chatdMessageRefs{})
	return out
}

type chatdMessageRefs struct {
	StanzaID            string
	StanzaTimestamp     string
	Contact             string
	Sender              string
	ContactPN           string
	SenderPN            string
	NotifyName          string
	ParticipantUsername string
	ContactHints        []waContactHint
}

func routingInfoFromNode(node chatdNode) string {
	var found string
	var walk func(chatdNode)
	walk = func(current chatdNode) {
		if found != "" {
			return
		}
		if current.Tag == "routing_info" {
			found = routingInfoFromContent(current.Content)
			return
		}
		if children, ok := current.Content.([]chatdNode); ok {
			for _, child := range children {
				walk(child)
				if found != "" {
					return
				}
			}
		}
	}
	walk(node)
	return found
}

func routingInfoFromContent(content any) string {
	switch value := content.(type) {
	case []byte:
		if len(value) == 0 || len(value) > 256 {
			return ""
		}
		return b64u(value)
	case string:
		return normalizeChatRoutingInfo(value)
	default:
		return ""
	}
}

func nodePayloadSummary(node chatdNode) string {
	if len(node.Attrs) == 0 {
		return node.Tag
	}
	parts := make([]string, 0, len(node.Attrs)+1)
	parts = append(parts, node.Tag)
	for key, value := range node.Attrs {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, " ")
}

func isChatdTerminalNode(node chatdNode) bool {
	switch node.Tag {
	case "stream:error", "failure", "error":
		return true
	default:
		return false
	}
}

func controlNodeSummary(node chatdNode) string {
	parts := append([]string{node.Tag}, controlNodeAttrSummary(node.Attrs)...)
	if children, ok := node.Content.([]chatdNode); ok && len(children) > 0 {
		for _, child := range children {
			childParts := append([]string{child.Tag}, controlNodeAttrSummary(child.Attrs)...)
			parts = append(parts, "<"+strings.Join(childParts, " ")+">")
		}
	}
	return strings.Join(parts, " ")
}

// controlNodeAttrSummary 取 failure/stream:error 等控制节点里安全的诊断属性(reason 码等),
// 刻意排除 from/to/id 等可能含 JID/手机号的字段,长值截断。
func controlNodeAttrSummary(attrs map[string]string) []string {
	out := make([]string, 0, len(attrs))
	for _, key := range []string{"type", "code", "class", "reason", "text", "location", "xmlns", "t", "expire", "kind", "stat"} {
		if value := strings.TrimSpace(attrs[key]); value != "" {
			if len(value) > 48 {
				value = value[:48] + "…"
			}
			out = append(out, key+"="+value)
		}
	}
	return out
}

func payloadRefForEnc(accountID string, payload []byte) string {
	return "native-enc:" + stableID(accountID+":"+hexKey(payload))
}

func timeNowMillis() int64 {
	return time.Now().UnixMilli()
}
