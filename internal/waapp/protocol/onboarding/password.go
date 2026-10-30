package onboarding

import (
	"context"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/security"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (e *engineCore) passwordChallenge() *waappv1.AccountAddPasswordChallenge {
	return &waappv1.AccountAddPasswordChallenge{NextAttemptAt: shared.ProtoTimestamp(e.passwordRetryAt)}
}

// SubmitPassword continues confirmed first-factor verification with its retained keys.
func (e *AndroidFlow) SubmitPassword(ctx context.Context, password string) wacore.EngineRegisterResult {
	rejected := func(err error) wacore.EngineRegisterResult {
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: err}
	}
	if !e.passwordRequired || e.verification == nil {
		return rejected(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "registration is not waiting for a password", false))
	}
	if e.clock.Now().Before(e.passwordRetryAt) {
		return rejected(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, "two-step verification is cooling down", true))
	}
	client, err := e.HttpForProxy()
	if err != nil {
		return rejected(err)
	}
	key, err := security.New(nil, client.Client, e.current.Client, e.clock.Now, nil).RegistrationPasswordEncryptionKey(ctx)
	if err != nil {
		return rejected(err)
	}
	sealed, err := security.SealPassword(password, key, e.clock.Now())
	if err != nil {
		return rejected(err)
	}
	input := e.input(e.verificationMethod)
	params, err := e.RegisterOrderedParams(ctx, e.phone, input.DeliveryMethod, sealed, e.current, "twofac_dynamic", input.AppVersion)
	if err != nil {
		return rejected(err)
	}
	// KotlinRegistrationBridge.A0R omits user type for security verification.
	params.Remove("type")
	params.Remove("auth_response")
	params.Set("method", "password", false)
	params.Set("context", "twofac_dynamic", false)
	data, enc, err := client.PostWASafe(ctx, wire.DefaultWARegisterURL, params.Render(), state.NativeUserAgentForState(e.current, input.AppVersion), e.build.GetServerPublicKey())
	result := e.registerResult(ctx, input, e.current, data, enc, err)
	wait := max(FirstJSONWaitStatus(data["guess_wait"]).Seconds, FirstJSONWaitStatus(data["retry_after"]).Seconds)
	if e.passwordRequired {
		if wait > 0 {
			e.passwordRetryAt = e.clock.Now().Add(time.Duration(wait) * time.Second)
		}
		result.PasswordChallenge = e.passwordChallenge()
	}
	return result
}
