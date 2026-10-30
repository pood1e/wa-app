package wamodel

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

// ApplyMessageChanges reduces authenticated events after pagination, including events received before their targets.
func ApplyMessageChanges(messages, events []*waappv1.AccountMessage) {
	events = slices.Clone(events)
	slices.SortFunc(events, func(a, b *waappv1.AccountMessage) int {
		if order := changeTime(a).Compare(changeTime(b)); order != 0 {
			return order
		}
		return cmp.Compare(a.GetAccountMessageId(), b.GetAccountMessageId())
	})
	for _, message := range messages {
		for _, event := range events {
			if changeTargetsMessage(message, event) {
				applyMessageChange(message, event)
			}
		}
	}
}

func changeTargetsMessage(target, event *waappv1.AccountMessage) bool {
	change := event.GetContent().GetChange()
	if change == nil || target.GetProviderMessageId() == "" || target.GetProviderMessageId() != change.GetTargetProviderMessageId() || target.GetWaAccountId() != event.GetWaAccountId() || messagePeer(target.GetContactRef()) != messagePeer(event.GetContactRef()) {
		return false
	}
	if strings.HasSuffix(target.GetContactRef(), "@g.us") && change.GetTargetContactRef() != "" && messagePeer(change.GetTargetContactRef()) != messagePeer(target.GetContactRef()) {
		return false
	}
	ownTarget := target.GetDirection() == waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND
	ownEvent := event.GetDirection() == waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND
	sameAuthor := ownTarget && ownEvent || !ownTarget && !ownEvent && messagePeer(target.GetSenderRef()) == messagePeer(event.GetSenderRef())
	if participant := change.GetTargetSenderRef(); participant != "" {
		if messagePeer(participant) != messagePeer(target.GetSenderRef()) {
			return false
		}
	} else if change.GetTargetFromMe() != sameAuthor {
		return false
	}
	return change.GetType() == waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_REACTION || sameAuthor
}

func applyMessageChange(target, event *waappv1.AccountMessage) {
	change := event.GetContent().GetChange()
	at := timestamppb.New(changeTime(event))
	switch change.GetType() {
	case waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_EDIT:
		if target.GetRevokedAt() == nil && target.GetContent().GetType() == waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_TEXT && change.GetText() != nil {
			target.Text = proto.Clone(change.GetText()).(*waappv1.SensitiveText)
			target.EditedAt = at
		}
	case waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_REVOKE:
		target.RevokedAt = at
		target.Text = &waappv1.SensitiveText{}
		target.Content = nil
		target.Reactions = nil
	case waappv1.MessageChangeType_MESSAGE_CHANGE_TYPE_REACTION:
		if target.GetRevokedAt() != nil {
			return
		}
		sender := event.GetSenderRef()
		if event.GetDirection() == waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND {
			sender = "self"
		}
		target.Reactions = slices.DeleteFunc(target.GetReactions(), func(reaction *waappv1.MessageReaction) bool {
			return messagePeer(reaction.GetSenderRef()) == messagePeer(sender)
		})
		if text := change.GetText().GetValue(); text != "" {
			target.Reactions = append(target.Reactions, &waappv1.MessageReaction{SenderRef: sender, Text: text, Timestamp: at})
		}
	}
}

func changeTime(event *waappv1.AccountMessage) time.Time {
	received := ProtoTimeOrZero(event.GetReceivedAt())
	if at := ProtoTimeOrZero(event.GetContent().GetChange().GetTimestamp()); !at.IsZero() && at.Before(received.Add(5*time.Minute)) {
		return at
	}
	return received
}

func messagePeer(value string) string {
	jid, err := types.ParseJID(value)
	if err != nil || jid.IsEmpty() {
		return value
	}
	return jid.ToNonAD().String()
}
