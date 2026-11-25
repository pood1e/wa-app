package communication

import (
	"strconv"
	"testing"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func pairingDetails(t *testing.T, request wire.ChatdNode) *waAdv.ADVDeviceIdentity {
	t.Helper()
	pair, _ := ChatdChild(request, "pair-device")
	identity, _ := ChatdChild(pair, "device-identity")
	var envelope waAdv.ADVSignedDeviceIdentityHMAC
	var signed waAdv.ADVSignedDeviceIdentity
	var details waAdv.ADVDeviceIdentity
	if proto.Unmarshal(identity.Content.([]byte), &envelope) != nil || proto.Unmarshal(envelope.GetDetails(), &signed) != nil || proto.Unmarshal(signed.GetDetails(), &details) != nil {
		t.Fatal("pairing request has no valid ADV identity")
	}
	return &details
}

func TestPairingCorrectsServerTimestampWithTheSameReservedIdentity(t *testing.T) {
	current := newMessageParty(t)
	current.RegistrationJID = "12025550123@s.whatsapp.net"
	connection, peer := pairingTestConnection(t, current)
	qr, _ := pairingTestQR(t)
	type result struct {
		response *waappv1.LinkDeviceResponse
		err      error
	}
	done := make(chan result, 1)
	go func() { response, err := connection.LinkDevice(t.Context(), qr); done <- result{response, err} }()
	request := deviceTestIQ(t, peer, "usync")
	deviceTestResponse(t, peer, request, ownRosterNode(false, ""))
	request = pairingTestIQ(t, peer)
	first := pairingDetails(t, request)
	corrected := uint64(connection.clock.Now().Unix() - 10)
	deviceTestResponse(t, peer, request, wire.ChatdNode{Tag: "retry-ts", Attrs: map[string]string{"ts": strconv.FormatUint(corrected, 10)}})
	request = pairingTestIQ(t, peer)
	second := pairingDetails(t, request)
	reserved, err := connection.loadState(t.Context(), "current")
	var list waAdv.ADVSignedKeyIndexList
	var indexes waAdv.ADVKeyIndexList
	if err != nil || proto.Unmarshal(reserved.LinkedDevices.GetSignedKeyIndexList(), &list) != nil || proto.Unmarshal(list.GetDetails(), &indexes) != nil {
		t.Fatal("corrected reservation was not saved before sending", err)
	}
	if first.GetRawID() != second.GetRawID() || first.GetKeyIndex() != second.GetKeyIndex() || second.GetTimestamp() != corrected || indexes.GetTimestamp() != corrected {
		t.Fatal("timestamp correction rotated the account or companion index")
	}
	deviceTestResponse(t, peer, request, wire.ChatdNode{Tag: "device", Attrs: map[string]string{"jid": linkedTestDevice}})
	linked := <-done
	if linked.err != nil || linked.response.GetDevice().GetDeviceRef() != linkedTestDevice || linked.response.GetOutcomeUnknown() {
		t.Fatal("server-authorized timestamp continuation did not complete", linked.err)
	}
}

func TestPairingRejectsInvalidAndRepeatedTimestampCorrections(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "invalid", "18446744073709551615", "repeat"} {
		t.Run(value, func(t *testing.T) {
			current := newMessageParty(t)
			current.RegistrationJID = "12025550123@s.whatsapp.net"
			connection, peer := pairingTestConnection(t, current)
			qr, _ := pairingTestQR(t)
			done := make(chan error, 1)
			go func() { _, err := connection.LinkDevice(t.Context(), qr); done <- err }()
			request := deviceTestIQ(t, peer, "usync")
			deviceTestResponse(t, peer, request, ownRosterNode(false, ""))
			request = pairingTestIQ(t, peer)
			if value == "repeat" {
				value = strconv.FormatInt(connection.clock.Now().Unix(), 10)
				deviceTestResponse(t, peer, request, wire.ChatdNode{Tag: "retry-ts", Attrs: map[string]string{"ts": value}})
				request = pairingTestIQ(t, peer)
			}
			deviceTestResponse(t, peer, request, wire.ChatdNode{Tag: "retry-ts", Attrs: map[string]string{"ts": value}})
			if err := <-done; err == nil {
				t.Fatal("invalid or repeated timestamp correction was authorized")
			}
		})
	}
}
