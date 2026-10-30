package wire

import (
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// BuildMessageEdit wraps a versioned edit in the native future-proof envelope.
func BuildMessageEdit(key *waCommon.MessageKey, text string, at time.Time) *waE2E.Message {
	return &waE2E.Message{EditedMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Key: key, Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), TimestampMS: proto.Int64(at.UnixMilli()),
		EditedMessage: &waE2E.Message{Conversation: proto.String(text)},
	}}}}
}

// BuildMessageReaction also represents removal when text is empty.
func BuildMessageReaction(key *waCommon.MessageKey, text string, at time.Time) *waE2E.Message {
	return &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: key, Text: proto.String(text), SenderTimestampMS: proto.Int64(at.UnixMilli())}}
}

// BuildMessageRevoke preserves the explicit zero-valued protocol type.
func BuildMessageRevoke(key *waCommon.MessageKey) *waE2E.Message {
	return &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Key: key, Type: waE2E.ProtocolMessage_REVOKE.Enum()}}
}
