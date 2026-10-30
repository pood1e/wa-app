package communication

import (
	"context"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (c *Connection) SubscribePresence(ctx context.Context, contactRef, pushName string) error {
	jid, err := types.ParseJID(contactRef)
	if err != nil || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) {
		return fmt.Errorf("presence requires a user contact")
	}
	ctx, finish := c.operationContext(ctx)
	defer finish()
	if err := c.LockInteractiveIQLocked(ctx); err != nil {
		return err
	}
	defer c.UnlockInteractiveIQLocked()
	if c.Session == nil {
		return fmt.Errorf("account connection is not ready")
	}
	state, err := c.loadState(ctx, c.Input.ClientProfileID)
	if err != nil {
		return err
	}
	finishWrite, err := c.Session.bindDeadline(ctx, time.Now().Add(ContextBoundTimeout(ctx, 3*time.Second)))
	if err != nil {
		return err
	}
	defer finishWrite()
	if err := ctx.Err(); err != nil {
		return err
	}
	attrs := map[string]string{"type": string(types.PresenceAvailable)}
	if name := shared.FirstNonEmpty(state.PushName, pushName); name != "" {
		attrs["name"] = name
	} else {
		return fmt.Errorf("account profile name is required for presence")
	}
	if err := c.Session.Transport.SendNode(wire.ChatdNode{Tag: "presence", Attrs: attrs}); err != nil {
		c.closeFailedSessionLocked()
		return err
	}
	node := wire.ChatdNode{Tag: "presence", Attrs: map[string]string{"type": "subscribe", "to": jid.ToNonAD().String()}}
	if token := PrivacyTokenForJID(state, jid.ToNonAD().String(), c.clock.Now()); len(token) > 0 {
		node.Content = []wire.ChatdNode{{Tag: "tctoken", Content: token}}
	}
	if err := c.Session.Transport.SendNode(node); err != nil {
		c.closeFailedSessionLocked()
		return err
	}
	return nil
}
