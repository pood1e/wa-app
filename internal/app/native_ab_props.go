package app

import (
	"context"
	"log"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type nativePreChatdABState struct {
	Hash                 string `json:"hash,omitempty"`
	Key                  string `json:"key,omitempty"`
	ExpConfig            string `json:"exp_config,omitempty"`
	NextFetchAtUnixMilli int64  `json:"next_fetch_at_unix_ms,omitempty"`
}

const nativeABPropSuccessRefreshDelay = time.Minute

func (e *NativeEngine) refreshPreChatdABProps(ctx context.Context, phone *waappv1.PhoneTarget, state *nativeState, appVersion string) {
	if e == nil || state == nil || !state.PreChatdAB.due(e.clock.Now()) {
		return
	}
	result, err := e.fetchPreChatdABProps(ctx, phone, *state, appVersion)
	now := e.clock.Now()
	if err != nil {
		state.PreChatdAB.scheduleRetry(now, 5*time.Minute)
		log.Printf("wa_registration_abprop_status status=transport_error retry_after_seconds=%d", int64(5*time.Minute/time.Second))
		return
	}
	state.PreChatdAB.applyResponse(result, now)
	summary := nativePreChatdABLogSummary(*state)
	log.Printf(
		"wa_registration_abprop_status status=%s reason=%s has_hash=%t has_exp_cfg=%t retry_after_seconds=%d exp_cfg_count=%d gpia_enabled=%t sim_signal_enabled=%t recaptcha_stage=%s request_id_random=%t",
		probeLogValue(responseStatus(result)),
		probeLogValue(responseReason(result)),
		strings.TrimSpace(state.PreChatdAB.Hash) != "",
		strings.TrimSpace(state.PreChatdAB.ExpConfig) != "",
		nativeABPropRetryAfterSeconds(result),
		summary.ConfigCount,
		summary.GPIAEnabled,
		summary.SIMSignalEnabled,
		summary.RecaptchaStage,
		summary.RequestIDRandom,
	)
}

func (s nativePreChatdABState) due(now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	return s.NextFetchAtUnixMilli <= 0 || now.UTC().UnixMilli() >= s.NextFetchAtUnixMilli
}

func (s *nativePreChatdABState) scheduleRetry(now time.Time, delay time.Duration) {
	if s == nil || delay <= 0 {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.NextFetchAtUnixMilli = now.UTC().Add(delay).UnixMilli()
}

func (s *nativePreChatdABState) applyResponse(data map[string]any, now time.Time) {
	if s == nil || data == nil {
		return
	}
	if hash := strings.TrimSpace(jsonString(data["ab_hash"])); hash != "" {
		s.Hash = hash
	}
	if key := strings.TrimSpace(jsonString(data["ab_key"])); key != "" {
		s.Key = key
	}
	if expConfig := strings.TrimSpace(jsonString(data["exp_cfg"])); expConfig != "" && expConfig != "wamsys initialization fails" {
		s.ExpConfig = expConfig
	}
	if seconds := nativeABPropRetryAfterSeconds(data); seconds > 0 {
		s.scheduleRetry(now, time.Duration(seconds)*time.Second)
		return
	}
	if responseStatus(data) == "ok" {
		s.scheduleRetry(now, nativeABPropSuccessRefreshDelay)
	}
}

func nativeABPropRetryAfterSeconds(data map[string]any) int64 {
	if data == nil {
		return 0
	}
	return jsonInt64(data["retry_after"])
}

func (e *NativeEngine) fetchPreChatdABProps(ctx context.Context, phone *waappv1.PhoneTarget, state nativeState, appVersion string) (map[string]any, error) {
	params := preChatdABPropParams(phone, state)
	logNativeRegistrationOrderedShape("abprop", phone, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED, params)
	client, err := e.httpForProxy()
	if err != nil {
		return nil, err
	}
	return client.postWASafeNoAuth(ctx, defaultWAABPropURL, params.render(), nativeUserAgentForState(state, appVersion))
}

func preChatdABPropParams(phone *waappv1.PhoneTarget, state nativeState) orderedParams {
	lg, lc := registrationLocale(phone)
	params := orderedParams{}
	params.set("cc", phoneCC(phone), false)
	params.set("in", phoneNational(phone), false)
	params.set("lg", lg, false)
	params.set("lc", lc, false)
	params.set("fdid", state.Profile.FDID, false)
	params.set("expid", state.Profile.ExpID, false)
	if state.Profile.AccessSessionID != "" {
		params.set("access_session_id", state.Profile.AccessSessionID, false)
	}
	applyNativeE2EParams(&params, state)
	if hash := strings.TrimSpace(state.PreChatdAB.Hash); hash != "" {
		params.set("ab_hash", pctBytes([]byte(hash)), true)
	}
	return params
}
