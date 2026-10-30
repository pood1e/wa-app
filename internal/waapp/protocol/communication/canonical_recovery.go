package communication

import (
	"context"
	"errors"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func canonicalNonceFromNode(node wire.ChatdNode) string {
	if node.Tag != "notification" || node.Attrs["from"] != "s.whatsapp.net" {
		return ""
	}
	if nonce, ok := ChatdChild(node, "recovery_nonce"); ok && nonce.Attrs["use_case"] == "11" {
		return nonce.Attrs["code"]
	}
	if nonce, ok := ChatdChild(node, "account_recovery_nonce"); ok && ChatdNodeValue(node, "use_case") == "11" {
		switch value := nonce.Content.(type) {
		case []byte:
			return string(value)
		case string:
			return value
		}
	}
	return ""
}

func (s lockedIQSender) WaitCanonicalNonce(ctx context.Context) (string, error) {
	c := s.connection
	if c.Session == nil {
		return "", errors.New("canonical recovery account connection is closed")
	}
	session := c.Session
	deadline := time.Now().Add(DefaultAccountIQTimeout)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	update := session.Update()
	defer func() {
		c.BufferPendingLocked(nil, update)
		session.canonicalNonce = ""
	}()
	for session.canonicalNonce == "" {
		next, items, err := session.receiveBatchNode(ctx, c.Input, update, deadline, c.clock)
		update = next
		c.BufferPendingLocked(items, ChatdSessionUpdate{})
		if err != nil {
			if errors.Is(err, errReceiveWindowComplete) {
				return "", errors.New("canonical recovery nonce timed out")
			}
			c.closeFailedSessionLocked()
			return "", err
		}
	}
	return session.canonicalNonce, nil
}
