package communication

import (
	"context"
	"encoding/json"

	"github.com/pood1e/wa-app/internal/waapp/protocol/security"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (s *connectionState) pairingSecurity(ctx context.Context) (*security.Client, error) {
	c := s.connection
	input := wacore.EngineAccountSettingsInput{WAAccountID: c.Input.WAAccountID, ClientProfileID: c.Input.ClientProfileID,
		RegisteredIdentityID: c.Input.RegisteredIdentityID, AppVersion: c.Input.AppVersion}
	sender := lockedIQSender{c}
	client := security.New(func(ctx context.Context, op security.Operation, variables any) (json.RawMessage, error) {
		return c.accountSecurityMEX(ctx, input, &s.current, sender, op, variables)
	}, nil, s.current.Client, c.clock.Now, sender.WaitCanonicalNonce)
	credentials, err := client.Credentials(ctx, s.current.Canonical)
	if err != nil {
		return nil, err
	}
	if credentials != s.current.Canonical {
		s.current.Canonical = credentials
		if err := c.SaveState(ctx, input.ClientProfileID, s.current); err != nil {
			return nil, err
		}
	}
	return client, nil
}
