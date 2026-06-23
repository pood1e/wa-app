package app

import (
	"context"
	"log"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *Server) StartRegistration(ctx context.Context, payload map[string]any) (map[string]any, error) {
	if s == nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "wa-app service is not configured", false)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	gateway := &actionGateway{server: s}
	basePayload := cloneActionPayload(payload)
	basePayload["purpose"] = firstNonEmpty(textField(basePayload, "purpose"), "WA_REGISTRATION")
	basePayload["proxy_session_mode"] = firstNonEmpty(textField(basePayload, "proxy_session_mode"), "STICKY")
	method := registrationMethodFromPayload(basePayload)
	authCodeContext := authCodeContextFromPayload(basePayload)
	if reason := directRegistrationMethodUnsupportedReason(method); reason != "" {
		return rejectedRegistrationResult(basePayload, registrationMethodUnsupportedMap(method, reason)), nil
	}
	phone := normalizePhone(phoneFromAction(basePayload))
	state, stateRef, reusedState, err := gateway.registrationAttemptState(ctx, phone)
	if err != nil {
		return nil, err
	}
	logRegistrationAttemptState(basePayload, phone, reusedState)
	runner, route, managedRoute, err := gateway.registrationRunner(basePayload)
	if err != nil {
		return nil, err
	}
	defer runner.CloseIdleConnections()
	probeResult, state := runner.probeAccountWithState(ctx, EngineRegistrationInput{AppVersion: defaultWAAppVersion, Phone: phone, DeliveryMethod: method, AuthCodeContext: authCodeContext}, state)
	_ = gateway.saveRegistrationAttemptState(context.Background(), stateRef, state)
	logRegistrationProbeResult(basePayload, phone, route, method, probeResult)
	if !registrationProbeAllowsMethod(probeResult, method) {
		return rejectedRegistrationResult(basePayload, registrationProbeFailureMap(probeResult, route, managedRoute)), nil
	}
	codeResult, updatedState := runner.requestVerificationCodeWithState(ctx, EngineRegistrationInput{AppVersion: defaultWAAppVersion, Phone: phone, DeliveryMethod: method, AuthCodeContext: authCodeContext}, state)
	_ = gateway.saveRegistrationAttemptState(context.Background(), stateRef, updatedState)
	logRegistrationCodeResult(basePayload, phone, route, method, codeResult)
	if !verificationCodeRequestAccepted(codeResult) {
		return rejectedRegistrationResult(basePayload, registrationRequestFailureMap(codeResult, method, route, managedRoute)), nil
	}
	account, profile, protocol, err := gateway.server.commitNativeState(ctx, phone, updatedState)
	if err != nil {
		return nil, err
	}
	record := gateway.server.newVerificationCodeRequestRecord(account, profile, method, codeResult)
	challenge := codeResult.AccountTransferChallenge
	if challenge != nil {
		challenge.VerificationRequestId = record.GetVerificationRequestId()
	}
	if err := gateway.server.store.SaveVerificationRequest(ctx, record); err != nil {
		_ = gateway.discardRejectedRegistration(context.Background(), basePayload, waAccountID(account), record.GetVerificationRequestId())
		return nil, err
	}
	verificationRequestID := record.GetVerificationRequestId()
	wait := registrationOTPWait{
		WAAccountID:           waAccountID(account),
		VerificationRequestID: verificationRequestID,
		CreatedAtUnix:         time.Now().UTC().Unix(),
	}
	if err := gateway.saveRegistrationOTPWait(ctx, wait, registrationOTPWaitDefaultTTL); err != nil {
		_ = gateway.discardRejectedRegistration(context.Background(), basePayload, waAccountID(account), verificationRequestID)
		return nil, err
	}
	_ = gateway.server.runtime.DeleteTransientState(context.Background(), stateRef)
	response := map[string]any{
		"success":                 true,
		"status":                  record.GetStatus().String(),
		"error_message":           "",
		"phone":                   objectField(basePayload, "phone"),
		"wa_account_id":           waAccountID(account),
		"client_profile_id":       profile.GetClientProfileId(),
		"protocol_profile_id":     protocol.GetProtocolProfileId(),
		"verification_request_id": verificationRequestID,
		"verification_request":    protoMap(record),
		"delivery_method":         method.String(),
		"method":                  registrationMethodName(method, "sms"),
		"method_statuses":         methodStatusMaps(codeResult.MethodStatuses),
		"registration_phase":      registrationPhase(true, verificationRequestID, durationFromProto(record.GetRetryAfter())),
		"fingerprint_persistence": "COMMITTED",
		"persisted":               true,
		"phone_status":            registrationCodeResultPhoneStatus(codeResult, method, false),
		"proxy":                   registrationOrchestratorProxySummary(registrationProxyRouteMap(route, managedRoute)),
	}
	if challenge != nil {
		response["account_transfer_challenge"] = protoMap(challenge)
		response["registration_phase"] = "ACCOUNT_TRANSFER_WAITING"
	}
	if seconds := durationSeconds(record.GetRetryAfter()); seconds > 0 {
		response["retry_after_seconds"] = seconds
	}
	return response, nil
}

func (g *actionGateway) registrationAttemptState(ctx context.Context, phone *waappv1.PhoneTarget) (nativeState, string, bool, error) {
	ref := registrationAttemptStateKey(phone)
	if data, err := g.server.runtime.GetTransientState(ctx, ref); err == nil {
		state, err := unmarshalNativeState(data)
		if err == nil {
			return state, ref, true, nil
		}
		_ = g.server.runtime.DeleteTransientState(ctx, ref)
	}
	engine, err := g.nativeEngine()
	if err != nil {
		return nativeState{}, "", false, err
	}
	state, err := engine.newState(phone)
	if err != nil {
		return nativeState{}, "", false, err
	}
	if err := g.saveRegistrationAttemptState(ctx, ref, state); err != nil {
		return nativeState{}, "", false, err
	}
	return state, ref, false, nil
}

func (g *actionGateway) saveRegistrationAttemptState(ctx context.Context, ref string, state nativeState) error {
	data, err := marshalNativeState(state)
	if err != nil {
		return err
	}
	return g.server.runtime.SaveTransientState(ctx, ref, data, registrationAttemptStateTTL)
}

func registrationAttemptStateKey(phone *waappv1.PhoneTarget) string {
	return "wa-register-state:" + stableID(firstNonEmpty(phone.GetE164Number(), fullPhoneKey(phoneCC(phone), phoneNational(phone))))
}

func logRegistrationAttemptState(payload map[string]any, phone *waappv1.PhoneTarget, reused bool) {
	phoneHash := ""
	if phone != nil && phone.GetE164Number() != "" {
		phoneHash = stableID(phone.GetE164Number())
	}
	log.Printf(
		"wa_registration_attempt_state correlation=%s phone_hash=%s reused=%t ttl_seconds=%d",
		probeLogValue(actionContext(payload).GetCorrelationId()),
		phoneHash,
		reused,
		int64(registrationAttemptStateTTL/time.Second),
	)
}

func logRegistrationCodeResult(payload map[string]any, phone *waappv1.PhoneTarget, route WAProxyRoute, method waappv1.VerificationDeliveryMethod, result EngineCodeResult) {
	phoneHash := ""
	if phone != nil && phone.GetE164Number() != "" {
		phoneHash = stableID(phone.GetE164Number())
	}
	protoErr := ToProtoError(result.Err)
	log.Printf(
		"wa_registration_code_result correlation=%s phone_hash=%s proxy_account=%s route_id=%s accepted=%t method=%s status=%s raw_status=%s raw_reason=%s retry_after_seconds=%d method_status_count=%d error=%s",
		probeLogValue(actionContext(payload).GetCorrelationId()),
		phoneHash,
		probeLogValue(route.AccountID),
		probeLogValue(route.RouteID),
		verificationCodeRequestAccepted(result),
		probeLogValue(registrationMethodName(method, "sms")),
		probeLogValue(result.Status.String()),
		probeLogValue(result.RawStatus),
		probeLogValue(result.RawReason),
		int64(result.RetryAfter/time.Second),
		len(result.MethodStatuses),
		probeLogValue(protoErr.GetMessage()),
	)
}

func logRegistrationProbeResult(payload map[string]any, phone *waappv1.PhoneTarget, route WAProxyRoute, method waappv1.VerificationDeliveryMethod, result EngineProbeResult) {
	phoneHash := ""
	if phone != nil && phone.GetE164Number() != "" {
		phoneHash = stableID(phone.GetE164Number())
	}
	protoErr := ToProtoError(result.Err)
	log.Printf(
		"wa_registration_probe_result correlation=%s phone_hash=%s proxy_account=%s route_id=%s allowed=%t method=%s account_flow=%s account_status=%s raw_status=%s raw_reason=%s sms_available=%t sms_wait_seconds=%d method_status_count=%d error=%s",
		probeLogValue(actionContext(payload).GetCorrelationId()),
		phoneHash,
		probeLogValue(route.AccountID),
		probeLogValue(route.RouteID),
		registrationProbeAllowsMethod(result, method),
		probeLogValue(registrationMethodName(method, "sms")),
		probeLogValue(result.AccountFlow),
		probeLogValue(result.Status.String()),
		probeLogValue(result.RawStatus),
		probeLogValue(result.RawReason),
		result.CanSendSMS,
		result.SMSWaitSeconds,
		len(result.MethodStatuses),
		probeLogValue(protoErr.GetMessage()),
	)
}

func authCodeContextFromPayload(payload map[string]any) string {
	return firstNonEmpty(
		textField(payload, "auth_code_context"),
		textField(payload, "authCodeContext"),
		textField(payload, "code_entrypoint"),
	)
}

func registrationMethodFromPayload(payload map[string]any) waappv1.VerificationDeliveryMethod {
	method := registrationMethodFromName(firstNonEmpty(
		textField(payload, "delivery_method"),
		textField(payload, "verification_method"),
		textField(payload, "method"),
	))
	if method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS
	}
	return method
}

func directRegistrationMethodUnsupportedReason(method waappv1.VerificationDeliveryMethod) string {
	switch method {
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_FLASH:
		return "flash call verification requires Android missed-call/call-log runtime"
	default:
		return ""
	}
}

func registrationMethodUnsupportedMap(method waappv1.VerificationDeliveryMethod, reason string) map[string]any {
	return map[string]any{
		"success":        false,
		"request_failed": true,
		"status":         "REGISTRATION_METHOD_UNSUPPORTED",
		"error_message":  reason,
		"reject_reason":  reason,
		"phone_status": map[string]any{
			"account_status":      waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED.String(),
			"account_flow":        accountProbeFlowProbeFailed,
			"account_reachable":   false,
			"request_failed":      true,
			"sms_available":       false,
			"can_register":        false,
			"delivery_method":     method.String(),
			"registration_method": registrationMethodName(method, ""),
			"reject_reason":       reason,
		},
	}
}

func cloneActionPayload(payload map[string]any) map[string]any {
	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func registrationPhase(success bool, verificationRequestID string, retryAfter time.Duration) string {
	if !success || strings.TrimSpace(verificationRequestID) == "" {
		return "OTP_REQUEST_FAILED"
	}
	if retryAfter > 0 {
		return "OTP_COOLDOWN"
	}
	return "OTP_WAITING"
}

func verificationCodeRequestAccepted(result EngineCodeResult) bool {
	return result.Err == nil && (result.Status == waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT || result.Status == waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_WAITING)
}

func registrationProbeAllowsMethod(result EngineProbeResult, method waappv1.VerificationDeliveryMethod) bool {
	if result.Err != nil || result.Blocked || result.AccountFlow == accountProbeFlowInvalidNumber {
		return false
	}
	if method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED ||
		method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS {
		return registrationProbeMethodAvailable(result, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS) || result.CanSendSMS
	}
	if method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
		return result.Status == waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REACHABLE &&
			(result.Registered || registrationProbeMethodAvailable(result, method) || registrationProbeMethodAvailable(result, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD))
	}
	return registrationProbeMethodAvailable(result, method)
}

func registrationProbeMethodAvailable(result EngineProbeResult, method waappv1.VerificationDeliveryMethod) bool {
	for _, status := range result.MethodStatuses {
		if status.Method == method {
			return status.Available
		}
	}
	return result.Status == waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REACHABLE && method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS && result.CanSendSMS
}

func registrationRequestFailureMap(result EngineCodeResult, method waappv1.VerificationDeliveryMethod, route WAProxyRoute, managedRoute bool) map[string]any {
	err := result.Err
	if err == nil {
		err = registrationCodeRequestError(result)
	}
	protoErr := ToProtoError(err)
	seconds := int64(result.RetryAfter / time.Second)
	accountFlow := registrationCodeRequestFlow(result, protoErr)
	response := map[string]any{
		"success":         false,
		"request_failed":  true,
		"status":          firstNonEmpty(result.Status.String(), "VERIFICATION_REQUEST_STATUS_REJECTED"),
		"error":           protoMap(protoErr),
		"error_message":   protoErr.GetMessage(),
		"reject_reason":   registrationRejectReason(protoErr.GetMessage()),
		"method_statuses": methodStatusMaps(result.MethodStatuses),
		"proxy":           registrationProxyRouteMap(route, managedRoute),
		"sms_probe": map[string]any{
			"status":              result.Status.String(),
			"raw_status":          result.RawStatus,
			"raw_reason":          result.RawReason,
			"retry_after_seconds": seconds,
			"method_statuses":     methodStatusMaps(result.MethodStatuses),
		},
		"phone_status": registrationCodeResultPhoneStatus(result, method, true),
	}
	phoneStatus := objectField(response, "phone_status")
	phoneStatus["account_flow"] = accountFlow
	phoneStatus["account_error"] = protoErr.GetMessage()
	phoneStatus["blocked"] = accountFlow == accountProbeFlowBlocked
	phoneStatus["reject_reason"] = protoErr.GetMessage()
	if seconds > 0 {
		response["retry_after_seconds"] = seconds
		response["registration_phase"] = "OTP_COOLDOWN"
	}
	return response
}

func registrationCodeRequestError(result EngineCodeResult) error {
	switch {
	case result.RetryAfter > 0 || existRateLimitedReason(result.RawReason):
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, "verification request is cooling down", true)
	case existInvalidNumberReason(result.RawReason):
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "verification request rejected: phone format is invalid", false)
	case strings.EqualFold(result.RawReason, "no_routes") || strings.EqualFold(result.RawStatus, "no_routes"):
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "no_routes: verification route is unavailable", false)
	case strings.TrimSpace(result.RawReason) != "":
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "verification request was rejected: reason="+result.RawReason, false)
	case strings.TrimSpace(result.RawStatus) != "":
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "verification request was rejected: status="+result.RawStatus, false)
	default:
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "verification request was rejected", false)
	}
}

func registrationCodeRequestFlow(result EngineCodeResult, protoErr *waappv1.WaError) string {
	raw := strings.ToLower(strings.TrimSpace(result.RawReason + " " + result.RawStatus + " " + protoErr.GetMessage()))
	switch {
	case existInvalidNumberReason(raw) || strings.Contains(raw, "format_wrong") || strings.Contains(raw, "length_short") || strings.Contains(raw, "length_long"):
		return accountProbeFlowInvalidNumber
	case existRateLimitedReason(raw) || strings.Contains(raw, "cooling down"):
		return accountProbeFlowRateLimited
	case strings.Contains(raw, "blocked"):
		return accountProbeFlowBlocked
	default:
		return accountProbeFlowProbeFailed
	}
}

func registrationProbeFailureMap(result EngineProbeResult, route WAProxyRoute, managedRoute bool) map[string]any {
	err := result.Err
	if err == nil {
		err = registrationProbeError(result)
	}
	protoErr := ToProtoError(err)
	response := map[string]any{
		"success":         false,
		"status":          firstNonEmpty(result.Status.String(), "ACCOUNT_PROBE_STATUS_UNREACHABLE"),
		"error":           protoMap(protoErr),
		"error_message":   protoErr.GetMessage(),
		"method_statuses": methodStatusMaps(result.MethodStatuses),
		"proxy":           registrationProxyRouteMap(route, managedRoute),
		"phone_status": map[string]any{
			"account_status":     result.Status.String(),
			"account_flow":       result.AccountFlow,
			"account_raw_status": result.RawStatus,
			"account_raw_reason": result.RawReason,
			"blocked":            result.Blocked,
			"registered":         result.Registered,
			"sms_available":      result.CanSendSMS,
			"sms_status":         registrationProbeSMSStatus(result),
			"sms_wait_seconds":   result.SMSWaitSeconds,
			"supported_methods":  enumNames(result.SupportedMethods),
			"method_statuses":    methodStatusMaps(result.MethodStatuses),
		},
	}
	if result.SMSWaitSeconds > 0 {
		response["retry_after_seconds"] = result.SMSWaitSeconds
		response["registration_phase"] = "OTP_COOLDOWN"
	}
	return response
}

func registrationProbeError(result EngineProbeResult) error {
	switch {
	case result.SMSWaitSeconds > 0 || result.AccountFlow == accountProbeFlowRateLimited:
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, "verification request is cooling down", true)
	case result.Blocked:
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "number is blocked", false)
	case result.Status != waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REACHABLE:
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "account probe is not reachable", false)
	default:
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "verification route unavailable", true)
	}
}

func registrationProbeSMSStatus(result EngineProbeResult) string {
	if result.CanSendSMS {
		return "AVAILABLE"
	}
	if result.SMSWaitSeconds > 0 {
		return "COOLDOWN"
	}
	return "UNAVAILABLE"
}

func registrationCodeResultPhoneStatus(result EngineCodeResult, method waappv1.VerificationDeliveryMethod, failed bool) map[string]any {
	smsStatus, smsAvailable, smsWaitSeconds := registrationCodeSMSStatus(result.MethodStatuses)
	if method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS && smsStatus == "UNKNOWN" && result.RetryAfter > 0 {
		smsStatus = "COOLDOWN"
		smsAvailable = false
		smsWaitSeconds = int64(result.RetryAfter / time.Second)
	}
	registrationPhaseValue := registrationPhase(!failed, "accepted", result.RetryAfter)
	if failed {
		registrationPhaseValue = "OTP_REQUEST_FAILED"
		if result.RetryAfter > 0 {
			registrationPhaseValue = "OTP_COOLDOWN"
		}
	}
	rawStatus := result.RawStatus
	rawReason := result.RawReason
	return map[string]any{
		"account_status":               registrationCodeAccountStatus(failed),
		"account_flow":                 accountProbeFlowUnknown,
		"account_raw_status":           rawStatus,
		"account_raw_reason":           rawReason,
		"account_reachable":            !failed,
		"request_failed":               failed,
		"sms_status":                   smsStatus,
		"sms_available":                smsAvailable,
		"sms_wait_seconds":             smsWaitSeconds,
		"delivery_method":              method.String(),
		"registration_method":          registrationMethodName(method, ""),
		"selected_method_wait_seconds": int64(result.RetryAfter / time.Second),
		"method_statuses":              methodStatusMaps(result.MethodStatuses),
		"can_register":                 !failed,
		"registration_phase":           registrationPhaseValue,
		"verification_status":          result.Status.String(),
		"verification_reason":          rawReason,
		"verification_outcome":         rawStatus,
	}
}

func registrationCodeSMSStatus(statuses []VerificationMethodStatus) (string, bool, int64) {
	for _, status := range statuses {
		if status.Code == "sms" || status.Method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS {
			if status.CooldownSeconds > 0 {
				return "COOLDOWN", false, status.CooldownSeconds
			}
			if status.Available {
				return "AVAILABLE", true, 0
			}
			return "UNAVAILABLE", false, 0
		}
	}
	return "UNKNOWN", false, 0
}

func registrationCodeAccountStatus(failed bool) string {
	if failed {
		return waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED.String()
	}
	return waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REACHABLE.String()
}

func (g *actionGateway) discardRejectedRegistration(ctx context.Context, basePayload map[string]any, waAccountID string, verificationRequestID string) error {
	if strings.TrimSpace(waAccountID) == "" {
		return nil
	}
	result, err := g.cleanupFailedRegistration(ctx, map[string]any{
		"wa_account_id":           waAccountID,
		"verification_request_id": verificationRequestID,
	})
	if err != nil {
		return err
	}
	if boolField(result, "success") {
		return nil
	}
	return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, firstNonEmpty(textField(result, "error_message"), "discard rejected WA registration failed"), false)
}

func rejectedRegistrationResult(basePayload map[string]any, requested map[string]any) map[string]any {
	errorMessage := firstNonEmpty(textField(requested, "error_message"), textField(objectField(requested, "error"), "message"), "WA registration request was rejected")
	response := map[string]any{
		"success":                 false,
		"status":                  firstNonEmpty(textField(requested, "status"), "OTP_REQUEST_REJECTED"),
		"error":                   objectField(requested, "error"),
		"error_message":           errorMessage,
		"reject_reason":           registrationRejectReason(errorMessage),
		"phone":                   objectField(basePayload, "phone"),
		"registration_phase":      firstNonEmpty(textField(requested, "registration_phase"), "OTP_REQUEST_REJECTED"),
		"fingerprint_persistence": "DISCARDED",
		"persisted":               false,
		"phone_status":            objectField(requested, "phone_status"),
		"proxy":                   registrationOrchestratorProxySummary(objectField(requested, "proxy")),
	}
	if seconds := numberField(requested, "retry_after_seconds"); seconds > 0 {
		response["retry_after_seconds"] = seconds
	}
	if methodStatuses, ok := requested["method_statuses"]; ok {
		response["method_statuses"] = methodStatuses
	}
	return response
}

func registrationRejectReason(errorMessage string) string {
	normalized := strings.ToLower(errorMessage)
	if strings.Contains(normalized, "format_wrong") || strings.Contains(normalized, "length_short") || strings.Contains(normalized, "length_long") || strings.Contains(normalized, "phone format") {
		return "invalid_number"
	}
	if strings.Contains(normalized, "too_recent") || strings.Contains(normalized, "too_many") || strings.Contains(normalized, "temporarily_unavailable") || strings.Contains(normalized, "cooldown") || strings.Contains(normalized, "cooling down") {
		return "rate_limited"
	}
	if strings.Contains(normalized, "no_routes") {
		return "no_routes"
	}
	if strings.Contains(normalized, "blocked") {
		return "blocked"
	}
	return "rejected"
}

func registrationOrchestratorProxySummary(proxy map[string]any) map[string]any {
	mode := firstNonEmpty(textField(proxy, "proxy_mode"), "DIRECT")
	countryCode := firstNonEmpty(textField(proxy, "country_code"), "LOCAL")
	return map[string]any{"proxy_mode": mode, "country_code": countryCode}
}
