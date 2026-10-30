package communication

import (
	"context"

	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (c *Connection) CheckLoginState(ctx context.Context, input wacore.EngineLoginCheckInput) wacore.EngineLoginCheckResult {
	ctx, finish := c.operationContext(ctx)
	defer finish()
	if err := c.LockInteractiveIQLocked(ctx); err != nil {
		return wacore.EngineLoginCheckResult{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE, Err: err}
	}
	defer c.UnlockInteractiveIQLocked()
	session, err := c.EnsureSessionWithTimeoutLocked(ctx, c.Input)
	if err != nil {
		return wacore.EngineLoginCheckResult{Status: LoginCheckStatusForError(err), Err: ChatdReceiveError(err)}
	}
	response, items, update, err := session.SendIQ(ctx, c.Input, wire.BuildPingNode(),
		ContextBoundTimeout(ctx, TextMessageSendTimeout(input.RemoteTimeout)), "login heartbeat response timed out")
	c.BufferPendingLocked(items, update)
	if err == nil {
		err = ChatdIQError(response)
	}
	if err != nil {
		c.closeFailedSessionLocked()
		return wacore.EngineLoginCheckResult{Status: LoginCheckStatusForError(err), Err: ChatdReceiveError(err)}
	}
	return wacore.EngineLoginCheckResult{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE}
}

func (c *Connection) DecryptMessage(ctx context.Context, input wacore.EngineDecryptInput) wacore.EngineDecryptResult {
	ctx, finish := c.operationContext(ctx)
	defer finish()
	if err := c.LockInteractiveIQLocked(ctx); err != nil {
		return wacore.EngineDecryptResult{Err: err}
	}
	defer c.UnlockInteractiveIQLocked()
	result := c.Engine.DecryptMessage(ctx, input)
	if retryableDecryptionFailure(result.Err) && input.SessionCommitPolicy == waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE {
		refreshed, err := c.refreshSenderIdentity(ctx, input)
		if err != nil {
			result.Err = shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_DECRYPTION_FAILED, "peer identity refresh failed", true)
		} else if refreshed {
			result = c.Engine.DecryptMessage(ctx, input)
		}
		if retryableDecryptionFailure(result.Err) {
			if err := c.requestSignalRetry(ctx, input); err != nil {
				result.Err = shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_DECRYPTION_FAILED, "message retry request could not be sent", true)
			}
		}
	}
	return result
}
