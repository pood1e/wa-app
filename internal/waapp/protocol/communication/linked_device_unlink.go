package communication

import (
	"context"
	"slices"
	"strconv"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

// UnlinkDevice revokes an own companion and publishes the remaining signed indexes.
func (c *Connection) UnlinkDevice(ctx context.Context, ref string) error {
	_, err := withConnectionState(ctx, c, func(ctx context.Context, s *connectionState) (struct{}, error) {
		jid, err := ownLinkedDevice(&s.current, ref)
		if err != nil {
			return struct{}{}, err
		}
		roster, err := s.ownDeviceRoster(ctx)
		if err != nil {
			return struct{}{}, err
		}
		active, err := activeDeviceKeyIndexes(&s.current, roster.devices)
		if err != nil {
			return struct{}{}, err
		}
		list, err := nextDeviceKeyIndex(&s.current, c.clock.Now(), roster.signedKeyIndexList, active)
		if err != nil {
			return struct{}{}, err
		}
		remaining := slices.DeleteFunc(slices.Clone(roster.devices), func(device discoveredDevice) bool {
			candidate, err := ownLinkedDevice(&s.current, device.Ref)
			return err == nil && candidate.Device == jid.Device
		})
		indexes, err := activeDeviceKeyIndexes(&s.current, remaining)
		if err != nil {
			return struct{}{}, err
		}
		list.ValidIndexes = append(indexes, list.GetCurrentIndex())
		slices.Sort(list.ValidIndexes)
		list.ValidIndexes = slices.Compact(list.ValidIndexes)
		private, err := s.current.Signal.Identity.PrivateBytes()
		if err != nil {
			return struct{}{}, err
		}
		encoded, err := wire.SignDeviceKeyIndexList(private, list)
		if err != nil {
			return struct{}{}, err
		}
		if _, err := s.query(ctx, linkedDeviceIQ(c.ids.NewID("wa_unlink_"), "remove-companion-device", map[string]string{"jid": jid.String(), "reason": "user_initiated"}, nil)); err != nil {
			return struct{}{}, err
		}
		removeLinkedDeviceState(&s.current, jid.Device)
		if s.current.LinkedDevices == nil {
			s.current.LinkedDevices = new(protocolv1.LinkedDeviceState)
		}
		s.current.LinkedDevices.SignedKeyIndexList = encoded
		s.changed = true
		if err := c.SaveState(ctx, c.Input.ClientProfileID, s.current); err != nil {
			return struct{}{}, err
		}
		response, err := s.query(ctx, linkedDeviceIQ(c.ids.NewID("wa_device_indexes_"), "key-index-list", map[string]string{"ts": strconv.FormatUint(list.GetTimestamp(), 10)}, encoded))
		if err != nil {
			return struct{}{}, err
		}
		if _, retry := ChatdChild(response, "retry-ts"); retry {
			return struct{}{}, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "device was revoked; its signed index publication needs to be retried", false)
		}
		return struct{}{}, nil
	})
	return err
}
