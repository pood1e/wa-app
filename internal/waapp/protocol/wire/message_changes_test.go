package wire

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestMessageChangeSchemaMatchesNativeAPK(t *testing.T) {
	for _, schema := range []struct {
		message proto.Message
		fields  map[protoreflect.Name]protoreflect.FieldNumber
	}{
		{new(waE2E.Message), map[protoreflect.Name]protoreflect.FieldNumber{"protocolMessage": 12, "reactionMessage": 46, "editedMessage": 58, "deviceSentMessage": 31, "ephemeralMessage": 40, "viewOnceMessage": 37, "viewOnceMessageV2": 55, "viewOnceMessageV2Extension": 59, "documentWithCaptionMessage": 53}},
		{new(waE2E.ProtocolMessage), map[protoreflect.Name]protoreflect.FieldNumber{"key": 1, "type": 2, "editedMessage": 14, "timestampMS": 15}},
		{new(waE2E.ReactionMessage), map[protoreflect.Name]protoreflect.FieldNumber{"key": 1, "text": 2, "groupingKey": 3, "senderTimestampMS": 4}},
		{new(waCommon.MessageKey), map[protoreflect.Name]protoreflect.FieldNumber{"remoteJID": 1, "fromMe": 2, "ID": 3, "participant": 4}},
		{new(waE2E.FutureProofMessage), map[protoreflect.Name]protoreflect.FieldNumber{"message": 1}},
		{new(waE2E.DeviceSentMessage), map[protoreflect.Name]protoreflect.FieldNumber{"destinationJID": 1, "message": 2}},
	} {
		fields := schema.message.ProtoReflect().Descriptor().Fields()
		for name, number := range schema.fields {
			field := fields.ByName(name)
			if field == nil || field.Number() != number {
				t.Fatalf("%s.%s differs from the native APK field number %d", schema.message.ProtoReflect().Descriptor().FullName(), name, number)
			}
		}
	}
	if waE2E.ProtocolMessage_REVOKE != 0 || waE2E.ProtocolMessage_MESSAGE_EDIT != 14 {
		t.Fatal("protocol types differ from native CF2 enum values")
	}
}

func TestMessageChangesPreserveNativeKeyPresenceAndTimestamps(t *testing.T) {
	key := &waCommon.MessageKey{RemoteJID: proto.String("120363000001@g.us"), FromMe: proto.Bool(false), ID: proto.String("original"), Participant: proto.String("100001@lid")}
	at := time.UnixMilli(1700000000123)
	edit := BuildMessageEdit(key, "修改后的正文", at)
	change := edit.GetEditedMessage().GetMessage().GetProtocolMessage()
	if change.GetType() != waE2E.ProtocolMessage_MESSAGE_EDIT || change.GetTimestampMS() != at.UnixMilli() || !proto.Equal(change.GetKey(), key) || change.GetEditedMessage().GetConversation() != "修改后的正文" {
		t.Fatal("edit lost its native envelope, key, text or millisecond timestamp")
	}
	for _, text := range []string{"👍", ""} {
		reaction := BuildMessageReaction(key, text, at).GetReactionMessage()
		if !proto.Equal(reaction.GetKey(), key) || reaction.Text == nil || reaction.GetText() != text || reaction.GetSenderTimestampMS() != at.UnixMilli() {
			t.Fatal("reaction or removal lost its explicit text and timestamp")
		}
	}
	revoke := BuildMessageRevoke(key)
	raw, err := proto.Marshal(revoke)
	if err != nil {
		t.Fatal(err)
	}
	decoded := new(waE2E.Message)
	if err := proto.Unmarshal(raw, decoded); err != nil || decoded.GetProtocolMessage().Type == nil || decoded.GetProtocolMessage().GetType() != waE2E.ProtocolMessage_REVOKE || !proto.Equal(decoded.GetProtocolMessage().GetKey(), key) {
		t.Fatal("revoke lost its explicit zero-valued protocol type", err)
	}
}
