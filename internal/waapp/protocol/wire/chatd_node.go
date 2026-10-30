package wire

import (
	"fmt"
	"strings"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"

	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type ChatdNode struct {
	Tag     string
	Attrs   map[string]string
	Content any
}

type ChatdEncPayload struct {
	StanzaID            string
	StanzaTimestamp     string
	Contact             string
	Sender              string
	ContactPN           string
	SenderPN            string
	NotifyName          string
	ParticipantUsername string
	ContactHints        []wacore.WAContactHint
	EncType             string
	Path                string
	Payload             []byte
	RetryCount          uint32
}

// BinaryNodeCodec adapts maintained WhatsApp binary framing to native chatd nodes.
type BinaryNodeCodec struct{}

func NewBinaryNodeCodec() *BinaryNodeCodec { return &BinaryNodeCodec{} }

func (c *BinaryNodeCodec) DecodeNodePayload(plaintext []byte) (ChatdNode, error) {
	body, err := CompressMaybeDecodeNodePayload(plaintext)
	if err != nil {
		return ChatdNode{}, err
	}
	node, err := waBinary.Unmarshal(body)
	if err != nil {
		return ChatdNode{}, err
	}
	return nativeChatdNode(*node), nil
}

func (c *BinaryNodeCodec) EncodeNode(node ChatdNode) ([]byte, error) {
	encoded, err := waBinary.Marshal(SDKChatdNode(node))
	if err != nil {
		return nil, err
	}
	// The encrypted transport adds the compression flag to the serialized node.
	return encoded[1:], nil
}

func SDKChatdNode(node ChatdNode) waBinary.Node {
	attrs := make(waBinary.Attrs, len(node.Attrs))
	for key, value := range node.Attrs {
		attrs[key] = value
		if jidAttribute(key) || node.Tag == "group" && key == "id" && strings.Contains(value, "@") {
			if jid, err := types.ParseJID(value); err == nil {
				attrs[key] = jid
			}
		}
	}
	content := node.Content
	switch value := content.(type) {
	case string:
		// Native node bodies are bytes; SDK strings may become packed tokens.
		content = []byte(value)
	case []ChatdNode:
		next := make([]waBinary.Node, len(value))
		for i, child := range value {
			next[i] = SDKChatdNode(child)
		}
		content = next
	}
	return waBinary.Node{Tag: node.Tag, Attrs: attrs, Content: content}
}

func nativeChatdNode(node waBinary.Node) ChatdNode {
	attrs := make(map[string]string, len(node.Attrs))
	for key, value := range node.Attrs {
		attrs[key] = fmt.Sprint(value)
	}
	content := node.Content
	if children, ok := content.([]waBinary.Node); ok {
		next := make([]ChatdNode, len(children))
		for i, child := range children {
			next[i] = nativeChatdNode(child)
		}
		content = next
	}
	return ChatdNode{Tag: node.Tag, Attrs: attrs, Content: content}
}

func jidAttribute(key string) bool {
	return key == "to" || key == "from" || key == "target" || key == "jid" || key == "lid" || key == "phone_number" || key == "participant" || key == "recipient" ||
		strings.HasSuffix(key, "_jid") || strings.HasSuffix(key, "_lid") || strings.HasSuffix(key, "_pn")
}
