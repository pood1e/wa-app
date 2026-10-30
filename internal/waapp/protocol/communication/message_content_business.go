package communication

import (
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

func businessMessageText(message *waE2E.Message) string {
	switch {
	case message.TemplateMessage != nil:
		return templateMessageText(message.GetTemplateMessage())
	case message.HighlyStructuredMessage != nil:
		return templateMessageText(message.GetHighlyStructuredMessage().GetHydratedHsm())
	case message.InteractiveMessage != nil:
		return interactiveMessageText(message.GetInteractiveMessage())
	case message.ButtonsMessage != nil:
		buttons := message.GetButtonsMessage()
		return joinMessageText(buttons.GetText(), buttons.GetContentText(), buttons.GetFooterText())
	case message.ListMessage != nil:
		list := message.GetListMessage()
		return joinMessageText(list.GetTitle(), list.GetDescription(), list.GetFooterText())
	case message.TemplateButtonReplyMessage != nil:
		return strings.TrimSpace(message.GetTemplateButtonReplyMessage().GetSelectedDisplayText())
	case message.ButtonsResponseMessage != nil:
		return strings.TrimSpace(message.GetButtonsResponseMessage().GetSelectedDisplayText())
	case message.ListResponseMessage != nil:
		reply := message.GetListResponseMessage()
		return joinMessageText(reply.GetTitle(), reply.GetDescription())
	case message.InteractiveResponseMessage != nil:
		return strings.TrimSpace(message.GetInteractiveResponseMessage().GetBody().GetText())
	default:
		return ""
	}
}

func templateMessageText(message *waE2E.TemplateMessage) string {
	for _, hydrated := range []*waE2E.TemplateMessage_HydratedFourRowTemplate{
		message.GetHydratedTemplate(), message.GetHydratedFourRowTemplate(),
	} {
		if text := joinMessageText(hydrated.GetHydratedTitleText(), hydrated.GetHydratedContentText(), hydrated.GetHydratedFooterText()); text != "" {
			return text
		}
	}
	return interactiveMessageText(message.GetInteractiveMessageTemplate())
}

func interactiveMessageText(message *waE2E.InteractiveMessage) string {
	return joinMessageText(message.GetHeader().GetTitle(), message.GetHeader().GetSubtitle(), message.GetBody().GetText(), message.GetFooter().GetText())
}

func joinMessageText(parts ...string) string {
	var lines []string
	for _, part := range parts {
		if text := strings.TrimSpace(part); text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}
