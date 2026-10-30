package onboarding

import (
	"context"
	"log"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (e *engineCore) RefreshPreChatdABProps(ctx context.Context, phone *waappv1.PhoneTarget, state *pstate.NativeState, appVersion string) {
	if e == nil || state == nil || !state.PreChatdAB.Due(e.clock.Now()) {
		return
	}
	result, err := e.FetchPreChatdABProps(ctx, phone, *state, appVersion)
	now := e.clock.Now()
	if err != nil {
		state.PreChatdAB.ScheduleRetry(now, 5*time.Minute)
		log.Printf("wa_registration_abprop_status status=transport_error retry_after_seconds=%d", int64(5*time.Minute/time.Second))
		return
	}
	state.PreChatdAB.ApplyResponse(result, now)
	summary := NativePreChatdABLogSummary(*state)
	log.Printf(
		"wa_registration_abprop_status status=%s reason=%s has_hash=%t has_exp_cfg=%t retry_after_seconds=%d exp_cfg_count=%d gpia_enabled=%t sim_signal_enabled=%t recaptcha_stage=%s request_id_random=%t key_attestation_observed=%t key_attestation_enabled=%t",
		shared.ProbeLogValue(pstate.ResponseStatus(result)),
		shared.ProbeLogValue(ResponseReason(result)),
		strings.TrimSpace(state.PreChatdAB.Hash) != "",
		strings.TrimSpace(state.PreChatdAB.ExpConfig) != "", pstate.NativeABPropRetryAfterSeconds(result), summary.ConfigCount,
		summary.GPIAEnabled,
		summary.SIMSignalEnabled,
		summary.RecaptchaStage,
		summary.RequestIDRandom,
		summary.KeyAttestationObserved,
		summary.KeyAttestationEnabled,
	)
}

func (e *engineCore) FetchPreChatdABProps(ctx context.Context, phone *waappv1.PhoneTarget, state pstate.NativeState, appVersion string) (map[string]any, error) {
	params := PreChatdABPropParams(phone, state)
	LogNativeRegistrationOrderedShape("abprop", phone, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED, params)
	client, err := e.HttpForProxy()
	if err != nil {
		return nil, err
	}
	return client.PostWASafeNoAuth(ctx, pwire.DefaultWAABPropURL, params.Render(), pstate.NativeUserAgentForState(state, appVersion), e.build.GetServerPublicKey())
}

func PreChatdABPropParams(phone *waappv1.PhoneTarget, state pstate.NativeState) OrderedParams {
	lg, lc := RegistrationLocale(phone)
	params := OrderedParams{}
	params.Set("cc", shared.PhoneCC(phone), false)
	params.Set("in", shared.PhoneNational(phone), false)
	params.Set("lg", lg, false)
	params.Set("lc", lc, false)
	params.Set("fdid", state.Profile.FDID, false)
	params.Set("expid", state.Profile.ExpID, false)
	if state.Profile.AccessSessionID != "" {
		params.Set("access_session_id", state.Profile.AccessSessionID, false)
	}
	ApplyNativeE2EParams(&params, state)
	if hash := strings.TrimSpace(state.PreChatdAB.Hash); hash != "" {
		params.Set("ab_hash", pstate.PctBytes([]byte(hash)), true)
	}
	return params
}
