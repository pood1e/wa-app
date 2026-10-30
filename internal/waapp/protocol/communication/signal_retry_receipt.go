package communication

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (c *Connection) requestSignalRetry(ctx context.Context, input wacore.EngineDecryptInput) error {
	current, err := c.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return err
	}
	payload, exists := current.MessagePayloads[input.PayloadRef]
	if !exists || payload.ProviderMessageID == "" || payload.ProviderTimestamp <= 0 || ownProtocolSender(&current, payload.Sender) || time.Since(time.Unix(payload.ProviderTimestamp, 0)) > sentMessageLifetime {
		return nil
	}
	key := protocolRetryKey(&current, payload.Contact, payload.Sender, payload.ProviderMessageID)
	record := current.SignalRetries[key]
	if record.GetCompleted() {
		return nil
	}
	codec := wire.NewBinaryNodeCodec()
	var node wire.ChatdNode
	if len(record.GetPendingReceipt()) > 0 {
		node, err = codec.DecodeNodePayload(record.GetPendingReceipt())
		if err != nil {
			return err
		}
	} else {
		count := max(record.GetRequestedCount(), payload.RetryCount) + 1
		if count > maxSignalRetries {
			return nil
		}
		if payload.RetryCount < record.GetRequestedCount() && time.Since(time.Unix(record.GetUpdatedAt(), 0)) < time.Duration(5*(1<<record.GetRequestedCount()))*time.Second {
			return nil
		}
		node, err = buildSignalRetryReceipt(&current, payload, count)
		if err != nil {
			return err
		}
		encoded, err := codec.EncodeNode(node)
		if err != nil {
			return err
		}
		record = &protocolv1.SignalRetryState{RequestedCount: count, UpdatedAt: c.clock.Now().Unix(), PendingReceipt: append([]byte{0}, encoded...)}
		current.SignalRetries[key] = record
		if err := c.SaveState(ctx, input.ClientProfileID, current); err != nil {
			return err
		}
	}
	session, err := c.EnsureSessionForIQLocked(ctx, c.Input, current)
	if err != nil {
		return err
	}
	if err := sendProtocolNode(ctx, session, node); err != nil {
		return err
	}
	record.PendingReceipt = nil
	return c.SaveState(ctx, input.ClientProfileID, current)
}

func buildSignalRetryReceipt(current *state.NativeState, payload state.NativeMessagePayload, count uint32) (wire.ChatdNode, error) {
	var registration [4]byte
	binary.BigEndian.PutUint32(registration[:], uint32(current.Signal.RegistrationID))
	attrs := map[string]string{"to": payload.Contact, "id": payload.ProviderMessageID, "type": "retry"}
	if strings.HasSuffix(payload.Contact, "@g.us") {
		attrs["participant"] = payload.Sender
	}
	node := wire.ChatdNode{Tag: "receipt", Attrs: attrs, Content: []wire.ChatdNode{
		{Tag: "retry", Attrs: map[string]string{"v": "1", "count": strconv.FormatUint(uint64(count), 10), "id": payload.ProviderMessageID, "t": strconv.FormatInt(payload.ProviderTimestamp, 10), "error": "0"}},
		{Tag: "registration", Content: registration[:]},
	}}
	if count >= 2 {
		keys, err := signalRetryKeys(current)
		if err != nil {
			return wire.ChatdNode{}, err
		}
		node.Content = append(wire.ChatdChildren(node), keys)
	}
	return node, nil
}

func signalRetryKeys(current *state.NativeState) (wire.ChatdNode, error) {
	if len(current.Signal.OneTimePreKeys) >= 1024 {
		return wire.ChatdNode{}, errors.New("unconsumed retry prekey limit reached")
	}
	pair, err := wire.NewNativeCurveKeyPair()
	if err != nil {
		return wire.ChatdNode{}, err
	}
	var id uint32
	for id == 0 {
		var value [4]byte
		if _, err := rand.Read(value[:]); err != nil {
			return wire.ChatdNode{}, err
		}
		id = binary.BigEndian.Uint32(value[:]) & 0xffffff
		if _, exists := FindNativeOneTimePreKey(current.Signal.OneTimePreKeys, int(id)); exists {
			id = 0
		}
	}
	oneTime := state.NativeSignalPreKey{ID: int32(id), KeyPair: pair}
	key, err := retryPreKeyNode("key", oneTime)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	signed, err := retryPreKeyNode("skey", current.Signal.SignedPreKey)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	identity, err := current.Signal.Identity.PublicBytes()
	if err != nil {
		return wire.ChatdNode{}, err
	}
	private, err := current.Signal.Identity.PrivateBytes()
	if err != nil {
		return wire.ChatdNode{}, err
	}
	public, err := current.Signal.SignedPreKey.KeyPair.PublicBytes()
	if err != nil {
		return wire.ChatdNode{}, err
	}
	public, err = wire.WithSignalCurvePrefix(public)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	signature, err := wire.XeddsaSignCurve25519(private, public)
	if err != nil {
		return wire.ChatdNode{}, err
	}
	signed.Content = append(wire.ChatdChildren(signed), wire.ChatdNode{Tag: "signature", Content: signature})
	children := []wire.ChatdNode{{Tag: "type", Content: []byte{wire.SignalCurvePrefix}}, {Tag: "identity", Content: identity}, key, signed}
	if pq := current.Signal.PQLastResortPreKey; pq != nil {
		children = append(children, wire.ChatdNode{Tag: "pq", Content: []wire.ChatdNode{{Tag: "id", Content: uint24(uint32(pq.GetId()))}, {Tag: "value", Content: pq.GetPublic()}, {Tag: "signature", Content: pq.GetSignature()}}})
	}
	current.Signal.OneTimePreKeys = append(current.Signal.OneTimePreKeys, oneTime)
	return wire.ChatdNode{Tag: "keys", Content: children}, nil
}

func retryPreKeyNode(tag string, key state.NativeSignalPreKey) (wire.ChatdNode, error) {
	public, err := key.KeyPair.PublicBytes()
	if err != nil {
		return wire.ChatdNode{}, err
	}
	return wire.ChatdNode{Tag: tag, Content: []wire.ChatdNode{{Tag: "id", Content: uint24(uint32(key.ID))}, {Tag: "value", Content: public}}}, nil
}

func uint24(value uint32) []byte { return []byte{byte(value >> 16), byte(value >> 8), byte(value)} }
