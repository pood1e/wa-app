package communication

import (
	"bytes"
	"context"
	"errors"
	"time"

	"go.mau.fi/whatsmeow/types"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type linkedDeviceRoster struct {
	devices            []discoveredDevice
	signedKeyIndexList []byte
}

func (s *connectionState) ownDeviceRoster(ctx context.Context) (linkedDeviceRoster, error) {
	pn, _ := ownMessageJIDs(&s.current)
	request, requested, err := deviceUsyncIQ(s.connection.ids.NewID("wa_devices_"), []string{pn})
	if err != nil {
		return linkedDeviceRoster{}, err
	}
	response, err := s.query(ctx, request)
	if err != nil {
		return linkedDeviceRoster{}, err
	}
	s.changed = ApplyChatdSessionUpdateState(&s.current, ChatdSessionUpdate{ContactHints: contactUsyncIdentityHints(s.connection.Input.WAAccountID, response, s.connection.clock.Now(), []string{pn})}) || s.changed
	roster, err := deviceRosterFromUsync(response, requested)
	if err != nil {
		return linkedDeviceRoster{}, err
	}
	usync, _ := ChatdChild(response, "usync")
	list, _ := ChatdChild(usync, "list")
	for _, user := range wire.ChatdChildren(list) {
		jid, err := types.ParseJID(user.Attrs["jid"])
		if user.Tag == "user" && err == nil && requested[jid.ToNonAD().String()] {
			devices, _ := ChatdChild(user, "devices")
			index, _ := ChatdChild(devices, "key-index-list")
			raw, _ := index.Content.([]byte)
			return s.confirmedDeviceRoster(roster, raw)
		}
	}
	return s.confirmedDeviceRoster(roster, nil)
}

// ListLinkedDevices loads the server-confirmed companions of this account.
func (c *Connection) ListLinkedDevices(ctx context.Context) ([]*waappv1.LinkedDevice, error) {
	return withConnectionState(ctx, c, func(ctx context.Context, s *connectionState) ([]*waappv1.LinkedDevice, error) {
		roster, err := s.ownDeviceRoster(ctx)
		if err != nil {
			return nil, err
		}
		devices := make([]*waappv1.LinkedDevice, 0, len(roster.devices))
		for _, record := range roster.devices {
			if _, err := ownLinkedDevice(&s.current, record.Ref); err == nil {
				devices = append(devices, linkedDeviceView(&s.current, record.Ref))
			}
		}
		return devices, nil
	})
}

// LinkDevice confirms primary-device authorization; companion readiness is separate.
func (c *Connection) LinkDevice(ctx context.Context, value string) (*waappv1.LinkDeviceResponse, error) {
	qr, err := wire.ParseDevicePairingQR(value)
	if err != nil {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	defer clear(qr.Secret)
	return withConnectionState(ctx, c, func(ctx context.Context, s *connectionState) (*waappv1.LinkDeviceResponse, error) {
		roster, err := s.ownDeviceRoster(ctx)
		if err != nil {
			return nil, err
		}
		indexes, err := activeDeviceKeyIndexes(&s.current, roster.devices)
		if err != nil {
			return nil, err
		}
		security, err := s.pairingSecurity(ctx)
		if err != nil {
			return nil, err
		}
		list, err := nextDeviceKeyIndex(&s.current, c.clock.Now(), roster.signedKeyIndexList, indexes)
		if err != nil {
			return nil, err
		}
		response, uncertain, err := s.authorizeDevice(ctx, qr, list, indexes, security)
		if err != nil {
			return &waappv1.LinkDeviceResponse{OutcomeUnknown: uncertain}, err
		}
		deviceNode, ok := ChatdChild(response, "device")
		jid, err := ownLinkedDevice(&s.current, deviceNode.Attrs["jid"])
		if !ok || err != nil {
			return &waappv1.LinkDeviceResponse{OutcomeUnknown: true}, errors.New("device linking response did not identify the new companion")
		}
		props, _ := ChatdChild(response, "companion-props")
		propsData, _ := props.Content.([]byte)
		if len(propsData) > 64<<10 {
			propsData = nil
		}
		record := &protocolv1.LinkedDeviceRecord{DeviceRef: jid.String(), KeyIndex: list.GetCurrentIndex(), CompanionProps: bytes.Clone(propsData), LinkedAt: c.clock.Now().Unix(),
			BootstrapRetryAt: c.clock.Now().Add(8 * time.Second).Unix()}
		removeLinkedDeviceState(&s.current, jid.Device)
		s.current.LinkedDevices.Devices = append(s.current.LinkedDevices.GetDevices(), record)
		s.current.Signal.RemoteIdentities[SignalSenderKey(signalIdentityForDevice(&s.current, jid.String()))] = wire.B64u(qr.IdentityPublic)
		s.changed = true
		return &waappv1.LinkDeviceResponse{Device: linkedDeviceView(&s.current, jid.String())}, nil
	})
}

func linkedDeviceIQ(id, tag string, attrs map[string]string, content any) wire.ChatdNode {
	return wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": id, "type": "set", "xmlns": "md", "to": types.ServerJID.String()},
		Content: []wire.ChatdNode{{Tag: tag, Attrs: attrs, Content: content}}}
}
