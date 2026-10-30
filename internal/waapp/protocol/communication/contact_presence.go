package communication

import (
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func (s *ChatdSession) contactPresence(node wire.ChatdNode, now time.Time) *waappv1.WAContactPresence {
	if node.Tag == "presence" {
		return presenceFromNode(wire.SDKChatdNode(node), now)
	}
	if node.Tag != "chatstate" {
		return nil
	}
	source, ok := s.messageSource(node)
	children := wire.ChatdChildren(node)
	if !ok || source.fromMe || source.sender.IsEmpty() || len(children) != 1 {
		return nil
	}
	var activity waappv1.WAChatActivityType
	switch children[0].Tag {
	case "composing":
		activity = waappv1.WAChatActivityType_WA_CHAT_ACTIVITY_TYPE_TYPING
		if children[0].Attrs["media"] == "audio" {
			activity = waappv1.WAChatActivityType_WA_CHAT_ACTIVITY_TYPE_RECORDING
		}
	case "paused":
		activity = waappv1.WAChatActivityType_WA_CHAT_ACTIVITY_TYPE_PAUSED
	default:
		return nil
	}
	return &waappv1.WAContactPresence{ContactRef: source.chat.String(), Activities: []*waappv1.WAChatActivity{{ParticipantRef: source.sender.ToNonAD().String(), Type: activity, UpdatedAt: timestamppb.New(now)}}}
}

func presenceFromNode(node waBinary.Node, now time.Time) *waappv1.WAContactPresence {
	attrs := node.AttrGetter()
	from := attrs.JID("from")
	if !attrs.OK() || (from.Server != types.DefaultUserServer && from.Server != types.HiddenUserServer) {
		return nil
	}
	presence := &waappv1.WAContactPresence{ContactRef: from.ToNonAD().String(), ObservedAt: timestamppb.New(now)}
	switch attrs.OptionalString("type") {
	case "", "available":
		presence.State = waappv1.WAContactPresenceState_WA_CONTACT_PRESENCE_STATE_ONLINE
	case "unavailable":
		presence.State = waappv1.WAContactPresenceState_WA_CONTACT_PRESENCE_STATE_OFFLINE
		if last := attrs.OptionalString("last"); last != "" && last != "deny" {
			if stamp := ChatdProviderTimestamp(last); stamp != nil && stamp.CheckValid() == nil {
				presence.LastSeenAt = stamp
			}
		}
	default:
		return nil
	}
	return presence
}
