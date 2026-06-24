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
	reverse := make(map[string]tokenRef, len(chatdPrimaryTokens))
	for idx, value := range chatdPrimaryTokens {
		if value != "" {
			reverse[value] = tokenRef{prefix: -1, index: idx}
		}
	}
	for bucket, values := range chatdSecondaryTokens {
		for idx, value := range values {
			// 同名优先单字节 primary token(更短),仅在 primary 不含时登记二级 token。
			if value != "" {
				if _, ok := reverse[value]; !ok {
					reverse[value] = tokenRef{prefix: 236 + bucket, index: idx}
				}
			}
		}
	}
	return &tokenDictionary{primary: chatdPrimaryTokens, secondary: chatdSecondaryTokens, reverse: reverse}
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
