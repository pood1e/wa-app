package communication

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"time"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func ownLinkedDevice(current *state.NativeState, ref string) (types.JID, error) {
	jid, err := types.ParseJID(ref)
	pn, lid := ownMessageJIDs(current)
	if err != nil || jid.Device == 0 || jid.RawAgent != 0 || jid.Integrator != 0 ||
		(jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) ||
		(jid.ToNonAD().String() != pn && jid.ToNonAD().String() != lid) {
		return types.JID{}, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "a companion belonging to this account is required", false)
	}
	return jid, nil
}

func linkedDeviceRecord(current *state.NativeState, ref string) *protocolv1.LinkedDeviceRecord {
	jid, err := ownLinkedDevice(current, ref)
	if err != nil {
		return nil
	}
	for _, record := range current.LinkedDevices.GetDevices() {
		candidate, err := ownLinkedDevice(current, record.GetDeviceRef())
		if err == nil && candidate.Device == jid.Device {
			return record
		}
	}
	return nil
}

func linkedDeviceView(current *state.NativeState, ref string) *waappv1.LinkedDevice {
	device := &waappv1.LinkedDevice{DeviceRef: ref}
	if record := linkedDeviceRecord(current, ref); record != nil {
		var props waCompanionReg.DeviceProps
		if len(record.GetCompanionProps()) <= 64<<10 && proto.Unmarshal(record.GetCompanionProps(), &props) == nil {
			device.Name = props.GetOs()
			if props.GetPlatformType() != waCompanionReg.DeviceProps_UNKNOWN {
				device.Platform = props.GetPlatformType().String()
			}
		}
		if record.GetLinkedAt() > 0 {
			device.LinkedAt = shared.ProtoTimestamp(time.Unix(record.GetLinkedAt(), 0))
		}
	}
	return device
}

func nextDeviceKeyIndex(current *state.NativeState, now time.Time, remote []byte, active []uint32) (*waAdv.ADVKeyIndexList, error) {
	list, err := verifiedDeviceKeyIndexes(current, now, remote)
	if err != nil {
		return nil, err
	}
	if list == nil {
		if slices.ContainsFunc(active, func(index uint32) bool { return index != 0 }) {
			return nil, errors.New("existing linked devices require their signed account key index list")
		}
		list = &waAdv.ADVKeyIndexList{AccountType: waAdv.ADVEncryptionType_E2EE.Enum()}
		var value [4]byte
		if _, err := rand.Read(value[:]); err != nil {
			return nil, err
		}
		list.RawID = proto.Uint32(max(1, binary.BigEndian.Uint32(value[:])&math.MaxInt32))
	}
	for _, index := range active {
		list.CurrentIndex = proto.Uint32(max(list.GetCurrentIndex(), index))
	}
	if list.GetCurrentIndex() >= math.MaxInt32 || list.GetTimestamp() > uint64(max(0, now.Unix())+86400) {
		return nil, errors.New("linked-device key index or timestamp is exhausted")
	}
	list.CurrentIndex = proto.Uint32(list.GetCurrentIndex() + 1)
	list.Timestamp = proto.Uint64(max(uint64(max(0, now.Unix())), list.GetTimestamp()+1))
	return list, nil
}

func activeDeviceKeyIndexes(current *state.NativeState, roster []discoveredDevice) ([]uint32, error) {
	indexes := []uint32{0}
	for _, device := range roster {
		jid, err := types.ParseJID(device.Ref)
		if err != nil {
			return nil, err
		}
		if jid.Device == 0 {
			continue
		}
		if device.KeyIndex != nil {
			indexes = append(indexes, *device.KeyIndex)
		} else if record := linkedDeviceRecord(current, device.Ref); record != nil {
			indexes = append(indexes, record.GetKeyIndex())
		} else {
			return nil, errors.New("linked-device roster is missing an existing key index")
		}
	}
	slices.Sort(indexes)
	return slices.Compact(indexes), nil
}
