package communication

import (
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestIncomingMessageKeepsSenderDeviceAndOwnMessageDirection(t *testing.T) {
	for _, test := range []struct {
		name, from, alt, recipient, chat, sender string
		outgoing                                 bool
	}{
		{"peer_device", "12025550124:3@s.whatsapp.net", "987654321@lid", "", "12025550124@s.whatsapp.net", "987654321:3@lid", false},
		{"own_companion", "12025550123:2@s.whatsapp.net", "", "987654321@lid", "987654321@lid", "12025550123:2@s.whatsapp.net", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock := &receiveTestClock{now: time.Now()}
			node := wire.ChatdNode{Tag: "message", Attrs: map[string]string{"from": test.from, "id": "fixture", "sender_lid": test.alt, "recipient": test.recipient}, Content: []wire.ChatdNode{{Tag: "enc", Attrs: map[string]string{"type": "msg", "v": "2"}, Content: []byte("synthetic-encrypted-message")}}}
			session := receiveTestSession(t, node, clock)
			_, items, err := session.ConsumeIncomingNode(wacore.EngineMessageInput{WAAccountID: "synthetic", MessageSessionID: "session"}, node, ChatdSessionUpdate{}, clock.Now())
			if err != nil || len(items) != 1 || items[0].Payload.Sender != test.sender || items[0].Payload.Contact != test.chat || items[0].Message.GetContactRef() != test.chat || (items[0].Message.GetDirection() == waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND) != test.outgoing {
				t.Fatal("message source lost device, chat or direction", err)
			}
		})
	}
}

func TestNativeMessageSourceSeparatesGroupChatFromParticipant(t *testing.T) {
	session := &ChatdSession{PhoneJID: "12025550123@s.whatsapp.net", IdentityJID: "100001@lid"}
	node := wire.ChatdNode{Tag: "message", Attrs: map[string]string{"from": "120363000001@g.us", "participant": "12025550124:3@s.whatsapp.net", "participant_lid": "100002@lid", "recipient": session.PhoneJID}}
	source, ok := session.messageSource(node)
	if !ok || source.chat.String() != "120363000001@g.us" || source.sender.String() != "12025550124:3@s.whatsapp.net" || source.senderAlt.String() != "100002:3@lid" || source.fromMe {
		t.Fatal("native participant alias replaced the group or lost its sending device")
	}
	node.Attrs["participant"] = "100001:2@lid"
	node.Attrs["participant_pn"] = session.PhoneJID
	source, ok = session.messageSource(node)
	if !ok || !source.fromMe {
		t.Fatal("own group message was attributed to another member")
	}
}

func TestNativeMessageSourceRejectsUnknownRoutingDomains(t *testing.T) {
	session := &ChatdSession{}
	for _, from := range []string{"", "s.whatsapp.net", "100001@unknown.test"} {
		if _, ok := session.messageSource(wire.ChatdNode{Attrs: map[string]string{"from": from}}); ok {
			t.Fatal("missing or unsupported sender was accepted as a conversation")
		}
	}
}
