package communication

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (c *Connection) ResolveContactProfilePicture(ctx context.Context, input wacore.EngineContactProfilePictureInput) wacore.EngineContactProfilePictureResult {
	if c == nil || c.Engine == nil {
		return wacore.EngineContactProfilePictureResult{Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "native engine is required", false)}
	}
	ctx, finish := c.operationContext(ctx)
	defer finish()
	ctx, cancel := context.WithTimeout(ctx, profilePictureTimeout(input))
	defer cancel()
	location, userAgent, err := c.lookupProfilePicture(ctx, input)
	if err != nil {
		return wacore.EngineContactProfilePictureResult{Err: err}
	}
	return c.Engine.downloadProfilePicture(ctx, input.ClientProfileID, location, userAgent)
}

func (c *Connection) lookupProfilePicture(ctx context.Context, input wacore.EngineContactProfilePictureInput) (wire.ContactProfilePictureLocation, string, error) {
	if err := c.LockInteractiveIQLocked(ctx); err != nil {
		return wire.ContactProfilePictureLocation{}, "", err
	}
	defer c.UnlockInteractiveIQLocked()
	return c.Engine.lookupContactProfilePicture(ctx, input, lockedIQSender{c})
}
