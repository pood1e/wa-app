package wire

import "testing"

func TestMessageAndReceiptAcknowledgementAttributes(t *testing.T) {
	for _, tag := range []string{"message", "receipt", "notification"} {
		node := ChatdNode{Tag: tag, Attrs: map[string]string{"from": "12345@g.us", "id": "fixture", "type": "retry", "participant": "12025550124:2@s.whatsapp.net", "recipient": "12025550123@s.whatsapp.net"}}
		ack, ok := BuildAckForNode(node)
		if !ok || ack.Tag != "ack" || ack.Attrs["class"] != tag || ack.Attrs["id"] != "fixture" || ack.Attrs["participant"] != node.Attrs["participant"] || ack.Attrs["recipient"] != node.Attrs["recipient"] {
			t.Fatalf("%s acknowledgement lost routing fields", tag)
		}
		if tag == "message" && ack.Attrs["type"] != "" || tag != "message" && ack.Attrs["type"] != "retry" {
			t.Fatalf("%s acknowledgement type=%q", tag, ack.Attrs["type"])
		}
	}
	if _, ok := BuildAckForNode(ChatdNode{Tag: "ack", Attrs: map[string]string{"from": "12345@g.us", "id": "fixture"}}); ok {
		t.Fatal("acknowledgements must not acknowledge themselves")
	}
}
