package communication

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (p *messagePreparation) recipientDevices(ctx context.Context, users []string) ([]string, error) {
	list := make([]wire.ChatdNode, 0, len(users))
	requested := make(map[string]bool, len(users))
	for _, user := range users {
		jid, err := types.ParseJID(wacore.NormalizeWAJID(user))
		if err != nil || jid.User == "" || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) {
			return nil, errors.New("message recipient address is invalid")
		}
		base := jid.ToNonAD().String()
		if !requested[base] {
			list = append(list, wire.ChatdNode{Tag: "user", Attrs: map[string]string{"jid": base}})
			requested[base] = true
		}
	}
	request := wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": NewTextProviderMessageID(""), "type": "get", "xmlns": "usync", "to": "s.whatsapp.net"}, Content: []wire.ChatdNode{{
		Tag: "usync", Attrs: map[string]string{"sid": NewTextProviderMessageID(""), "mode": "query", "context": "message", "last": "true", "index": "0"}, Content: []wire.ChatdNode{
			{Tag: "query", Content: []wire.ChatdNode{{Tag: "devices", Attrs: map[string]string{"version": "2"}}, {Tag: "lid"}}},
			{Tag: "list", Content: list},
		},
	}}}
	response, err := p.query(ctx, request, "recipient device query timed out")
	if err != nil {
		return nil, err
	}
	var hints []wacore.WAContactHint
	for _, contact := range ContactsFromContactUsyncIQ(p.input.WAAccountID, response, time.Now(), users) {
		hint := wacore.WAContactHint{LIDJID: contact.GetJid(), PNJID: wacore.NormalizeWAJID(contact.GetNumber())}.Normalized()
		if hint.Valid() {
			hints = append(hints, hint)
		}
	}
	p.record(nil, ChatdSessionUpdate{ContactHints: hints})
	return devicesFromUsync(response, requested)
}

func devicesFromUsync(response wire.ChatdNode, requested map[string]bool) ([]string, error) {
	usync, ok := ChatdChild(response, "usync")
	if !ok {
		return nil, errors.New("recipient response is missing usync")
	}
	list, ok := ChatdChild(usync, "list")
	if !ok {
		return nil, errors.New("recipient response is missing its list")
	}
	var result []string
	seen := make(map[string]bool, len(requested))
	for _, user := range wire.ChatdChildren(list) {
		jid, err := types.ParseJID(user.Attrs["jid"])
		if user.Tag != "user" || err != nil || !requested[jid.ToNonAD().String()] {
			continue
		}
		devices, _ := ChatdChild(user, "devices")
		deviceList, _ := ChatdChild(devices, "device-list")
		for _, device := range wire.ChatdChildren(deviceList) {
			id, err := strconv.ParseUint(device.Attrs["id"], 10, 16)
			if device.Tag != "device" || err != nil || device.Attrs["is_hosted"] == "true" {
				continue
			}
			jid.Device = uint16(id)
			result = append(result, jid.String())
			seen[jid.ToNonAD().String()] = true
		}
	}
	if len(seen) != len(requested) {
		return nil, errors.New("recipient device list is incomplete")
	}
	slices.Sort(result)
	return slices.Compact(result), nil
}

func signalIdentityForDevice(current *state.NativeState, ref string) string {
	jid, err := types.ParseJID(ref)
	if err != nil || jid.Server != types.DefaultUserServer {
		return ref
	}
	for _, hint := range current.ContactHints {
		hint = hint.Normalized()
		if hint.PNJID != jid.ToNonAD().String() || hint.LIDJID == "" {
			continue
		}
		lid, err := types.ParseJID(hint.LIDJID)
		if err == nil && lid.Server == types.HiddenUserServer {
			lid.Device = jid.Device
			return lid.String()
		}
	}
	return ref
}

func ownMessageJIDs(current *state.NativeState) (string, string) {
	pn := wacore.NormalizeWAJID(shared.FirstNonEmpty(current.RegistrationJID, current.CC+current.Phone))
	return pn, signalIdentityForDevice(current, pn)
}
