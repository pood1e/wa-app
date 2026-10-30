package communication

import (
	"context"
	"errors"
	"strconv"
	"time"

	"go.mau.fi/whatsmeow/types"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

const maxSignalRetries = 4

type signalRetryRequest struct {
	chat   types.JID
	sender types.JID
	id     string
	count  uint32
}

func (s *ChatdSession) parseSignalRetry(node wire.ChatdNode) (signalRetryRequest, bool) {
	if node.Tag != "receipt" || node.Attrs["type"] != "retry" {
		return signalRetryRequest{}, false
	}
	retry, exists := ChatdChild(node, "retry")
	count, err := strconv.ParseUint(retry.Attrs["count"], 10, 32)
	id := retry.Attrs["id"]
	if !exists || err != nil || count < 1 || count > maxSignalRetries || id == "" || len(id) > 256 || node.Attrs["id"] != id {
		return signalRetryRequest{}, false
	}
	source, ok := s.messageSource(node)
	if !ok || source.sender.IsEmpty() || source.fromMe {
		return signalRetryRequest{}, false
	}
	return signalRetryRequest{chat: source.chat, sender: source.sender, id: id, count: uint32(count)}, true
}

func retryStateKey(chat, sender, id string) string {
	return shared.StableID(chat + "\x00" + sender + "\x00" + id)
}

func protocolRetryKey(current *state.NativeState, chat, sender, id string) string {
	if jid, err := types.ParseJID(chat); err == nil {
		chat = signalIdentityForDevice(current, jid.ToNonAD().String())
	}
	return retryStateKey(chat, signalIdentityForDevice(current, sender), id)
}

func queueSignalRetries(current *state.NativeState, nodes []wire.ChatdNode) bool {
	changed := false
	codec := wire.NewBinaryNodeCodec()
	for _, node := range nodes {
		retry, _ := ChatdChild(node, "retry")
		key := retryStateKey(node.Attrs["from"], node.Attrs["participant"], node.Attrs["id"]+":"+retry.Attrs["count"])
		if _, exists := current.PendingSignalRetries[key]; exists {
			continue
		}
		if len(current.PendingSignalRetries) >= 128 {
			break
		}
		encoded, err := codec.EncodeNode(node)
		if err != nil {
			continue
		}
		current.PendingSignalRetries[key] = &protocolv1.PendingSignalRetry{Receipt: append([]byte{0}, encoded...), ReceivedAt: time.Now().Unix()}
		changed = true
	}
	return changed
}

func (c *Connection) flushSignalRetries(ctx context.Context, current *state.NativeState) error {
	pruneSentMessages(current, c.clock.Now())
	codec := wire.NewBinaryNodeCodec()
	processed := 0
	changed := len(current.PendingSignalRetries) > 0
	for key, pending := range current.PendingSignalRetries {
		if processed >= 2 {
			break
		}
		node, err := codec.DecodeNodePayload(pending.GetReceipt())
		if err != nil || pending.GetAttempts() >= maxSignalRetries || time.Since(time.Unix(pending.GetReceivedAt(), 0)) > 10*time.Minute {
			delete(current.PendingSignalRetries, key)
			continue
		}
		processed++
		if ack, ok := wire.BuildAckForNodeWithAttrs(node, nil); ok {
			if err := sendProtocolNode(ctx, c.Session, ack); err != nil {
				return err
			}
		}
		request, valid := c.Session.parseSignalRetry(node)
		if !valid {
			delete(current.PendingSignalRetries, key)
			continue
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		preparation := messagePreparation{session: c.Session, current: current, input: c.Input, timeout: 10 * time.Second}
		var response wire.ChatdNode
		if len(pending.GetResponse()) > 0 {
			response, err = codec.DecodeNodePayload(pending.GetResponse())
		} else {
			response, err = preparation.retryMessage(attemptCtx, node, request)
			if err == nil && response.Tag != "" {
				var encoded []byte
				encoded, err = codec.EncodeNode(response)
				if err == nil {
					pending.Response = append([]byte{0}, encoded...)
				}
			}
		}
		c.BufferPendingLocked(preparation.items, preparation.update)
		pending.Attempts++
		// Commit the next ratchet and retry count before emitting its ciphertext.
		if saveErr := c.SaveState(attemptCtx, c.Input.ClientProfileID, *current); saveErr != nil {
			cancel()
			return saveErr
		}
		if err == nil && response.Tag != "" {
			err = sendProtocolNode(attemptCtx, c.Session, response)
		}
		cancel()
		if err == nil || errors.Is(err, errSignalRetryRejected) {
			delete(current.PendingSignalRetries, key)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if !changed {
		return nil
	}
	return c.SaveState(ctx, c.Input.ClientProfileID, *current)
}

var errSignalRetryRejected = errors.New("signal retry request is not authorized")

func sendProtocolNode(ctx context.Context, session *ChatdSession, node wire.ChatdNode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	finish, err := session.bindDeadline(ctx, time.Now().Add(ContextBoundTimeout(ctx, DefaultMessageReceiptTimeout)))
	if err != nil {
		return err
	}
	defer finish()
	return session.Transport.SendNode(node)
}

func (c *Connection) flushSignalRetriesIfActive(ctx context.Context, current *state.NativeState, update ChatdSessionUpdate) error {
	if update.AccountLogout != nil {
		return nil
	}
	return c.flushSignalRetries(ctx, current)
}
