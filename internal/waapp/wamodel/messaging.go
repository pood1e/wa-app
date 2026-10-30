package wamodel

import (
	"strings"

	"github.com/pood1e/wa-app/internal/waapp/shared"
)

// MaxMessageActionBatchSize bounds how many messages a single contact-scoped
// message action (mark-read, delete) processes per call.
const MaxMessageActionBatchSize = 100

func StableOTPMessageID(accountID string, sourceParty string, otp string) string {
	return "waotp_" + shared.StableID(strings.Join([]string{accountID, sourceParty, otp}, ":"))
}
