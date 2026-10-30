package wamodel

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func TestMessageChangesReduceReorderingAndReactionRemoval(t *testing.T) {
	target := changeTarget()
	edit := changeEvent("edit", waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_EDIT, "edited", 2)
	reaction := changeEvent("reaction", waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_REACTION, "👍", 3)
	reaction.SenderRef = "12025550125:2@s.whatsapp.net"
	reaction.Content.Change.TargetFromMe = false
	reaction.Content.Change.TargetSenderRef = target.GetSenderRef()
	remove := proto.Clone(reaction).(*waappv1.AccountMessage)
	remove.AccountMessageId = "remove"
	remove.Content.Change.Text.Value = ""
	remove.Content.Change.Timestamp = timestamppb.New(time.Unix(1700000004, 0))
	ApplyMessageChanges([]*waappv1.AccountMessage{target}, []*waappv1.AccountMessage{remove, edit, reaction, edit, reaction})
	if target.GetText().GetValue() != "edited" || target.GetEditedAt() == nil || len(target.GetReactions()) != 0 || target.GetReadAt() == nil {
		t.Fatal("reordered/duplicate changes lost edit, removal or local read state")
	}
	revoke := changeEvent("revoke", waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_REVOKE, "", 5)
	lateEdit := changeEvent("late-edit", waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_EDIT, "resurrect", 6)
	ApplyMessageChanges([]*waappv1.AccountMessage{target}, []*waappv1.AccountMessage{lateEdit, revoke, reaction})
	if target.GetRevokedAt() == nil || target.GetText().GetValue() != "" || target.GetContent() != nil || len(target.GetReactions()) > 0 {
		t.Fatal("a later event resurrected revoked content")
	}
}

func TestMessageChangesRejectForeignTargetsAndAuthors(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*waappv1.AccountMessage)
	}{
		{"another_account", func(event *waappv1.AccountMessage) { event.WaAccountId = "another" }},
		{"another_chat", func(event *waappv1.AccountMessage) { event.ContactRef = "another@g.us" }},
		{"foreign_key_chat", func(event *waappv1.AccountMessage) { event.Content.Change.TargetContactRef = "another@g.us" }},
		{"foreign_author", func(event *waappv1.AccountMessage) { event.SenderRef = "12025550125@s.whatsapp.net" }},
		{"foreign_participant", func(event *waappv1.AccountMessage) {
			event.Content.Change.TargetSenderRef = "12025550125@s.whatsapp.net"
		}},
		{"wrong_ownership", func(event *waappv1.AccountMessage) { event.Content.Change.TargetFromMe = false }},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := changeTarget()
			event := changeEvent("revoke", waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_REVOKE, "", 2)
			test.mutate(event)
			ApplyMessageChanges([]*waappv1.AccountMessage{target}, []*waappv1.AccountMessage{event})
			if target.GetRevokedAt() != nil {
				t.Fatal("foreign revoke was applied")
			}
		})
	}
}

func TestMessageContentOutputRedactsNestedTextWithoutMutatingStorage(t *testing.T) {
	content := &waappv1.MessageContent{Quote: &waappv1.MessageQuote{Text: &waappv1.SensitiveText{Value: "quote secret"}}, Change: &waappv1.MessageChange{Text: &waappv1.SensitiveText{Value: "edit secret"}}}
	redacted := MessageContentOutput(content, false)
	if redacted.GetQuote().GetText().GetValue() != "" || redacted.GetChange().GetText().GetValue() != "" || content.GetQuote().GetText().GetValue() == "" {
		t.Fatal("nested output redaction failed")
	}
}

func changeTarget() *waappv1.AccountMessage {
	return &waappv1.AccountMessage{WaAccountId: "account", AccountMessageId: "target", ProviderMessageId: "provider", ContactRef: "fixture@g.us", SenderRef: "12025550124@s.whatsapp.net", Direction: waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND, Text: &waappv1.SensitiveText{Value: "original"}, Content: &waappv1.MessageContent{Type: waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT}, ReadAt: timestamppb.New(time.Unix(1700000000, 0))}
}

func changeEvent(id string, kind waappv1.MessageChangeType, text string, second int64) *waappv1.AccountMessage {
	return &waappv1.AccountMessage{WaAccountId: "account", AccountMessageId: id, ContactRef: "fixture@g.us", SenderRef: "12025550124:3@s.whatsapp.net", Direction: waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND, ReceivedAt: timestamppb.New(time.Unix(1700000100, 0)), Content: &waappv1.MessageContent{Type: waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL, Change: &waappv1.MessageChange{Type: kind, TargetProviderMessageId: "provider", TargetFromMe: true, Text: &waappv1.SensitiveText{Value: text}, Timestamp: timestamppb.New(time.Unix(1700000000+second, 0))}}}
}
