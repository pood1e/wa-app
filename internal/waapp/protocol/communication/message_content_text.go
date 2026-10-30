package communication

import (
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func messageBodyText(message *waE2E.Message) (string, waappv1.MessageContentType) {
	if message == nil {
		return "", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL
	}
	switch {
	case message.Conversation != nil:
		return message.GetConversation(), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT
	case message.ExtendedTextMessage != nil:
		return message.GetExtendedTextMessage().GetText(), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT
	case message.ImageMessage != nil:
		return mediaCaption("[图片]", message.GetImageMessage().GetCaption()), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_IMAGE
	case message.VideoMessage != nil:
		return mediaCaption("[视频]", message.GetVideoMessage().GetCaption()), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_VIDEO
	case message.PtvMessage != nil:
		return mediaCaption("[圆形视频]", message.GetPtvMessage().GetCaption()), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_VIDEO
	case message.AudioMessage != nil:
		return "[语音]", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_AUDIO
	case message.DocumentMessage != nil:
		document := message.GetDocumentMessage()
		return mediaCaption("[文件]", strings.Join([]string{document.GetFileName(), document.GetCaption()}, " ")), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_DOCUMENT
	case message.StickerMessage != nil:
		return "[贴纸]", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_STICKER
	case message.ContactMessage != nil:
		return mediaCaption("[联系人]", message.GetContactMessage().GetDisplayName()), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTACT
	case message.ContactsArrayMessage != nil:
		return mediaCaption("[联系人]", message.GetContactsArrayMessage().GetDisplayName()), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTACT
	case message.LocationMessage != nil:
		return mediaCaption("[位置]", message.GetLocationMessage().GetName()), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_LOCATION
	case message.LiveLocationMessage != nil:
		return mediaCaption("[实时位置]", message.GetLiveLocationMessage().GetCaption()), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_LOCATION
	case message.PollCreationMessage != nil:
		return mediaCaption("[投票]", message.GetPollCreationMessage().GetName()), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_POLL
	case message.PollCreationMessageV2 != nil:
		return mediaCaption("[投票]", message.GetPollCreationMessageV2().GetName()), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_POLL
	case message.PollCreationMessageV3 != nil:
		return mediaCaption("[投票]", message.GetPollCreationMessageV3().GetName()), waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_POLL
	case message.ProtocolMessage != nil || message.ReactionMessage != nil:
		return "", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL
	case controlMetadataOnly(message):
		return "", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL
	default:
		if text := businessMessageText(message); text != "" {
			return text, waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT
		}
		return "[暂不支持的消息]", waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_UNSUPPORTED
	}
}

func controlMetadataOnly(message *waE2E.Message) bool {
	return proto.Equal(message, &waE2E.Message{SenderKeyDistributionMessage: message.SenderKeyDistributionMessage, MessageContextInfo: message.MessageContextInfo})
}

func mediaCaption(label, caption string) string {
	return strings.TrimSpace(label + " " + strings.TrimSpace(caption))
}
