package communication

import (
	"context"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (c *Connection) activateSession(ctx context.Context, engine *Engine, input wacore.EngineMessageInput, current state.NativeState, session *ChatdSession) error {
	request := wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": NewTextProviderMessageID(""), "type": "set", "xmlns": "passive", "to": "s.whatsapp.net"},
		Content: []wire.ChatdNode{{Tag: "active"}}}
	response, items, update, err := session.SendIQ(ctx, input, request,
		ContextBoundTimeout(ctx, DefaultAccountIQTimeout), "connection activation response timed out")
	c.BufferPendingLocked(items, update)
	_, payloads, _ := SplitReceivedItems(items)
	if ApplyChatdReceiveState(&current, input, payloads, update) {
		if saveErr := engine.SaveState(ctx, input.ClientProfileID, current); saveErr != nil {
			return saveErr
		}
	}
	if err != nil {
		return err
	}
	return ChatdIQError(response)
}
