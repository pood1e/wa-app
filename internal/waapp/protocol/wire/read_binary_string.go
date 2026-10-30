package wire

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func signalRetryCount(value string) uint32 {
	count, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(count)
}

func BuildPingNode() ChatdNode {
	stamp := TimeNowMillis()
	return ChatdNode{Tag: "iq", Attrs: map[string]string{"id": fmt.Sprintf("go-%d", stamp), "type": "get", "to": "s.whatsapp.net", "xmlns": "w:p"}}
}

func BuildAckForNode(node ChatdNode) (ChatdNode, bool) {
	return BuildAckForNodeWithAttrs(node, nil)
}

func BuildAckForNodeWithAttrs(node ChatdNode, extra map[string]string) (ChatdNode, bool) {
	nodeID := node.Attrs["id"]
	sender := node.Attrs["from"]
	if sender == "" || nodeID == "" && node.Tag != "receipt" {
		return ChatdNode{}, false
	}
	switch node.Tag {
	case "notification", "message", "receipt":
	default:
		return ChatdNode{}, false
	}
	attrs := map[string]string{"to": sender, "class": node.Tag}
	if nodeID != "" {
		attrs["id"] = nodeID
	}
	if node.Tag != "message" && node.Attrs["type"] != "" {
		attrs["type"] = node.Attrs["type"]
	}
	for _, name := range []string{"participant", "recipient"} {
		if value := node.Attrs[name]; value != "" {
			attrs[name] = value
		}
	}
	AddAckExtraAttrs(attrs, extra)
	return ChatdNode{Tag: "ack", Attrs: attrs}, true
}

func AddAckExtraAttrs(attrs map[string]string, extra map[string]string) {
	for key, value := range extra {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			attrs[key] = value
		}
	}
}

func IterEncPayloads(node ChatdNode) []ChatdEncPayload {
	out := []ChatdEncPayload{}
	var walk func(ChatdNode, []string, ChatdMessageRefs)
	walk = func(current ChatdNode, path []string, refs ChatdMessageRefs) {
		currentPath := append(append([]string{}, path...), current.Tag)
		if id := current.Attrs["id"]; id != "" {
			refs.StanzaID = id
		}
		if stanzaTimestamp := current.Attrs["t"]; stanzaTimestamp != "" {
			refs.StanzaTimestamp = stanzaTimestamp
		}
		if current.Tag == "message" {
			senderLID := FirstChatdAttr(current.Attrs, "sender_lid", "participant_lid")
			senderPN := FirstChatdAttr(current.Attrs, "sender_pn", "sender_pn_jid", "participant_pn", "participant_pn_jid")
			peerLID := FirstChatdAttr(current.Attrs, "peer_recipient_lid", "recipient_latest_lid", "recipient_lid", "peer_lid")
			peerPN := FirstChatdAttr(current.Attrs, "peer_recipient_pn", "peer_recipient_pn_jid", "recipient_pn", "recipient_pn_jid", "peer_pn", "peer_pn_jid")
			contactLID := FirstChatdAttr(current.Attrs, "contact_lid", "author_lid", "creator_lid", "caller_lid", "invitee_lid")
			contactPN := FirstChatdAttr(current.Attrs, "contact_pn", "contact_pn_jid", "author_pn", "author_pn_jid", "creator_pn", "creator_pn_jid", "caller_pn", "caller_pn_jid", "invitee_pn", "invitee_pn_jid")
			refs.Contact = shared.FirstNonEmpty(FirstChatdAttr(current.Attrs, "from"), peerLID, senderLID, FirstChatdAttr(current.Attrs, "participant"), refs.Contact)
			refs.Sender = shared.FirstNonEmpty(senderLID, contactLID, FirstChatdAttr(current.Attrs, "participant", "from"), refs.Sender, refs.Contact)
			refs.ContactPN = shared.FirstNonEmpty(FirstChatdAttr(current.Attrs, "from_pn", "from_pn_jid", "pn_jid", "new_jid"), peerPN, contactPN, senderPN, refs.ContactPN)
			refs.SenderPN = shared.FirstNonEmpty(FirstChatdAttr(current.Attrs, "participant_pn", "participant_pn_jid", "sender_pn", "sender_pn_jid"), contactPN, FirstChatdAttr(current.Attrs, "from_pn", "from_pn_jid", "pn_jid"), refs.SenderPN, refs.ContactPN)
			refs.NotifyName = shared.FirstNonEmpty(FirstChatdAttr(current.Attrs, "notify", "notify_name", "display_name", "contact_push_name"), refs.NotifyName)
			refs.ParticipantUsername = shared.FirstNonEmpty(FirstChatdAttr(current.Attrs, "participant_username", "peer_recipient_username", "contact_username", "username"), refs.ParticipantUsername)
			refs.ContactHints = DedupeWAContactHints(append(refs.ContactHints, ContactHintsFromChatdNode(current)...))
		}
		if current.Tag == "enc" {
			if raw, ok := current.Content.([]byte); ok {
				out = append(out, ChatdEncPayload{
					StanzaID:            refs.StanzaID,
					StanzaTimestamp:     refs.StanzaTimestamp,
					Contact:             refs.Contact,
					Sender:              refs.Sender,
					ContactPN:           refs.ContactPN,
					SenderPN:            refs.SenderPN,
					NotifyName:          refs.NotifyName,
					ParticipantUsername: refs.ParticipantUsername,
					ContactHints:        refs.ContactHints,
					EncType:             shared.FirstNonEmpty(current.Attrs["type"], current.Attrs["v"], "auto"),
					Path:                strings.Join(currentPath, "/"),
					Payload:             raw,
					RetryCount:          signalRetryCount(current.Attrs["count"]),
				})
			}
		}
		if children, ok := current.Content.([]ChatdNode); ok {
			for _, child := range children {
				walk(child, currentPath, refs)
			}
		}
	}
	walk(node, nil, ChatdMessageRefs{})
	return out
}

type ChatdMessageRefs struct {
	StanzaID            string
	StanzaTimestamp     string
	Contact             string
	Sender              string
	ContactPN           string
	SenderPN            string
	NotifyName          string
	ParticipantUsername string
	ContactHints        []wacore.WAContactHint
}

func RoutingInfoFromNode(node ChatdNode) string {
	var found string
	var walk func(ChatdNode)
	walk = func(current ChatdNode) {
		if found != "" {
			return
		}
		if current.Tag == "routing_info" {
			found = RoutingInfoFromContent(current.Content)
			return
		}
		if children, ok := current.Content.([]ChatdNode); ok {
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

func RoutingInfoFromContent(content any) string {
	switch value := content.(type) {
	case []byte:
		if len(value) == 0 || len(value) > 256 {
			return ""
		}
		return B64u(value)
	case string:
		return NormalizeChatRoutingInfo(value)
	default:
		return ""
	}
}

func NodePayloadSummary(node ChatdNode) string {
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

func IsChatdTerminalNode(node ChatdNode) bool {
	switch node.Tag {
	case "stream:error", "failure", "error":
		return true
	default:
		return false
	}
}

func ControlNodeSummary(node ChatdNode) string {
	parts := append([]string{node.Tag}, ControlNodeAttrSummary(node.Attrs)...)
	if children, ok := node.Content.([]ChatdNode); ok && len(children) > 0 {
		for _, child := range children {
			childParts := append([]string{child.Tag}, ControlNodeAttrSummary(child.Attrs)...)
			parts = append(parts, "<"+strings.Join(childParts, " ")+">")
		}
	}
	return strings.Join(parts, " ")
}

// controlNodeAttrSummary 取 failure/stream:error 等控制节点里安全的诊断属性(reason 码等),
// 刻意排除 from/to/id 等可能含 JID/手机号的字段,长值截断。
func ControlNodeAttrSummary(attrs map[string]string) []string {
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

func PayloadRefForEnc(accountID string, payload []byte) string {
	return "native-enc:" + shared.StableID(accountID+":"+HexKey(payload))
}

func TimeNowMillis() int64 {
	return time.Now().UnixMilli()
}
