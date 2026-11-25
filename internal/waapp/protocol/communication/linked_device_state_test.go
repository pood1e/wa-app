package communication

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestDeviceKeyIndexPreservesRemoteAndReservedIndexes(t *testing.T) {
	current := newMessageParty(t)
	credentials, _, err := current.Export()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	list := &waAdv.ADVKeyIndexList{RawID: proto.Uint32(12), CurrentIndex: proto.Uint32(4), Timestamp: proto.Uint64(uint64(now.Unix())), AccountType: waAdv.ADVEncryptionType_E2EE.Enum()}
	remote, err := wire.SignDeviceKeyIndexList(credentials.GetIdentity().GetPrivate(), list)
	if err != nil {
		t.Fatal(err)
	}
	list.CurrentIndex = proto.Uint32(5)
	reserved, err := wire.SignDeviceKeyIndexList(credentials.GetIdentity().GetPrivate(), list)
	if err != nil {
		t.Fatal(err)
	}
	for _, local := range [][]byte{nil, reserved} {
		current.LinkedDevices = &protocolv1.LinkedDeviceState{SignedKeyIndexList: local}
		next, err := nextDeviceKeyIndex(&current, now, remote, []uint32{0, 7})
		if err != nil || next.GetRawID() != 12 || next.GetCurrentIndex() != 8 || next.GetTimestamp() <= uint64(now.Unix()) {
			t.Fatal("remote or locally reserved device identity was reset", err)
		}
	}
	remote[len(remote)-1] ^= 1
	if _, err := nextDeviceKeyIndex(&current, now, remote, []uint32{0, 7}); err == nil {
		t.Fatal("unverified remote key indexes were accepted")
	}
	current.LinkedDevices = nil
	if _, err := nextDeviceKeyIndex(&current, now, nil, []uint32{0, 7}); err == nil {
		t.Fatal("existing companions were assigned a new account identity")
	}
}

func TestRemovedDeviceKeysArePrunedAfterDiscoveryGrace(t *testing.T) {
	current := newMessageParty(t)
	current.RegistrationJID = "12025550123@s.whatsapp.net"
	now := time.Now()
	current.LinkedDevices = &protocolv1.LinkedDeviceState{Devices: []*protocolv1.LinkedDeviceRecord{{DeviceRef: linkedTestDevice, LinkedAt: now.Unix()}}}
	identity := SignalSenderKey(linkedTestDevice)
	current.Signal.RemoteIdentities[identity] = "synthetic-public-key"
	if pruneLinkedDeviceState(&current, nil, now) || len(current.LinkedDevices.GetDevices()) != 1 {
		t.Fatal("discovery lag removed a recently linked companion")
	}
	if pruneLinkedDeviceState(&current, []discoveredDevice{{Ref: linkedTestDevice}}, now.Add(2*time.Minute)) {
		t.Fatal("active companion keys were pruned")
	}
	if !pruneLinkedDeviceState(&current, nil, now.Add(2*time.Minute)) || len(current.LinkedDevices.GetDevices()) != 0 || current.Signal.RemoteIdentities[identity] != "" {
		t.Fatal("revoked companion credentials survived cleanup")
	}
}

func TestDeviceRosterUsesTheLatestSignedRevocationIndexes(t *testing.T) {
	current := newMessageParty(t)
	current.RegistrationJID = "12025550123@s.whatsapp.net"
	private, err := current.Signal.Identity.PrivateBytes()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	signed := func(index uint32, timestamp uint64, valid []uint32) []byte {
		t.Helper()
		raw, err := wire.SignDeviceKeyIndexList(private, &waAdv.ADVKeyIndexList{RawID: proto.Uint32(12), CurrentIndex: proto.Uint32(index), Timestamp: proto.Uint64(timestamp), ValidIndexes: valid})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	current.LinkedDevices = &protocolv1.LinkedDeviceState{SignedKeyIndexList: signed(6, uint64(now.Unix()), []uint32{0, 6})}
	connection, _, _ := groupTestConnection(t, current)
	operation := connectionState{connection: connection, current: current}
	old := signed(5, uint64(now.Unix()-1), []uint32{0, 5})
	roster := []discoveredDevice{{Ref: current.RegistrationJID}, {Ref: linkedTestDevice, KeyIndex: proto.Uint32(5)}}
	confirmed, err := operation.confirmedDeviceRoster(roster, old)
	if err != nil || len(confirmed.devices) != 1 {
		t.Fatal("a cached roster resurrected a cryptographically revoked device", err)
	}
	newer := signed(7, uint64(now.Unix()+1), []uint32{0, 7})
	confirmed, err = operation.confirmedDeviceRoster([]discoveredDevice{{Ref: linkedTestDevice, KeyIndex: proto.Uint32(7)}}, newer)
	if err != nil || len(confirmed.devices) != 1 {
		t.Fatal("newer primary-signed device indexes were discarded", err)
	}
	newer[len(newer)-1] ^= 1
	if _, err := operation.confirmedDeviceRoster(roster, newer); err == nil {
		t.Fatal("device discovery accepted unverified account indexes")
	}
}
