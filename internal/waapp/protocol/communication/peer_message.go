package communication

import (
	"context"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func ownPeerControl(message *waE2E.Message) bool {
	control := message.GetProtocolMessage()
	if control == nil || control.Type == nil {
		return false
	}
	switch control.GetType() {
	case waE2E.ProtocolMessage_APP_STATE_SYNC_KEY_SHARE, waE2E.ProtocolMessage_HISTORY_SYNC_NOTIFICATION:
		return true
	default:
		return false
	}
}

func (p *messagePreparation) peerMessage(ctx context.Context, target, id string, message *waE2E.Message) (wire.ChatdNode, error) {
	if _, err := ownLinkedDevice(p.current, target); err != nil {
		return wire.ChatdNode{}, err
	}
	plaintext, err := proto.Marshal(message)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	defer clear(plaintext)
	participants, err := p.encryptForDevices(ctx, []string{target}, plaintext)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	children := []wire.ChatdNode{{Tag: "meta", Attrs: map[string]string{"appdata": "default"}}}
	children = append(children, wire.ChatdChildren(participants[0])...)
	return wire.ChatdNode{Tag: "message", Attrs: map[string]string{"id": id, "to": target, "type": "text", "category": "peer"}, Content: children}, nil
}
