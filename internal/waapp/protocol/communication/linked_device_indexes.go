package communication

import (
	"errors"
	"slices"
	"time"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"google.golang.org/protobuf/proto"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func verifiedDeviceKeyIndexes(current *state.NativeState, now time.Time, remote []byte) (*waAdv.ADVKeyIndexList, error) {
	var list *waAdv.ADVKeyIndexList
	for _, raw := range [][]byte{current.LinkedDevices.GetSignedKeyIndexList(), remote} {
		if len(raw) == 0 {
			continue
		}
		var signed waAdv.ADVSignedKeyIndexList
		candidate := new(waAdv.ADVKeyIndexList)
		if len(raw) > 16<<10 || proto.Unmarshal(raw, &signed) != nil || proto.Unmarshal(signed.GetDetails(), candidate) != nil {
			return nil, errors.New("linked-device key index list is invalid")
		}
		public, err := current.Signal.Identity.PublicBytes()
		if err != nil {
			return nil, err
		}
		valid, err := wire.SignalVerifyCurve25519(public, append([]byte{6, 2}, signed.GetDetails()...), signed.GetAccountSignature())
		if err != nil || !valid || candidate.GetRawID() == 0 || candidate.GetAccountType() != waAdv.ADVEncryptionType_E2EE || candidate.GetTimestamp() > uint64(max(0, now.Unix())+86400) {
			return nil, errors.New("linked-device identity requires the matching primary-device signing material")
		}
		if list != nil && list.GetRawID() != candidate.GetRawID() {
			return nil, errors.New("linked-device account identity changed")
		}
		if list == nil {
			list = candidate
			continue
		}
		index := max(list.GetCurrentIndex(), candidate.GetCurrentIndex())
		if candidate.GetTimestamp() > list.GetTimestamp() || candidate.GetTimestamp() == list.GetTimestamp() && candidate.GetCurrentIndex() > list.GetCurrentIndex() {
			list = candidate
		}
		list.CurrentIndex = proto.Uint32(index)
	}
	return list, nil
}

func (s *connectionState) confirmedDeviceRoster(roster []discoveredDevice, raw []byte) (linkedDeviceRoster, error) {
	indexes, err := verifiedDeviceKeyIndexes(&s.current, s.connection.clock.Now(), raw)
	if err != nil {
		return linkedDeviceRoster{}, err
	}
	if indexes != nil {
		roster = slices.DeleteFunc(roster, func(device discoveredDevice) bool {
			index := device.KeyIndex
			if index == nil {
				if record := linkedDeviceRecord(&s.current, device.Ref); record != nil {
					index = &record.KeyIndex
				}
			}
			return index != nil && *index != 0 && !slices.Contains(indexes.GetValidIndexes(), *index)
		})
	}
	s.changed = pruneLinkedDeviceState(&s.current, roster, s.connection.clock.Now()) || s.changed
	return linkedDeviceRoster{devices: roster, signedKeyIndexList: raw}, nil
}
