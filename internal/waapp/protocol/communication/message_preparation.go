package communication

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type messagePreparation struct {
	session *ChatdSession
	current *state.NativeState
	input   wacore.EngineMessageInput
	timeout time.Duration
	items   []ChatdReceivedItem
	update  ChatdSessionUpdate
}

func (p *messagePreparation) query(ctx context.Context, node wire.ChatdNode, timeoutMessage string) (wire.ChatdNode, error) {
	response, items, update, err := p.session.SendIQ(ctx, p.input, node, p.timeout, timeoutMessage)
	p.record(items, update)
	if err == nil {
		err = ChatdIQError(response)
	}
	return response, err
}

func (p *messagePreparation) record(items []ChatdReceivedItem, update ChatdSessionUpdate) {
	p.items = append(p.items, items...)
	p.update = MergeChatdSessionUpdate(p.update, update)
	_, payloads, _ := SplitReceivedItems(items)
	ApplyChatdReceiveState(p.current, p.input, payloads, update)
}

func (p *messagePreparation) encryptForDevices(ctx context.Context, devices []string, plaintext []byte) ([]wire.ChatdNode, error) {
	result := make([]wire.ChatdNode, 0, len(devices))
	for _, device := range devices {
		identity := signalIdentityForDevice(p.current, device)
		if err := migrateSignalDeviceIdentity(p.current, device, identity); err != nil {
			return nil, err
		}
		items, update, err := ensureDirectSignalSession(ctx, p.session, p.current, p.input, identity, p.timeout)
		p.record(items, update)
		if err != nil {
			return nil, err
		}
		raw, kind, err := encryptDirectSignalPayload(p.current, identity, plaintext)
		if err != nil {
			return nil, err
		}
		result = append(result, wire.ChatdNode{Tag: "to", Attrs: map[string]string{"jid": device}, Content: []wire.ChatdNode{{
			Tag: "enc", Attrs: map[string]string{"type": kind, "v": "2"}, Content: raw,
		}}})
	}
	return result, nil
}

func (p *messagePreparation) directMessage(ctx context.Context, target, id string, message *waE2E.Message) (wire.ChatdNode, error) {
	devices, err := p.recipientDevices(ctx, []string{target})
	if err != nil {
		return wire.ChatdNode{}, err
	}
	plaintext, err := proto.Marshal(message)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	participants, err := p.encryptForDevices(ctx, devices, plaintext)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	children := []wire.ChatdNode{{Tag: "participants", Content: participants}}
	identity := signalIdentityForDevice(p.current, target)
	token := PrivacyTokenForJID(*p.current, identity, time.Now())
	if len(token) == 0 && identity != target {
		token = PrivacyTokenForJID(*p.current, target, time.Now())
	}
	if len(token) > 0 {
		children = append(children, wire.ChatdNode{Tag: "tctoken", Content: token})
	}
	return wire.ChatdNode{Tag: "message", Attrs: map[string]string{"id": id, "to": target, "type": "text"}, Content: children}, nil
}
