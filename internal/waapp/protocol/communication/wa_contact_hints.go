package communication

import (
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func ContactHintsFromNativePayloadMetadata(payload pstate.NativeMessagePayload) []wacore.WAContactHint {
	hints := append([]wacore.WAContactHint{}, payload.ContactHints...)
	appendPayloadHint := func(lidRef string, pnRef string) {
		hint := wacore.WAContactHint{
			LIDJID:      wacore.NormalizeWAJID(lidRef),
			PNJID:       wacore.NormalizeWAJID(pnRef),
			DisplayName: wacore.WAContactName(payload.NotifyName),
			Username:    wacore.WAContactName(payload.ParticipantUsername),
		}
		if hint.Valid() {
			hints = append(hints, hint)
		}
	}
	appendPayloadHint(payload.Sender, payload.SenderPN)
	appendPayloadHint(payload.Contact, payload.ContactPN)
	appendPayloadHint(payload.Sender, payload.ContactPN)
	appendPayloadHint(payload.Contact, payload.SenderPN)
	return pwire.DedupeWAContactHints(hints)
}
