package account

import (
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func TestMessageChangeKeyRetainsOnlyApplicableParticipant(t *testing.T) {
	for _, test := range []struct {
		name, chat, participant string
		direction               waappv1.AccountMessageDirection
	}{
		{"incoming direct", "100001@lid", "", waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND},
		{"outgoing direct", "100001@lid", "", waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND},
		{"incoming group", "120363000001@g.us", "100001@lid", waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND},
		{"outgoing group", "120363000001@g.us", "", waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND},
	} {
		t.Run(test.name, func(t *testing.T) {
			key := messageChangeKey(&waappv1.AccountMessage{ContactRef: test.chat, SenderRef: "100001:3@lid", ProviderMessageId: "original", Direction: test.direction})
			if key.GetRemoteJID() != test.chat || key.GetID() != "original" || key.FromMe == nil || key.GetFromMe() != (test.direction == waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND) || key.GetParticipant() != test.participant {
				t.Fatal("message change confused the target's chat, direction or group sender")
			}
		})
	}
}
