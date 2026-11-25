package communication

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"io"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func bootstrapTestState(t *testing.T) state.NativeState {
	t.Helper()
	current := newMessageParty(t)
	current.RegistrationJID = "12025550123@s.whatsapp.net"
	current.PushName = "Fixture"
	private, err := current.Signal.Identity.PrivateBytes()
	if err != nil {
		t.Fatal(err)
	}
	indexes, err := wire.SignDeviceKeyIndexList(private, &waAdv.ADVKeyIndexList{RawID: proto.Uint32(12), CurrentIndex: proto.Uint32(2), ValidIndexes: []uint32{0, 2}, Timestamp: proto.Uint64(1800000000)})
	if err != nil {
		t.Fatal(err)
	}
	current.LinkedDevices = &protocolv1.LinkedDeviceState{SignedKeyIndexList: indexes, Devices: []*protocolv1.LinkedDeviceRecord{{DeviceRef: linkedTestDevice, KeyIndex: 2, LinkedAt: 1800000000}}}
	return current
}

func TestCompanionBootstrapKeysAndMetadataSurviveRecovery(t *testing.T) {
	current := bootstrapTestState(t)
	current.ContactHints = []wacore.WAContactHint{{PNJID: "12025550999@s.whatsapp.net", LIDJID: "123456789@lid", WAName: "Contact"}}
	now := time.Unix(1800000000, 0)
	if err := prepareLinkedDeviceKeys(&current, now); err != nil {
		t.Fatal(err)
	}
	share, err := linkedDeviceKeyShare(&current)
	if err != nil || len(share.GetProtocolMessage().GetAppStateSyncKeyShare().GetKeys()) != 1 {
		t.Fatal("bootstrap did not create a recoverable native key share", err)
	}
	before, _, _ := current.Export()
	reloaded := reloadMessageParty(t, current)
	if err := prepareLinkedDeviceKeys(&reloaded, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	nextShare, _ := linkedDeviceKeyShare(&reloaded)
	after, _, _ := reloaded.Export()
	if !proto.Equal(before, after) || !proto.Equal(share, nextShare) || !bytes.Equal(current.LinkedDevices.GetHistoryThreadSecret(), reloaded.LinkedDevices.GetHistoryThreadSecret()) {
		t.Fatal("retry rotated primary credentials or already persisted bootstrap material")
	}
	message, err := linkedDeviceInitialMetadata(&reloaded)
	if err != nil {
		t.Fatal(err)
	}
	notification := message.GetProtocolMessage().GetHistorySyncNotification()
	hash := sha256.Sum256(notification.GetInitialHistBootstrapInlinePayload())
	if !bytes.Equal(hash[:], notification.GetFileSHA256()) {
		t.Fatal("inline history notification does not bind its exact payload")
	}
	reader, err := zlib.NewReader(bytes.NewReader(notification.GetInitialHistBootstrapInlinePayload()))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, 1<<20))
	history := new(waHistorySync.HistorySync)
	if err != nil || proto.Unmarshal(raw, history) != nil || history.GetSyncType() != waHistorySync.HistorySync_INITIAL_BOOTSTRAP || len(history.GetConversations()) != 0 || len(history.GetPhoneNumberToLidMappings()) != 1 || len(history.GetPushnames()) != 1 || len(history.GetThreadIDUserSecret()) != 32 {
		t.Fatal("initial metadata discarded known contacts or fabricated conversation history")
	}
}

func TestCompanionBootstrapUsesOwnedEncryptedOutboxAndResumes(t *testing.T) {
	current, companion := bootstrapTestState(t), newMessageParty(t)
	current.PushName = ""
	connection, peer, _ := groupTestConnection(t, current)
	device := current.LinkedDevices.GetDevices()[0]
	done := make(chan error, 1)
	go func() { done <- connection.flushLinkedDeviceBootstrap(t.Context(), &current) }()
	request := deviceTestIQ(t, peer, "encrypt")
	bundle := messagePartyBundle(t, companion, true)
	bundle.Attrs = map[string]string{"jid": linkedTestDevice}
	deviceTestResponse(t, peer, request, wire.ChatdNode{Tag: "list", Content: []wire.ChatdNode{bundle}})
	readControl := func(want waE2E.ProtocolMessage_Type) {
		t.Helper()
		node, err := peer.ReadNode()
		if err != nil || node.Tag != "message" || node.Attrs["category"] != "peer" || node.Attrs["to"] != linkedTestDevice {
			t.Fatal("bootstrap bypassed the owned peer message path", err)
		}
		stored, err := connection.loadState(t.Context(), "current")
		record := findSentMessage(&stored, linkedTestDevice, node.Attrs["id"])
		if err != nil || record == nil || record.GetAcknowledged() || len(record.GetRecipientDevices()) != 1 {
			t.Fatal("bootstrap emitted ciphertext before checkpointing its ratchet and retry body", err)
		}
		enc, _ := ChatdChild(node, "enc")
		output, err := DecryptNativeSignalPayloadByType(&companion, enc.Content.([]byte), enc.Attrs["type"], current.RegistrationJID, true)
		if err != nil {
			t.Fatal("companion could not decrypt its bootstrap control message", err)
		}
		message, err := DecodeSignalMessage(output.Plaintext)
		if err != nil || message.GetProtocolMessage().GetType() != want {
			t.Fatal("companion could not authenticate its bootstrap control message", err)
		}
		if err := peer.SendNode(wire.ChatdNode{Tag: "ack", Attrs: map[string]string{"id": node.Attrs["id"], "class": "message"}}); err != nil {
			t.Fatal(err)
		}
	}
	readControl(waE2E.ProtocolMessage_APP_STATE_SYNC_KEY_SHARE)
	request = deviceTestIQ(t, peer, "w:sync:app:state")
	deviceTestResponse(t, peer, request, wire.ChatdNode{Tag: "sync", Content: []wire.ChatdNode{{Tag: "collection", Content: []wire.ChatdNode{{Tag: "error", Attrs: map[string]string{"code": "409"}}}}}})
	if err := <-done; err != nil || device.GetBootstrapSent() || device.GetBootstrapRetryAt() == 0 {
		t.Fatal("a rejected conditional update discarded its retry checkpoint", err)
	}
	device.BootstrapRetryAt = 0
	go func() { done <- connection.flushLinkedDeviceBootstrap(t.Context(), &current) }()
	request = deviceTestIQ(t, peer, "w:sync:app:state")
	deviceTestResponse(t, peer, request)
	readControl(waE2E.ProtocolMessage_HISTORY_SYNC_NOTIFICATION)
	if err := <-done; err != nil || !device.GetBootstrapSent() || device.GetBootstrapRetryAt() != 0 {
		t.Fatal("acknowledged initialization was not marked complete", err)
	}
	restored, err := connection.loadState(t.Context(), "current")
	if err != nil || !restored.LinkedDevices.GetDevices()[0].GetBootstrapSent() || len(restored.AppState.Keys) != 1 {
		t.Fatal("initialization did not survive process recovery", err)
	}
	if err := connection.flushLinkedDeviceBootstrap(t.Context(), &restored); err != nil {
		t.Fatal("completed initialization was replayed", err)
	}
}

func TestOwnCompanionRetryRetainsItsPeerCategoryAndAuthorization(t *testing.T) {
	current, companion := bootstrapTestState(t), newMessageParty(t)
	if err := prepareLinkedDeviceKeys(&current, time.Now()); err != nil {
		t.Fatal(err)
	}
	message, err := linkedDeviceKeyShare(&current)
	if err != nil {
		t.Fatal(err)
	}
	const id = "stable-bootstrap-retry"
	_, err = rememberSentMessage(&current, message, wire.ChatdNode{Tag: "message", Attrs: map[string]string{"category": "peer", "to": linkedTestDevice, "id": id}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := buildSignalRetryReceipt(&companion, stateMessagePayload(current.RegistrationJID, current.RegistrationJID, id), 2)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Attrs["from"] = linkedTestDevice
	delete(receipt.Attrs, "to")
	session := &ChatdSession{PhoneJID: current.RegistrationJID}
	request, valid := session.parseSignalRetry(receipt)
	if !valid {
		t.Fatal("an own companion's retry was rejected as a primary echo")
	}
	preparation := messagePreparation{session: session, current: &current}
	response, err := preparation.retryMessage(t.Context(), receipt, request)
	if err != nil || response.Attrs["category"] != "peer" || response.Attrs["id"] != id {
		t.Fatal("retry lost its control routing or durable body", err)
	}
	enc, _ := ChatdChild(response, "enc")
	plain, err := DecryptNativeSignalPayloadByType(&companion, enc.Content.([]byte), enc.Attrs["type"], current.RegistrationJID, true)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSignalMessage(plain.Plaintext)
	if err != nil || !proto.Equal(decoded, message) {
		t.Fatal("retry changed the authorized key share")
	}
	request.sender.User = "12025550999"
	if _, err := preparation.retryMessage(t.Context(), receipt, request); err == nil {
		t.Fatal("an unrelated account obtained companion initialization material")
	}
	current.SentMessages["other-chat"] = &protocolv1.SentMessage{ContactRef: "12025550999:2@s.whatsapp.net"}
	removeLinkedDeviceState(&current, 2)
	if len(current.SentMessages) != 1 || current.SentMessages["other-chat"] == nil {
		t.Fatal("revocation retained the private bootstrap outbox or removed another chat")
	}
}

func TestCompanionInitializationRejectsNestedCollectionErrors(t *testing.T) {
	response := wire.ChatdNode{Tag: "iq", Content: []wire.ChatdNode{{Tag: "sync", Content: []wire.ChatdNode{{Tag: "collection", Content: []wire.ChatdNode{{Tag: "error", Attrs: map[string]string{"code": "409"}}}}}}}}
	if err := linkedDeviceCollectionError(response); err == nil {
		t.Fatal("rejected conditional collection update was accepted")
	}
}
