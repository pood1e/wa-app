package communication

import (
	"slices"
	"testing"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func TestRecipientDeviceDiscoveryRequiresExplicitCompleteLists(t *testing.T) {
	response := wire.ChatdNode{Tag: "iq", Content: []wire.ChatdNode{{Tag: "usync", Content: []wire.ChatdNode{{Tag: "list", Content: []wire.ChatdNode{{
		Tag: "user", Attrs: map[string]string{"jid": "12345@s.whatsapp.net"}, Content: []wire.ChatdNode{{Tag: "devices", Content: []wire.ChatdNode{{Tag: "device-list", Content: []wire.ChatdNode{
			{Tag: "device", Attrs: map[string]string{"id": "0"}},
			{Tag: "device", Attrs: map[string]string{"id": "3"}},
			{Tag: "device", Attrs: map[string]string{"id": "3"}},
			{Tag: "device", Attrs: map[string]string{"id": "2", "is_hosted": "true"}},
			{Tag: "device", Attrs: map[string]string{"id": "65536"}},
		}}}}},
	}}}}}}}
	got, err := devicesFromUsync(response, map[string]bool{"12345@s.whatsapp.net": true})
	if err != nil || !slices.Equal(got, []string{"12345:3@s.whatsapp.net", "12345@s.whatsapp.net"}) {
		t.Fatalf("devices = %v, err = %v", got, err)
	}
	if _, err := devicesFromUsync(response, map[string]bool{"12345@s.whatsapp.net": true, "67890@s.whatsapp.net": true}); err == nil {
		t.Fatal("incomplete device discovery silently defaulted to a primary device")
	}
}

func TestLIDMappingPreservesDeviceAcrossRecovery(t *testing.T) {
	current := newMessageParty(t)
	current.RegistrationJID = "12345@s.whatsapp.net"
	ApplyChatdSessionUpdateState(&current, ChatdSessionUpdate{ContactHints: []wacore.WAContactHint{{PNJID: "12345@s.whatsapp.net", LIDJID: "987654@lid"}}})
	current = reloadMessageParty(t, current)
	if got := signalIdentityForDevice(&current, "12345:3@s.whatsapp.net"); got != "987654:3@lid" {
		t.Fatalf("mapped identity = %s", got)
	}
	pn, lid := ownMessageJIDs(&current)
	if pn != current.RegistrationJID || lid != "987654@lid" || current.CC != "" || current.Phone != "" {
		t.Fatal("restored runtime identity depends on discarded registration material")
	}
	unknown := &state.NativeState{}
	if got := signalIdentityForDevice(unknown, "777:2@s.whatsapp.net"); got != "777:2@s.whatsapp.net" {
		t.Fatal("an unknown LID mapping was invented")
	}
}
