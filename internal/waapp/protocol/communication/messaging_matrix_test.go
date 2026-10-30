package communication

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"net"
	"testing"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestFourAccountDirectAndGroupMatrixAcrossRecovery(t *testing.T) {
	const group = "123456789@g.us"
	var parties [4]state.NativeState
	var refs [4]string
	var roster []wire.ChatdNode
	for i := range parties {
		parties[i] = newMessageParty(t)
		if i == 0 {
			parties[i].Client.Platform = protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS
		}
		parties[i].RegistrationJID = fmt.Sprintf("1202555012%d@s.whatsapp.net", i)
		refs[i] = fmt.Sprintf("10000000%d@lid", i)
		roster = append(roster, wire.ChatdNode{Tag: "participant", Attrs: map[string]string{"jid": refs[i], "phone_number": parties[i].RegistrationJID}})
	}
	for i := range parties {
		for j := i + 1; j < len(parties); j++ {
			bundle, err := preKeyBundleFromNode(messagePartyBundle(t, parties[j], true))
			if err != nil {
				t.Fatal(err)
			}
			if err := initializeOutboundSession(&parties[i], refs[j], bundle); err != nil {
				t.Fatal(err)
			}
			for _, pair := range [][2]int{{i, j}, {j, i}} {
				sender, recipient := pair[0], pair[1]
				text := fmt.Sprintf("direct %d→%d 中文🙂\nsecond line", sender, recipient)
				raw, kind, err := encryptDirectSignalPayload(&parties[sender], refs[recipient], NativeTextMessagePlaintext(text))
				if err != nil {
					t.Fatal(err)
				}
				output, err := DecryptNativeSignalPayloadByType(&parties[recipient], raw, kind, refs[sender], true)
				if err != nil || NativePlaintextText(output.Plaintext) != text {
					t.Fatalf("direct %d→%d failed: %v", sender, recipient, err)
				}
				parties[sender] = reloadMessageParty(t, parties[sender])
				parties[recipient] = reloadMessageParty(t, parties[recipient])
			}
		}
	}
	for sender := range parties {
		text := fmt.Sprintf("group from %d 中文🙂\nsecond line", sender)
		metadata := wire.ChatdNode{Tag: "group", Attrs: map[string]string{"id": group, "subject": "Synthetic interoperability"}, Content: roster}
		node := sendSyntheticGroupMessage(t, &parties[sender], metadata, &waE2E.Message{Conversation: proto.String(text)})
		enc, ok := ChatdChild(node, "enc")
		if !ok || enc.Attrs["type"] != "skmsg" || node.Attrs["phash"] != participantDeviceHash(refs[:]) {
			t.Fatal("group ciphertext or participant hash is missing")
		}
		participants, _ := ChatdChild(node, "participants")
		if len(wire.ChatdChildren(participants)) != 3 {
			t.Fatal("group fanout does not cover exactly the other three accounts")
		}
		for recipient := range parties {
			if recipient == sender {
				continue
			}
			for _, peer := range wire.ChatdChildren(participants) {
				if peer.Attrs["jid"] != refs[recipient] {
					continue
				}
				payload, _ := ChatdChild(peer, "enc")
				output, err := DecryptNativeSignalPayloadByType(&parties[recipient], payload.Content.([]byte), payload.Attrs["type"], refs[sender], true)
				if err != nil {
					t.Fatal(err)
				}
				if err := applySignalControlMessage(t.Context(), &parties[recipient], refs[sender], output.Plaintext); err != nil {
					t.Fatal(err)
				}
			}
			parties[recipient] = reloadMessageParty(t, parties[recipient])
			output, err := decryptGroupMessage(&parties[recipient], group, refs[sender], enc.Content.([]byte), true)
			if err != nil || NativePlaintextText(output.Plaintext) != text {
				t.Fatalf("group %d→%d failed after recovery: %v", sender, recipient, err)
			}
		}
		parties[sender] = reloadMessageParty(t, parties[sender])
	}
}

func sendSyntheticGroupMessage(t *testing.T, current *state.NativeState, metadata wire.ChatdNode, body *waE2E.Message) wire.ChatdNode {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	session, peer := syntheticChatdPair(t)
	finished := make(chan wire.ChatdNode, 1)
	failed := make(chan error, 1)
	go func() {
		for range 3 {
			node, err := peer.ReadNode()
			if err != nil {
				failed <- err
				return
			}
			response := wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": node.Attrs["id"], "type": "result"}}
			switch node.Attrs["xmlns"] {
			case "w:g2":
				response.Content = []wire.ChatdNode{metadata}
			case "usync":
				var users []wire.ChatdNode
				for _, member := range wire.ChatdChildren(metadata) {
					users = append(users, wire.ChatdNode{Tag: "user", Attrs: map[string]string{"jid": member.Attrs["jid"]}, Content: []wire.ChatdNode{{
						Tag: "devices", Content: []wire.ChatdNode{{Tag: "device-list", Content: []wire.ChatdNode{{Tag: "device", Attrs: map[string]string{"id": "0"}}}}},
					}}})
				}
				response.Content = []wire.ChatdNode{{Tag: "usync", Content: []wire.ChatdNode{{Tag: "list", Content: users}}}}
			default:
				if node.Tag != "message" {
					failed <- fmt.Errorf("unexpected request %s", node.Tag)
					return
				}
				finished <- node
				response = wire.ChatdNode{Tag: "ack", Attrs: map[string]string{"id": node.Attrs["id"], "class": "message"}}
			}
			if err := peer.SendNode(response); err != nil {
				failed <- err
				return
			}
		}
	}()
	repository := &serializedMessageState{}
	engine, err := New(repository, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.CloseIdleConnections()
	result := engine.SendMessageOnSession(ctx, session, current, wacore.EngineMessageSendInput{ContactJID: metadata.Attrs["id"], Message: body},
		wacore.EngineMessageInput{WAAccountID: "synthetic", MessageSessionID: "synthetic-session"}, time.Second)
	if result.Err != nil || result.AckStatus != waappv1.MessageAckStatus_MESSAGE_ACK_STATUS_ACKED {
		t.Fatalf("group send failed: %v", result.Err)
	}
	_, recovered, err := repository.GetProtocolState(ctx, "")
	if err != nil || len(recovered.GetSenderKeyRecords()) == 0 {
		t.Fatal("outgoing sender-key state was not persisted")
	}
	select {
	case node := <-finished:
		return node
	case err := <-failed:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return wire.ChatdNode{}
}

func syntheticChatdPair(t *testing.T) (*ChatdSession, *ChatdTransport) {
	t.Helper()
	session, peer, _ := syntheticChatdPipe(t)
	return session, peer
}

func syntheticChatdPipe(t *testing.T) (*ChatdSession, *ChatdTransport, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	keyA, keyB := bytes.Repeat([]byte{7}, 32), bytes.Repeat([]byte{8}, 32)
	transport := func(conn net.Conn, send, receive []byte) ChatdTransport {
		return ChatdTransport{Rw: bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)), Codec: wire.NewBinaryNodeCodec(),
			Keys: &wire.ChatdTransportKeys{SendKey: send, RecvKey: receive}, MaxFrameBytes: DefaultChatdMaxFrame}
	}
	peer := transport(server, keyB, keyA)
	return &ChatdSession{Conn: client, Transport: transport(client, keyA, keyB)}, &peer, server
}
