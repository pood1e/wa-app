package communication

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

// DecodeSignalMessage decodes the upstream E2E message and removes valid Signal padding.
func DecodeSignalMessage(raw []byte) (*waE2E.Message, error) {
	if len(raw) > 0 {
		n := int(raw[len(raw)-1])
		if n > 0 && n <= 16 && n <= len(raw) && bytes.Equal(raw[len(raw)-n:], bytes.Repeat([]byte{byte(n)}, n)) {
			message := new(waE2E.Message)
			if proto.Unmarshal(raw[:len(raw)-n], message) == nil {
				return message, nil
			}
		}
	}
	message := new(waE2E.Message)
	if err := proto.Unmarshal(raw, message); err != nil {
		return nil, err
	}
	return message, nil
}

// MessagePlaintextRef binds cached plaintext to its profile, transport attempt and payload metadata.
func MessagePlaintextRef(input wacore.EngineDecryptInput, payload state.NativeMessagePayload) string {
	return "native-plain:" + shared.StableID(strings.Join([]string{
		input.ClientProfileID, input.MessageID, input.PayloadRef, payload.Contact, payload.Sender, payload.EncType,
	}, "|"))
}

func cachedMessagePlaintext(current *state.NativeState, input wacore.EngineDecryptInput, ref string) ([]byte, bool, error) {
	if input.MessageID == "" {
		return nil, false, nil
	}
	encoded, exists := current.MessagePlainRef[ref]
	if !exists {
		return nil, false, nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	return raw, true, err
}

func applySignalControlMessage(ctx context.Context, current *state.NativeState, sender string, raw []byte) error {
	message, err := DecodeSignalMessage(raw)
	if err != nil {
		return nil
	}
	if distribution := message.GetSenderKeyDistributionMessage(); distribution != nil {
		return processGroupDistribution(ctx, current, distribution.GetGroupID(), sender, distribution.GetAxolotlSenderKeyDistributionMessage())
	}
	return nil
}
