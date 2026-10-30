package communication

import (
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestReceiptEvents(t *testing.T) {
	session := &ChatdSession{PhoneJID: "12025550123@s.whatsapp.net"}
	for _, test := range []struct {
		name string
		node wire.ChatdNode
		want int
	}{
		{"delivery", wire.ChatdNode{Tag: "receipt", Attrs: map[string]string{"from": "12025550124:2@s.whatsapp.net", "id": "fixture", "t": "1700000000"}}, 1},
		{"batched_read", wire.ChatdNode{Tag: "receipt", Attrs: map[string]string{"from": "12025550124@s.whatsapp.net", "id": "first", "type": "read", "t": "1700000000"}, Content: []wire.ChatdNode{{Tag: "list", Content: []wire.ChatdNode{{Tag: "item", Attrs: map[string]string{"id": "second"}}}}}}, 2},
		{"own_device", wire.ChatdNode{Tag: "receipt", Attrs: map[string]string{"from": session.PhoneJID, "recipient": "12025550124@s.whatsapp.net", "id": "fixture", "type": "read", "t": "1700000000"}}, 0},
		{"retry_is_not_delivery", wire.ChatdNode{Tag: "receipt", Attrs: map[string]string{"from": "12025550124@s.whatsapp.net", "id": "fixture", "type": "retry", "t": "1700000000"}}, 0},
		{"private_read", wire.ChatdNode{Tag: "receipt", Attrs: map[string]string{"from": "12025550124@s.whatsapp.net", "id": "fixture", "type": "read-self", "t": "1700000000"}}, 0},
		{"missing_time", wire.ChatdNode{Tag: "receipt", Attrs: map[string]string{"from": "12025550124@s.whatsapp.net", "id": "fixture"}}, 0},
		{"group_member", wire.ChatdNode{Tag: "receipt", Attrs: map[string]string{"from": "120363000001@g.us", "participant": "12025550124:2@s.whatsapp.net", "id": "fixture", "type": "read", "t": "1700000000"}}, 1},
		{"grouped", wire.ChatdNode{Tag: "receipt", Attrs: map[string]string{"from": "120363000001@g.us", "type": "read"}, Content: []wire.ChatdNode{{Tag: "participants", Attrs: map[string]string{"message_id": "fixture"}, Content: []wire.ChatdNode{
			{Tag: "user", Attrs: map[string]string{"jid": "12025550124@s.whatsapp.net", "t": "1700000000"}},
			{Tag: "user", Attrs: map[string]string{"jid": "12025550125@s.whatsapp.net", "t": "1700000001"}},
			{Tag: "user", Attrs: map[string]string{"jid": session.PhoneJID, "t": "1700000002"}},
		}}}}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			receipts := session.messageReceipts(test.node)
			if len(receipts) != test.want {
				t.Fatalf("receipt count=%d want=%d", len(receipts), test.want)
			}
			for _, receipt := range receipts {
				if receipt.GetRecipientRef() == "12025550124:2@s.whatsapp.net" || receipt.GetTimestamp() == nil || receipt.GetContactRef() == "" {
					t.Fatal("receipt projection lost its canonical recipient, chat or timestamp")
				}
			}
		})
	}
}

func TestPresenceEventsAndPendingControlUpdates(t *testing.T) {
	now := time.Unix(1700000000, 0)
	session := &ChatdSession{PhoneJID: "12025550123@s.whatsapp.net"}
	offline := session.contactPresence(wire.ChatdNode{Tag: "presence", Attrs: map[string]string{"from": "12025550124@s.whatsapp.net", "type": "unavailable", "last": "deny"}}, now)
	if offline.GetState() != waappv1.WAContactPresenceState_WA_CONTACT_PRESENCE_STATE_OFFLINE || offline.GetLastSeenAt() != nil {
		t.Fatal("private last seen was fabricated")
	}
	typing := session.contactPresence(wire.ChatdNode{Tag: "chatstate", Attrs: map[string]string{"from": "120363000001@g.us", "participant": "12025550124@s.whatsapp.net"}, Content: []wire.ChatdNode{{Tag: "composing", Attrs: map[string]string{"media": "audio"}}}}, now)
	if len(typing.GetActivities()) != 1 || typing.GetActivities()[0].GetType() != waappv1.WAChatActivityType_WA_CHAT_ACTIVITY_TYPE_RECORDING {
		t.Fatal("group recording activity was dropped")
	}
	connection := &Connection{}
	connection.BufferPendingLocked(nil, ChatdSessionUpdate{Presences: []*waappv1.WAContactPresence{offline, typing}})
	connection.BufferPendingLocked(nil, ChatdSessionUpdate{Receipts: []*waappv1.MessageReceipt{{ProviderMessageId: "fixture"}}})
	_, _, _, update, drained := connection.DrainPendingLocked(wacore.EngineMessageInput{MaxMessages: 1})
	if !drained || len(update.Presences) != 2 || len(update.Receipts) != 1 || HasChatdSessionUpdate(connection.PendingUp) {
		t.Fatal("interactive IQ buffering dropped or retained consumed control updates")
	}
}
