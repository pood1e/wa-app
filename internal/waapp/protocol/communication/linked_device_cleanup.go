package communication

import (
	"slices"
	"time"

	"go.mau.fi/whatsmeow/types"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

func removeLinkedDeviceState(current *state.NativeState, deviceID uint16) {
	for key, message := range current.SentMessages {
		if jid, err := ownLinkedDevice(current, message.GetContactRef()); err == nil && jid.Device == deviceID {
			delete(current.SentMessages, key)
		}
	}
	if current.LinkedDevices != nil {
		current.LinkedDevices.Devices = slices.DeleteFunc(current.LinkedDevices.GetDevices(), func(record *protocolv1.LinkedDeviceRecord) bool {
			jid, err := ownLinkedDevice(current, record.GetDeviceRef())
			return err == nil && jid.Device == deviceID
		})
	}
	pn, lid := ownMessageJIDs(current)
	for _, owner := range []string{pn, lid} {
		jid, err := types.ParseJID(owner)
		if err != nil {
			continue
		}
		jid.Device = deviceID
		identity := SignalSenderKey(jid.String())
		delete(current.Signal.RemoteIdentities, identity)
		delete(current.Signal.Sessions, identity)
	}
}

func pruneLinkedDeviceState(current *state.NativeState, roster []discoveredDevice, now time.Time) bool {
	active := make(map[uint16]bool, len(roster))
	for _, device := range roster {
		if jid, err := ownLinkedDevice(current, device.Ref); err == nil {
			active[jid.Device] = true
		}
	}
	var removed []uint16
	for _, record := range current.LinkedDevices.GetDevices() {
		jid, err := ownLinkedDevice(current, record.GetDeviceRef())
		// Device discovery can briefly lag behind a successful authorization.
		if err == nil && !active[jid.Device] && record.GetLinkedAt() <= now.Add(-time.Minute).Unix() {
			removed = append(removed, jid.Device)
		}
	}
	for _, id := range removed {
		removeLinkedDeviceState(current, id)
	}
	return len(removed) != 0
}
