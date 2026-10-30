package communication

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestStructuredMessageProjection(t *testing.T) {
	chat, _ := types.ParseJID("12025550124@s.whatsapp.net")
	key := &waCommon.MessageKey{ID: proto.String("original"), FromMe: proto.Bool(true)}
	image := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("caption")}}
	for _, test := range []struct {
		name        string
		message     *waE2E.Message
		text        string
		contentType waappv1.MessageContentType
		change      waappv1.MessageChangeType
	}{
		{"text", &waE2E.Message{Conversation: proto.String("body 中文🙂")}, "body 中文🙂", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT, 0},
		{"quote", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("reply"), ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("original"), Participant: proto.String(chat.String()), QuotedMessage: &waE2E.Message{Conversation: proto.String("quoted secret")}}}}, "reply", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT, 0},
		{"image", image, "[图片] caption", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_IMAGE, 0},
		{"sender_key_and_image", &waE2E.Message{SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{GroupID: proto.String("fixture@g.us")}, ImageMessage: image.ImageMessage}, "[图片] caption", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_IMAGE, 0},
		{"sender_key_only", &waE2E.Message{SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{GroupID: proto.String("fixture@g.us")}}, "", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL, 0},
		{"metadata_only", &waE2E.Message{MessageContextInfo: &waE2E.MessageContextInfo{}}, "", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL, 0},
		{"history", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_HISTORY_SYNC_NOTIFICATION.Enum(), HistorySyncNotification: &waE2E.HistorySyncNotification{DirectPath: proto.String("/private/history/path")}}}, "", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL, 0},
		{"key_share", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_APP_STATE_SYNC_KEY_SHARE.Enum(), AppStateSyncKeyShare: &waE2E.AppStateSyncKeyShare{}}}, "", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL, 0},
		{"edit", wire.BuildMessageEdit(key, "edited secret", time.Unix(1700000000, 0)), "", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL, waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_EDIT},
		{"revoke_zero_enum", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: key}}, "", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL, waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_REVOKE},
		{"reaction", wire.BuildMessageReaction(key, "👍", time.Unix(1700000000, 0)), "", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL, waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_REACTION},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := proto.Clone(test.message)
			text, content, kind := ProjectMessage(test.message, true)
			if text != test.text || content.GetType() != test.contentType || content.GetChange().GetType() != test.change {
				t.Fatalf("projection text=%q type=%s change=%s", text, content.GetType(), content.GetChange().GetType())
			}
			if (kind == waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_SYSTEM) != (test.contentType == waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL) {
				t.Fatal("control traffic classification is inconsistent")
			}
			if test.name == "quote" && (content.GetQuote().GetProviderMessageId() != "original" || content.GetQuote().GetText().GetValue() != "quoted secret") {
				t.Fatal("quoted target was lost")
			}
			if test.change != 0 && content.GetChange().GetTargetProviderMessageId() != "original" {
				t.Fatal("change target was lost")
			}
			_, redacted, _ := ProjectMessage(test.message, false)
			if redacted.GetQuote().GetText().GetValue() != "" || redacted.GetChange().GetText().GetValue() != "" {
				t.Fatal("sensitive metadata was exposed")
			}
			raw, err := proto.Marshal(test.message)
			if err != nil {
				t.Fatal(err)
			}
			if got := NativePlaintextText(padSignalMessage(raw)); got != test.text {
				t.Fatalf("padded display=%q want=%q", got, test.text)
			}
			if !proto.Equal(before, test.message) {
				t.Fatal("projection mutated the E2E message")
			}
		})
	}
}

func TestMessageWrappersAndInvalidChange(t *testing.T) {
	message := &waE2E.Message{DeviceSentMessage: &waE2E.DeviceSentMessage{Message: &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("private")}}}}}}}}
	text, content, _ := ProjectMessage(message, true)
	if text != "[图片] private" || !content.GetViewOnce() || !content.GetEphemeral() {
		t.Fatal("nested wrapper flags were lost")
	}
	invalid := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Key: &waCommon.MessageKey{ID: proto.String("target"), FromMe: proto.Bool(true)}}}
	_, content, _ = ProjectMessage(invalid, true)
	if content.GetChange() != nil {
		t.Fatal("an absent protocol type became a revoke")
	}
}

func FuzzSignalMessageContent(f *testing.F) {
	for _, seed := range [][]byte{{}, {10, 2, 'o', 'k'}, {10, 0}, {255}, {0, 0, 0}, {0x0a, 0x01, 0x01}} {
		f.Add(seed)
	}
	for _, message := range []*waE2E.Message{
		{TemplateMessage: &waE2E.TemplateMessage{HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{HydratedContentText: proto.String("Your code is 012345.")}}},
		{InteractiveMessage: &waE2E.InteractiveMessage{Body: &waE2E.InteractiveMessage_Body{Text: proto.String("Confirm")}}},
	} {
		raw, err := proto.Marshal(message)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw)
		f.Add(padSignalMessage(raw))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			t.Skip()
		}
		if message, err := DecodeSignalMessage(raw); err == nil {
			ProjectMessage(message, true)
		}
	})
}
