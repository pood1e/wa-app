package onboarding

import (
	"context"
	"strings"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

// registrationService owns the account probe / verification / login flows.
type registrationService struct{ *engineCore }

func (e *registrationService) ProbeAccountWithState(ctx context.Context, input wacore.EngineRegistrationInput, state pstate.NativeState) (wacore.EngineProbeResult, pstate.NativeState) {
	built, err := e.ExistOrderedParamsWithWamsys(ctx, input.Phone, state, nil, true, input.AppVersion)
	if err != nil {
		return wacore.EngineProbeResult{Status: waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED, Err: err}, state
	}
	LogNativeRegistrationOrderedShape("exist", input.Phone, input.DeliveryMethod, built)
	plain := built.Render()
	client, err := e.HttpForProxy()
	if err != nil {
		return wacore.EngineProbeResult{Status: waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED, Err: err}, state
	}
	// /v2/exist 幂等;纯传输层抖动(代理 EOF/重置/超时,响应无任何可解析的应用层结论)
	// 时重试若干次,避免单次代理失败被判成 probe_failed —— 那会让前端对同一号码"通道
	// 忽有忽无"。拿到成功响应、真正的应用层结论(含协议拒绝)或 ctx 取消即停。
	var data map[string]any
	var result wacore.EngineProbeResult
	for attempt := 1; attempt <= pstate.ExistProbeTransientAttempts; attempt++ {
		data, _, err = client.PostWASafe(ctx, pwire.DefaultWAExistURL, plain, pstate.NativeUserAgentForState(state, input.AppVersion), e.build.GetServerPublicKey())
		result = ParseExistProbeResult(data)
		if err == nil || result.Err != nil || ParsedExistApplicationOutcome(result) || ctx.Err() != nil {
			break
		}
	}
	if err != nil {
		if result.Err != nil || ParsedExistApplicationOutcome(result) {
			return result, state
		}
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED
		result.AccountFlow = AccountProbeFlowProbeFailed
		result.Err = ClassifyHTTPError(data, err)
	}
	return result, state
}

func ParsedExistApplicationOutcome(result wacore.EngineProbeResult) bool {
	return result.Status != waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_UNKNOWN ||
		result.AccountFlow != AccountProbeFlowUnknown ||
		result.RawStatus != "" ||
		result.RawReason != "" ||
		len(result.MethodStatuses) > 0
}

func (e *registrationService) RequestVerificationCode(ctx context.Context, input wacore.EngineRegistrationInput) wacore.EngineCodeResult {
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: err}
	}
	result, updated := e.RequestVerificationCodeWithState(ctx, input, state)
	_ = e.SaveState(ctx, input.ClientProfileID, updated)
	return result
}

func (e *registrationService) RequestVerificationCodeWithState(ctx context.Context, input wacore.EngineRegistrationInput, state pstate.NativeState) (wacore.EngineCodeResult, pstate.NativeState) {
	if input.DeliveryMethod == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
		return wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: unsupportedQRTransfer()}, state
	}
	state.NextGenerateCodeAttempt()
	params, err := e.CodeRequestOrderedParams(ctx, input.Phone, input.DeliveryMethod, state, input.AuthCodeContext, input.AppVersion)
	if err != nil {
		return wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: err}, state
	}
	LogNativeRegistrationOrderedShape("code", input.Phone, input.DeliveryMethod, params)
	plain := params.Render()
	client, err := e.HttpForProxy()
	if err != nil {
		return wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: err}, state
	}
	data, enc, err := client.PostWASafe(ctx, pwire.DefaultWACodeURL, plain, pstate.NativeUserAgentForState(state, input.AppVersion), e.build.GetServerPublicKey())
	state.LastCodeParams = params.ToMap()
	state.LastCodeResult = SanitizeResponse(data)
	e.verification = nil
	if err == nil && (pstate.ResponseStatus(data) == "sent" || pstate.ResponseStatus(data) == "ok") {
		e.verificationMethod = input.DeliveryMethod
		e.verification = &protocolv1.AndroidVerificationContext{
			Login: pstate.JsonString(data["login"]), AuthResponse: pstate.JsonString(data["auth_response"]),
			Context: pstate.JsonString(data["context"]), AdvertisingId: pstate.JsonString(data["advertising_id"]),
		}
	}
	if enc != "" {
		state.LastCodeResult["enc_sha256"] = pwire.EncHash(enc)
	}
	retryAfter := VerificationCodeRetryAfter(data, input.DeliveryMethod)
	now := e.clock.Now()
	if err != nil {
		if VerificationCodeRateLimited(data) {
			return VerificationCodeRejectedResult(data, input.DeliveryMethod, now, retryAfter, "verification request is cooling down"), state
		}
		return wacore.EngineCodeResult{
			Status:         waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED,
			RetryAfter:     retryAfter,
			MethodStatuses: VerificationCodeMethodStatuses(data, input.DeliveryMethod),
			RawStatus:      pstate.ResponseStatus(data),
			RawReason:      ResponseReason(data),
			Err:            ClassifyHTTPError(data, err),
		}, state
	}
	s := pstate.ResponseStatus(data)
	if s == "sent" || s == "ok" {
		if returned := pstate.JsonString(data["method"]); returned != "" && returned != RegistrationMethodName(input.DeliveryMethod, "sms") {
			e.verification = nil
			return wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED,
				RawStatus: s, RetryAfter: retryAfter, MethodStatuses: VerificationCodeMethodStatuses(data, input.DeliveryMethod),
				Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "verification response changed the requested delivery method", false)}, state
		}
	} else if VerificationCodeRateLimited(data) {
		return VerificationCodeRejectedResult(data, input.DeliveryMethod, now, retryAfter, "verification request is cooling down"), state
	} else if s != "" {
		return wacore.EngineCodeResult{
			Status:         waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED,
			RetryAfter:     retryAfter,
			MethodStatuses: VerificationCodeMethodStatuses(data, input.DeliveryMethod),
			RawStatus:      pstate.ResponseStatus(data),
			RawReason:      ResponseReason(data),
			Err:            WaProtocolError(data, "verification request was rejected"),
		}, state
	} else {
		return wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED,
			RetryAfter: retryAfter, Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE,
				"verification response did not confirm code delivery; keep this flow and do not submit an old code", true)}, state
	}
	return VerificationCodeResult(waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT, data, input.DeliveryMethod, now, retryAfter), state
}

func (e *registrationService) SubmitVerificationCode(ctx context.Context, input wacore.EngineSubmitInput) wacore.EngineRegisterResult {
	if e.passwordRequired {
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_PASSWORD, PasswordChallenge: e.passwordChallenge()}
	}
	if input.DeliveryMethod == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: unsupportedQRTransfer()}
	}
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: err}
	}
	if e.verification == nil || state.LastCodeParams["method"] != RegistrationMethodName(input.DeliveryMethod, "sms") {
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED,
			Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "registration requires a confirmed code request through the same delivery method", false)}
	}
	code := strings.TrimSpace(input.Code)
	if code == "" {
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "verification code is required", false)}
	}
	params, err := e.RegisterOrderedParams(ctx, input.Phone, input.DeliveryMethod, code, state, input.AuthCodeContext, input.AppVersion)
	if err != nil {
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: err}
	}
	LogNativeRegistrationOrderedShape("register", input.Phone, input.DeliveryMethod, params)
	plain := params.Render()
	client, err := e.HttpForProxy()
	if err != nil {
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: err}
	}
	data, enc, err := client.PostWASafe(ctx, pwire.DefaultWARegisterURL, plain, pstate.NativeUserAgentForState(state, input.AppVersion), e.build.GetServerPublicKey())
	return e.registerResult(ctx, input.EngineRegistrationInput, state, data, enc, err)
}

func (e *registrationService) registerResult(ctx context.Context, input wacore.EngineRegistrationInput, state pstate.NativeState, data map[string]any, enc string, err error) wacore.EngineRegisterResult {
	state.LastRegister = SanitizeResponse(data)
	if routingInfo := pstate.ChatRoutingInfoFromValue(data["edge_routing_info"]); routingInfo != "" {
		state.ChatRoutingInfo = routingInfo
	}
	if enc != "" {
		state.LastRegister["enc_sha256"] = pwire.EncHash(enc)
	}
	if err != nil {
		_ = e.SaveState(ctx, input.ClientProfileID, state)
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: ClassifyHTTPError(data, err), OutcomeUnknown: pstate.ResponseStatus(data) == ""}
	}
	if status := pstate.ResponseStatus(data); status != "ok" && status != "registered" {
		_ = e.SaveState(ctx, input.ClientProfileID, state)
		if status == "" {
			return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, OutcomeUnknown: true,
				Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "registration response did not confirm its outcome", true)}
		}
		if ResponseReason(data) == "security_code" {
			e.passwordRequired = true
			if wait := FirstJSONWaitStatus(data["guess_wait"]).Seconds; wait > 0 {
				e.passwordRetryAt = e.clock.Now().Add(time.Duration(wait) * time.Second)
			}
			return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_PASSWORD, PasswordChallenge: e.passwordChallenge()}
		}
		if reason := ResponseReason(data); reason == "missing" || reason == "stale" {
			e.verification = nil
			e.passwordRequired = false
			return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_PENDING_CODE,
				RetryAfter: VerificationCodeRetryAfter(data, input.DeliveryMethod),
				Err:        shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VERIFICATION_REQUIRED, "verification code must be requested again: reason="+reason, true)}
		}
		return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: WaProtocolError(data, "registration was rejected")}
	}
	e.passwordRequired = false
	login := shared.FirstNonEmpty(pstate.JsonString(data["login"]), pstate.JsonString(data["jid"]), pstate.JsonString(data["registration_jid"]), state.CC+state.Phone)
	lid := shared.FirstNonEmpty(pstate.JsonString(data["lid"]), login)
	if login != "" {
		state.RegistrationJID = pstate.NormalizeJID(login)
	}
	_ = e.SaveState(ctx, input.ClientProfileID, state)
	completedAt := e.clock.Now()
	return wacore.EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED, RegisteredID: "waid_" + shared.StableID(login), ServiceAccountID: lid, ServiceLoginID: login, CompletedAt: completedAt}
}

func RetryableRegisterHTTPFailure(data map[string]any, err error) bool {
	if err == nil {
		return false
	}
	statusCode := int(pstate.JsonNumber(data["status_code"]))
	if statusCode == 429 || statusCode >= 500 {
		return true
	}
	if statusCode > 0 {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"timeout", "deadline", "temporary", "connection reset", "connection refused", "eof", "proxy", "network"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
