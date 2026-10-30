package communication

import (
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestNativeAggregateReceiptUsesMessageIDAndUserProperties(t *testing.T) {
	session := &ChatdSession{PhoneJID: "12025550123@s.whatsapp.net", IdentityJID: "100001@lid"}
	node := wire.ChatdNode{Tag: "receipt", Attrs: map[string]string{"from": "120363000001@g.us", "type": "read", "t": "1700000000"}, Content: []wire.ChatdNode{{Tag: "participants", Attrs: map[string]string{"message_id": "native-message"}, Content: []wire.ChatdNode{
		{Tag: "user", Attrs: map[string]string{"jid": "100002:3@lid", "type": "played", "t": "1700000010"}},
		{Tag: "user", Attrs: map[string]string{"jid": "100003@lid"}},
		{Tag: "user", Attrs: map[string]string{"jid": "100004@lid", "type": "read-self"}},
		{Tag: "user", Attrs: map[string]string{"jid": session.IdentityJID}},
		{Tag: "user", Attrs: map[string]string{"jid": "100005@lid", "t": "invalid"}},
	}}}}
	receipts := session.messageReceipts(node)
	if len(receipts) != 2 || receipts[0].GetType() != waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_PLAYED || receipts[0].GetTimestamp().GetSeconds() != 1700000010 || receipts[1].GetType() != waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_READ || receipts[1].GetTimestamp().GetSeconds() != 1700000000 {
		t.Fatal("native aggregate receipt lost per-user type, timestamp or privacy filtering")
	}
	for _, receipt := range receipts {
		if receipt.GetProviderMessageId() != "native-message" || receipt.GetContactRef() != "120363000001@g.us" {
			t.Fatal("aggregate receipt lost its message or group identity")
		}
	}
}

func TestNativeAggregateV1AndListRetainOriginalStanzaIDs(t *testing.T) {
	session := &ChatdSession{}
	node := wire.ChatdNode{Tag: "receipt", Attrs: map[string]string{"from": "120363000001@g.us", "id": "first", "type": "read", "t": "1700000000"}, Content: []wire.ChatdNode{
		{Tag: "list", Content: []wire.ChatdNode{{Tag: "item", Attrs: map[string]string{"id": "second"}}, {Tag: "item", Attrs: map[string]string{"id": "first"}}}},
		{Tag: "participants", Content: []wire.ChatdNode{{Tag: "user", Attrs: map[string]string{"jid": "100002@lid"}}}},
	}}
	receipts := session.messageReceipts(node)
	if len(receipts) != 2 || receipts[0].GetProviderMessageId() != "first" || receipts[1].GetProviderMessageId() != "second" {
		t.Fatal("native V1 aggregate discarded or duplicated original stanza IDs")
	}
	delete(node.Attrs, "id")
	node.Content = wire.ChatdChildren(node)[1:]
	if len(session.messageReceipts(node)) != 0 {
		t.Fatal("aggregate without a confirmed message ID was projected as delivery")
	}
}
