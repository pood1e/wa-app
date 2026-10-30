package communication

import (
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

// ProjectMessage interprets the upstream E2E schema without exposing private media material.
func ProjectMessage(raw *waE2E.Message, sensitive bool) (string, *waappv1.MessageContent, waappv1.InboundMessageKind) {
	event := unwrapMessage(raw)
	message := event.Message
	text, kind := messageBodyText(message)
	content := &waappv1.MessageContent{Type: kind, ViewOnce: event.IsViewOnce, Ephemeral: event.IsEphemeral}
	if context := messageContext(message); context.GetStanzaID() != "" {
		quoted, _ := messageBodyText(unwrapMessage(context.GetQuotedMessage()).Message)
		content.Quote = &waappv1.MessageQuote{
			ProviderMessageId: context.GetStanzaID(), SenderRef: context.GetParticipant(),
			Text: wire.SensitiveOutput(quoted, "quoted-message", sensitive),
		}
	}
	content.Change = messageChange(message, sensitive)
	if content.Change != nil || kind == waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL {
		return "", content, waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_SYSTEM
	}
	return text, content, waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE
}

type messageEnvelope struct {
	Message     *waE2E.Message
	IsViewOnce  bool
	IsEphemeral bool
}

func unwrapMessage(raw *waE2E.Message) messageEnvelope {
	event := messageEnvelope{Message: raw}
	for range 8 {
		current := event.Message
		if current == nil {
			break
		}
		if sent := current.GetDeviceSentMessage(); sent != nil {
			event.Message = sent.GetMessage()
			continue
		}
		var wrapper *waE2E.FutureProofMessage
		count := 0
		current.ProtoReflect().Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
			if field.Kind() == protoreflect.MessageKind && !field.IsList() && !field.IsMap() {
				if candidate, ok := value.Message().Interface().(*waE2E.FutureProofMessage); ok {
					wrapper, count = candidate, count+1
				}
			}
			return true
		})
		if count != 1 {
			break
		}
		event.IsViewOnce = event.IsViewOnce || current.GetViewOnceMessage() != nil || current.GetViewOnceMessageV2() != nil || current.GetViewOnceMessageV2Extension() != nil
		event.IsEphemeral = event.IsEphemeral || current.GetEphemeralMessage() != nil
		event.Message = wrapper.GetMessage()
	}
	return event
}

func messageContext(message *waE2E.Message) *waE2E.ContextInfo {
	if message == nil {
		return nil
	}
	var context *waE2E.ContextInfo
	message.ProtoReflect().Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Kind() == protoreflect.MessageKind && !field.IsList() && !field.IsMap() {
			if body, ok := value.Message().Interface().(interface{ GetContextInfo() *waE2E.ContextInfo }); ok {
				context = body.GetContextInfo()
			}
		}
		return context == nil
	})
	return context
}

func messageChange(message *waE2E.Message, sensitive bool) *waappv1.MessageChange {
	var key *waCommon.MessageKey
	change := &waappv1.MessageChange{}
	var text string
	var timestamp int64
	if reaction := message.GetReactionMessage(); reaction != nil {
		key, text, timestamp = reaction.GetKey(), reaction.GetText(), reaction.GetSenderTimestampMS()
		change.Type = waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_REACTION
	} else if control := message.GetProtocolMessage(); control != nil && control.Type != nil {
		key, timestamp = control.GetKey(), control.GetTimestampMS()
		switch control.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			change.Type = waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_REVOKE
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			var kind waappv1.MessageContentType
			text, kind = messageBodyText(unwrapMessage(control.GetEditedMessage()).Message)
			if kind != waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT {
				return nil
			}
			change.Type = waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_EDIT
		default:
			return nil
		}
	}
	if key.GetID() == "" || key.FromMe == nil {
		return nil
	}
	change.TargetProviderMessageId = key.GetID()
	change.TargetContactRef = key.GetRemoteJID()
	change.TargetSenderRef = key.GetParticipant()
	change.TargetFromMe = key.GetFromMe()
	change.Text = wire.SensitiveOutput(strings.TrimSpace(text), "message-change", sensitive)
	if timestamp > 0 {
		if at := timestamppb.New(time.UnixMilli(timestamp)); at.IsValid() {
			change.Timestamp = at
		}
	}
	return change
}
