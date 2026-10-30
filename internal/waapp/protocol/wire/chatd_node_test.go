package wire

import (
	"bytes"
	"compress/zlib"
	"reflect"
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

func TestProfilePictureIQTargetUsesJIDWireEncoding(t *testing.T) {
	for _, target := range []string{"12025550123@s.whatsapp.net", "123456789@lid", "120363000000000000@g.us"} {
		t.Run(target, func(t *testing.T) {
			raw, err := NewBinaryNodeCodec().EncodeNode(ChatdNode{
				Tag: "iq", Attrs: map[string]string{"xmlns": "w:profile:picture", "type": "get", "to": "s.whatsapp.net", "target": target},
			})
			if err != nil {
				t.Fatal(err)
			}
			node, err := waBinary.Unmarshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			jid, ok := node.Attrs["target"].(types.JID)
			if !ok || jid.String() != target {
				t.Fatalf("IQ target must be encoded as a JID, got %T", node.Attrs["target"])
			}
		})
	}
}

func TestLeaveGroupEncodesOnlyGroupAddressAsJID(t *testing.T) {
	for _, id := range []string{"120363000000000000@g.us", "120363000000000000"} {
		raw, err := NewBinaryNodeCodec().EncodeNode(ChatdNode{Tag: "group", Attrs: map[string]string{"id": id}})
		if err != nil {
			t.Fatal(err)
		}
		node, err := waBinary.Unmarshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		jid, isJID := node.Attrs["id"].(types.JID)
		if id == "120363000000000000@g.us" && (!isJID || jid.String() != id) {
			t.Fatal("leave-group address lost its native JID encoding")
		}
		if id == "120363000000000000" && (isJID || node.Attrs["id"] != id) {
			t.Fatal("numeric group metadata ID was reinterpreted as a JID")
		}
	}
}

func TestPackedStanzaIDsPreserveResponseCorrelation(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		packed   []byte
	}{
		{"hex", "3EB0ABCDEF1234567890", []byte{251, 10, 0x3e, 0xb0, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90}},
		{"decimal", "12-3.4", []byte{255, 3, 0x12, 0xa3, 0xb4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// iq[id=<packed>, type=result], with the uncompressed frame flag.
			frame := append([]byte{0, 248, 5, 25, 8}, tc.packed...)
			frame = append(frame, 4, 20)
			node, err := NewBinaryNodeCodec().DecodeNodePayload(frame)
			if err != nil {
				t.Fatal(err)
			}
			if node.Tag != "iq" || node.Attrs["type"] != "result" || node.Attrs["id"] != tc.id {
				t.Fatalf("decoded stanza = %#v", node)
			}
		})
	}
}

func TestBinaryNodeCodecPreservesJIDsAndBinaryPayloads(t *testing.T) {
	node := ChatdNode{Tag: "message", Attrs: map[string]string{"id": "synthetic-message", "to": "12025550123:2@s.whatsapp.net", "participant": "123456:7@lid", "value": "test@example.com"}, Content: []ChatdNode{{Tag: "enc", Attrs: map[string]string{"type": "pkmsg", "v": "2"}, Content: []byte{0, 1, 2, 255}}}}
	codec := NewBinaryNodeCodec()
	raw, err := codec.EncodeNode(node)
	if err != nil {
		t.Fatal(err)
	}
	for _, compressed := range []bool{false, true} {
		frame := append([]byte{0}, raw...)
		if compressed {
			var body bytes.Buffer
			writer := zlib.NewWriter(&body)
			if _, err := writer.Write(raw); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			frame = append([]byte{2}, body.Bytes()...)
		}
		decoded, err := codec.DecodeNodePayload(frame)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded, node) {
			t.Fatal("JID domains, device IDs or encrypted bytes changed in binary framing")
		}
	}
}

func TestTextContentUsesRawUTF8Bytes(t *testing.T) {
	for _, value := range []string{"012345", "DEADBEEF", "code", "", "中文", "fixture@example.com"} {
		t.Run(value, func(t *testing.T) {
			raw, err := NewBinaryNodeCodec().EncodeNode(ChatdNode{
				Tag: "iq", Attrs: map[string]string{"id": "012345"},
				Content: []ChatdNode{{Tag: "2fa", Content: []ChatdNode{{Tag: "code", Content: value}}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			node, err := waBinary.Unmarshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			twoFactor := node.GetChildByTag("2fa")
			content := twoFactor.GetChildByTag("code").Content
			data, ok := content.([]byte)
			if !ok || !bytes.Equal(data, []byte(value)) {
				t.Fatalf("native text body must decode as raw bytes, got %T", content)
			}
			if node.Attrs["id"] != "012345" {
				t.Fatal("text body conversion changed attribute encoding")
			}
		})
	}
}
