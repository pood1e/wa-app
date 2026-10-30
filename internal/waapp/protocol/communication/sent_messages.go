package communication

import (
	"errors"
	"slices"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

const sentMessageLifetime = 48 * time.Hour

func sentMessageKey(chat, id string) string { return shared.StableID(chat + "\x00" + id) }

func findSentMessage(current *state.NativeState, chat, id string) *protocolv1.SentMessage {
	if message := current.SentMessages[sentMessageKey(chat, id)]; message != nil {
		return message
	}
	for _, hint := range current.ContactHints {
		other := ""
		if chat == hint.PNJID {
			other = hint.LIDJID
		}
		if chat == hint.LIDJID {
			other = hint.PNJID
		}
		if other != "" {
			if message := current.SentMessages[sentMessageKey(other, id)]; message != nil {
				return message
			}
		}
	}
	return nil
}

func rememberSentMessage(current *state.NativeState, body *waE2E.Message, node wire.ChatdNode, at time.Time) (*protocolv1.SentMessage, error) {
	if body == nil {
		return nil, errors.New("message body is required")
	}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(body)
	if err != nil {
		return nil, err
	}
	current.EnsureMaps()
	id, chat := node.Attrs["id"], node.Attrs["to"]
	if previous := findSentMessage(current, chat, id); previous != nil {
		if !sameSentMessage(previous.GetMessage(), body) {
			return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "message id is already used by another message", false)
		}
		at = time.Unix(previous.GetTimestamp(), 0)
	}
	var devices []string
	if participants, exists := ChatdChild(node, "participants"); exists {
		for _, child := range wire.ChatdChildren(participants) {
			if child.Tag == "to" {
				devices = append(devices, child.Attrs["jid"])
			}
		}
	}
	record := &protocolv1.SentMessage{ProviderMessageId: id, ContactRef: chat, Message: data, RecipientDevices: devices, Timestamp: at.Unix(), Edit: node.Attrs["edit"]}
	current.SentMessages[sentMessageKey(chat, id)] = record
	pruneSentMessages(current, at)
	return record, nil
}

func sameSentMessage(data []byte, next *waE2E.Message) bool {
	previous := new(waE2E.Message)
	if proto.Unmarshal(data, previous) != nil {
		return false
	}
	next = proto.Clone(next).(*waE2E.Message)
	for _, raw := range []*waE2E.Message{previous, next} {
		message := unwrapMessage(raw).Message
		if reaction := message.GetReactionMessage(); reaction != nil {
			reaction.SenderTimestampMS = nil
		}
		if control := message.GetProtocolMessage(); control != nil {
			control.TimestampMS = nil
		}
	}
	return proto.Equal(previous, next)
}

func pruneSentMessages(current *state.NativeState, now time.Time) {
	for key, record := range current.SentMessages {
		if now.Sub(time.Unix(record.GetTimestamp(), 0)) > sentMessageLifetime {
			delete(current.SentMessages, key)
		}
	}
	for len(current.SentMessages) > 2048 {
		var oldestKey string
		var oldest int64
		for key, record := range current.SentMessages {
			if oldestKey == "" || record.GetTimestamp() < oldest {
				oldestKey, oldest = key, record.GetTimestamp()
			}
		}
		delete(current.SentMessages, oldestKey)
	}
	for key, record := range current.SignalRetries {
		if now.Sub(time.Unix(record.GetUpdatedAt(), 0)) > sentMessageLifetime {
			delete(current.SignalRetries, key)
		}
	}
}

func sentToDevice(current *state.NativeState, message *protocolv1.SentMessage, target string) bool {
	identity := signalIdentityForDevice(current, target)
	return slices.ContainsFunc(message.GetRecipientDevices(), func(device string) bool {
		return device == target || signalIdentityForDevice(current, device) == identity
	})
}

func applyMessageNodeAttributes(node *wire.ChatdNode, body *waE2E.Message) {
	message := unwrapMessage(body).Message
	_, contentType := messageBodyText(message)
	node.Attrs["type"] = "text"
	if message.GetReactionMessage() != nil {
		node.Attrs["type"] = "reaction"
	} else if contentType >= waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_IMAGE && contentType <= waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_STICKER {
		node.Attrs["type"] = "media"
	}
	control := message.GetProtocolMessage()
	switch {
	case control != nil && control.GetKey() != nil && control.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT:
		node.Attrs["edit"] = string(types.EditAttributeMessageEdit)
	case control != nil && control.GetKey() != nil && control.GetType() == waE2E.ProtocolMessage_REVOKE:
		node.Attrs["edit"] = string(types.EditAttributeSenderRevoke)
	case message.GetReactionMessage() != nil && message.GetReactionMessage().GetText() == "":
		node.Attrs["edit"] = string(types.EditAttributeSenderRevoke)
	}
	if node.Attrs["edit"] != "" || node.Attrs["type"] == "reaction" {
		hideMessageDecryptFailure(node)
	}
}

func hideMessageDecryptFailure(node *wire.ChatdNode) {
	if node.Tag == "enc" {
		if node.Attrs == nil {
			node.Attrs = make(map[string]string)
		}
		node.Attrs["decrypt-fail"] = "hide"
	}
	children := wire.ChatdChildren(*node)
	for index := range children {
		hideMessageDecryptFailure(&children[index])
	}
	if len(children) > 0 {
		node.Content = children
	}
}
