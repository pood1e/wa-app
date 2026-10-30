package communication

import (
	"go.mau.fi/whatsmeow/types"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

type messageSource struct {
	chat, sender, senderAlt types.JID
	fromMe                  bool
}

func (s *ChatdSession) messageSource(node wire.ChatdNode) (messageSource, bool) {
	from, err := types.ParseJID(node.Attrs["from"])
	if err != nil || from.User == "" {
		return messageSource{}, false
	}
	source := messageSource{chat: from.ToNonAD(), sender: from}
	pn, lid := "sender_pn", "sender_lid"
	switch from.Server {
	case types.GroupServer, types.BroadcastServer:
		source.sender = userJID(node.Attrs["participant"])
		pn, lid = "participant_pn", "participant_lid"
	case types.DefaultUserServer, types.HiddenUserServer:
		if recipient := userJID(node.Attrs["recipient"]); !recipient.IsEmpty() {
			source.chat = recipient.ToNonAD()
		}
	default:
		return messageSource{}, false
	}
	if source.sender.Server == types.HiddenUserServer {
		source.senderAlt = userJID(node.Attrs[pn])
	} else {
		source.senderAlt = userJID(node.Attrs[lid])
	}
	if !source.senderAlt.IsEmpty() {
		source.senderAlt.Device = source.sender.Device
	}
	source.fromMe = s.ownJID(source.sender) || s.ownJID(source.senderAlt)
	return source, true
}

func userJID(value string) types.JID {
	jid, err := types.ParseJID(value)
	if err != nil || jid.User == "" || jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer {
		return types.EmptyJID
	}
	return jid
}

func (s *ChatdSession) ownJID(jid types.JID) bool {
	if jid.IsEmpty() {
		return false
	}
	for _, ref := range []string{s.PhoneJID, s.IdentityJID} {
		if own := userJID(ref); !own.IsEmpty() && own.ToNonAD() == jid.ToNonAD() {
			return true
		}
	}
	return false
}
