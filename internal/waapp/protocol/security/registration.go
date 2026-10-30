package security

import (
	"context"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

// RegistrationPasswordEncryptionKey fetches public material with native application authorization.
func (c *Client) RegistrationPasswordEncryptionKey(ctx context.Context) (*protocolv1.PasswordEncryptionKey, error) {
	return c.PasswordEncryptionKey(ctx, "WA|1015890928915437|3201f239340c1c8ec6262a6dad04200e")
}
