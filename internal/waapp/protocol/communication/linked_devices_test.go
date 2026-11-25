package communication

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"slices"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

const linkedTestDevice = "12025550123:2@s.whatsapp.net"

func pairingTestQR(t *testing.T) (string, []byte) {
	t.Helper()
	companion := newMessageParty(t)
	secret := bytes.Repeat([]byte{7}, 32)
	return strings.Join([]string{"synthetic-pairing-reference", companion.ChatStatic.Public, companion.Signal.Identity.Public, wire.B64u(secret), "1"}, ","), secret
}

func ownRosterNode(device bool, index string) wire.ChatdNode {
	list := []wire.ChatdNode{{Tag: "device", Attrs: map[string]string{"id": "0"}}}
	if device {
		list = append(list, wire.ChatdNode{Tag: "device", Attrs: map[string]string{"id": "2", "key-index": index}})
	}
	return ownRosterDevices(list)
}

func ownRosterDevices(list []wire.ChatdNode) wire.ChatdNode {
	return wire.ChatdNode{Tag: "usync", Content: []wire.ChatdNode{{Tag: "list", Content: []wire.ChatdNode{{Tag: "user", Attrs: map[string]string{"jid": "12025550123@s.whatsapp.net"},
		Content: []wire.ChatdNode{{Tag: "devices", Content: []wire.ChatdNode{{Tag: "device-list", Content: list}}}}}}}}}
}

func TestUnlinkPreservesOtherCompanionIndexes(t *testing.T) {
	current := bootstrapTestState(t)
	private, err := current.Signal.Identity.PrivateBytes()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := wire.SignDeviceKeyIndexList(private, &waAdv.ADVKeyIndexList{RawID: proto.Uint32(12), CurrentIndex: proto.Uint32(5), Timestamp: proto.Uint64(uint64(time.Now().Unix())), ValidIndexes: []uint32{0, 2, 5}})
	if err != nil {
		t.Fatal(err)
	}
	current.LinkedDevices.SignedKeyIndexList = encoded
	connection, peer, _ := groupTestConnection(t, current)
	done := make(chan error, 1)
	go func() { done <- connection.UnlinkDevice(t.Context(), linkedTestDevice) }()
	request := deviceTestIQ(t, peer, "usync")
	deviceTestResponse(t, peer, request, ownRosterDevices([]wire.ChatdNode{
		{Tag: "device", Attrs: map[string]string{"id": "0"}},
		{Tag: "device", Attrs: map[string]string{"id": "2", "key-index": "2"}},
		{Tag: "device", Attrs: map[string]string{"id": "4", "key-index": "5"}},
	}))
	request = deviceTestIQ(t, peer, "md")
	deviceTestResponse(t, peer, request)
	request = deviceTestIQ(t, peer, "md")
	publication, _ := ChatdChild(request, "key-index-list")
	var signed waAdv.ADVSignedKeyIndexList
	var list waAdv.ADVKeyIndexList
	if proto.Unmarshal(publication.Content.([]byte), &signed) != nil || proto.Unmarshal(signed.GetDetails(), &list) != nil || list.GetRawID() != 12 || list.GetCurrentIndex() != 6 || !slices.Equal(list.GetValidIndexes(), []uint32{0, 5, 6}) {
		t.Fatal("revocation reset the account identity or removed another active companion")
	}
	deviceTestResponse(t, peer, request)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func deviceTestIQ(t *testing.T, peer *ChatdTransport, namespace string) wire.ChatdNode {
	t.Helper()
	request, err := peer.ReadNode()
	if err != nil || request.Tag != "iq" || request.Attrs["xmlns"] != namespace {
		t.Fatal("device operation lost its native IQ routing", err)
	}
	return request
}

func deviceTestResponse(t *testing.T, peer *ChatdTransport, request wire.ChatdNode, children ...wire.ChatdNode) {
	t.Helper()
	if err := peer.SendNode(wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": request.Attrs["id"], "type": "result"}, Content: children}); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceLinkListAndUnlinkUseTheOwnedConnection(t *testing.T) {
	for _, platform := range []protocolv1.ClientPlatform{protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID, protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS} {
		t.Run(platform.String(), func(t *testing.T) {
			current := newMessageParty(t)
			current.Client.Platform = platform
			current.RegistrationJID = "12025550123@s.whatsapp.net"
			connection, peer := pairingTestConnection(t, current)
			original := connection.Session
			qr, secret := pairingTestQR(t)
			type result struct {
				response *waappv1.LinkDeviceResponse
				err      error
			}
			done := make(chan result, 1)
			go func() { response, err := connection.LinkDevice(t.Context(), qr); done <- result{response, err} }()
			request := deviceTestIQ(t, peer, "usync")
			deviceTestResponse(t, peer, request, ownRosterNode(false, ""))
			request = pairingTestIQ(t, peer)
			pairing, _ := ChatdChild(request, "pair-device")
			proofNode, _ := ChatdChild(pairing, "device-identity")
			listNode, _ := ChatdChild(pairing, "key-index-list")
			propsNode, _ := ChatdChild(pairing, "client-props")
			var pairingProps waCompanionReg.ClientPairingProps
			if proto.Unmarshal(propsNode.Content.([]byte), &pairingProps) != nil || !pairingProps.GetIsChatDbLidMigrated() || pairingProps.GetIsSyncdSnapshotRecoveryEnabled() {
				t.Fatal("pairing omitted the supported LID mode or advertised unsupported snapshot recovery")
			}
			var proof waAdv.ADVSignedDeviceIdentityHMAC
			if proto.Unmarshal(proofNode.Content.([]byte), &proof) != nil {
				t.Fatal("pair request omitted the signed identity")
			}
			mac := hmac.New(sha256.New, secret)
			_, _ = mac.Write(proof.GetDetails())
			reserved, err := connection.loadState(t.Context(), "current")
			if err != nil || !hmac.Equal(mac.Sum(nil), proof.GetHMAC()) || len(reserved.LinkedDevices.GetSignedKeyIndexList()) == 0 || listNode.Attrs["ts"] == "" {
				t.Fatal("pairing did not reserve its index before sending the bound proof", err)
			}
			props, _ := proto.Marshal(&waCompanionReg.DeviceProps{Os: proto.String("Fixture browser"), PlatformType: waCompanionReg.DeviceProps_CHROME.Enum()})
			deviceTestResponse(t, peer, request, wire.ChatdNode{Tag: "device", Attrs: map[string]string{"jid": linkedTestDevice}}, wire.ChatdNode{Tag: "companion-props", Content: props})
			linked := <-done
			if linked.err != nil || linked.response.GetDevice().GetDeviceRef() != linkedTestDevice || linked.response.GetOutcomeUnknown() {
				t.Fatal("native companion acknowledgement was lost", linked.err)
			}
			restored, err := connection.loadState(t.Context(), "current")
			if err != nil || len(restored.LinkedDevices.GetDevices()) != 1 || restored.LinkedDevices.GetDevices()[0].GetKeyIndex() != 1 || !bytes.Equal(restored.LinkedDevices.GetDevices()[0].GetCompanionProps(), props) {
				t.Fatal("paired device metadata did not survive protocol recovery", err)
			}
			listed := make(chan []*waappv1.LinkedDevice, 1)
			go func() { devices, _ := connection.ListLinkedDevices(t.Context()); listed <- devices }()
			request = deviceTestIQ(t, peer, "usync")
			deviceTestResponse(t, peer, request, ownRosterNode(true, "1"))
			if devices := <-listed; len(devices) != 1 || devices[0].GetName() != "Fixture browser" {
				t.Fatal("device list exposed the primary or discarded companion metadata")
			}
			unlinked := make(chan error, 1)
			go func() { unlinked <- connection.UnlinkDevice(t.Context(), linkedTestDevice) }()
			request = deviceTestIQ(t, peer, "usync")
			deviceTestResponse(t, peer, request, ownRosterNode(true, "1"))
			request = deviceTestIQ(t, peer, "md")
			remove, _ := ChatdChild(request, "remove-companion-device")
			if remove.Attrs["jid"] != linkedTestDevice || remove.Attrs["reason"] != "user_initiated" {
				t.Fatal("unlink changed the authorized device or reason")
			}
			deviceTestResponse(t, peer, request)
			request = deviceTestIQ(t, peer, "md")
			publication, _ := ChatdChild(request, "key-index-list")
			var signedIndexes waAdv.ADVSignedKeyIndexList
			var remainingIndexes waAdv.ADVKeyIndexList
			if proto.Unmarshal(publication.Content.([]byte), &signedIndexes) != nil || proto.Unmarshal(signedIndexes.GetDetails(), &remainingIndexes) != nil || remainingIndexes.GetCurrentIndex() != 2 || slices.Contains(remainingIndexes.GetValidIndexes(), uint32(1)) {
				t.Fatal("revocation retained the removed companion's signed account index")
			}
			deviceTestResponse(t, peer, request)
			if err := <-unlinked; err != nil {
				t.Fatal(err)
			}
			restored, err = connection.loadState(t.Context(), "current")
			if err != nil || len(restored.LinkedDevices.GetDevices()) != 0 || connection.Session != original {
				t.Fatal("unlink retained device keys or replaced the primary connection", err)
			}
		})
	}
}

func TestDeviceAuthorizationRejectsForeignAndPrimaryDevices(t *testing.T) {
	current := newMessageParty(t)
	current.RegistrationJID = "12025550123@s.whatsapp.net"
	connection, _ := pairingTestConnection(t, current)
	for _, ref := range []string{"12025550123@s.whatsapp.net", "12025550999:2@s.whatsapp.net", "12025550123:2@g.us", "invalid"} {
		if err := connection.UnlinkDevice(t.Context(), ref); err == nil {
			t.Fatal("unlink authorized a primary, foreign or invalid device")
		}
	}
}

func TestPairingTransportFailurePreservesTheReservedIndex(t *testing.T) {
	current := newMessageParty(t)
	current.RegistrationJID = "12025550123@s.whatsapp.net"
	connection, peer := pairingTestConnection(t, current)
	qr, _ := pairingTestQR(t)
	done := make(chan *waappv1.LinkDeviceResponse, 1)
	go func() { response, _ := connection.LinkDevice(context.Background(), qr); done <- response }()
	request := deviceTestIQ(t, peer, "usync")
	deviceTestResponse(t, peer, request, ownRosterNode(false, ""))
	pairingTestIQ(t, peer)
	_ = connection.Session.Conn.Close()
	if response := <-done; !response.GetOutcomeUnknown() || response.GetDevice() != nil {
		t.Fatal("interrupted authorization was reported as completed")
	}
	restored, err := connection.loadState(t.Context(), "current")
	if err != nil || len(restored.LinkedDevices.GetSignedKeyIndexList()) == 0 || len(restored.LinkedDevices.GetDevices()) != 0 {
		t.Fatal("interrupted authorization forgot its reserved key index", err)
	}
}

func TestPairingChallengesRequireNativeVerification(t *testing.T) {
	for _, tag := range []string{"retry-after-challenges", "retry-with-method"} {
		t.Run(tag, func(t *testing.T) {
			current := newMessageParty(t)
			current.RegistrationJID = "12025550123@s.whatsapp.net"
			connection, peer := pairingTestConnection(t, current)
			qr, _ := pairingTestQR(t)
			done := make(chan error, 1)
			go func() {
				response, err := connection.LinkDevice(t.Context(), qr)
				if response.GetDevice() != nil {
					t.Error("challenge response claimed an authorized device")
				}
				done <- err
			}()
			request := deviceTestIQ(t, peer, "usync")
			deviceTestResponse(t, peer, request, ownRosterNode(false, ""))
			request = pairingTestIQ(t, peer)
			deviceTestResponse(t, peer, request, wire.ChatdNode{Tag: tag})
			if shared.ToProtoError(<-done).GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_VERIFICATION_REQUIRED {
				t.Fatal("native verification request was interpreted as pairing success")
			}
		})
	}
}
