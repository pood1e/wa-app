package wire

import (
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func SensitiveOutput(value string, refPrefix string, include bool) *waappv1.SensitiveText {
	out := &waappv1.SensitiveText{RedactedValue: shared.Redacted(value), SecretRef: refPrefix + ":" + shared.StableID(value)}
	if include {
		out.Value = value
	}
	return out
}
