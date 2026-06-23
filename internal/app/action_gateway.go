package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const transientStateTTL = 30 * time.Minute
const registrationAttemptStateTTL = 26 * time.Hour
const registrationOTPWaitDefaultTTL = 20 * time.Minute

type registrationOTPWait struct {
	WAAccountID           string `json:"wa_account_id"`
	VerificationRequestID string `json:"verification_request_id"`
	ResumeURL             string `json:"resume_url"`
	CreatedAtUnix         int64  `json:"created_at_unix"`
}

type actionGateway struct{ server *Server }

func NewActionGateway(server *Server) http.Handler {
	return &actionGateway{server: server}
}

func (g *actionGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeActionJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	payload, ok := readActionPayload(w, r)
	if !ok {
		return
	}
	action := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/wa/actions/"), "/")
	var result map[string]any
	var err error
	switch action {
	case "proxy-settings":
		result, err = g.proxySettings(r.Context(), payload)
	case "fingerprints/random":
		result, err = g.generateTransientFingerprint(r.Context(), payload)
	case "fingerprints/commit":
		result, err = g.commitFingerprint(r.Context(), payload)
	case "registration/request-sms-otp":
		result, err = g.requestSMSOTP(r.Context(), payload)
	case "registration/await-otp":
		result, err = g.awaitOTP(r.Context(), payload)
	case "registration/resume-otp":
		result, err = g.resumeOTP(r.Context(), payload)
	case "registration/submit-otp":
		result, err = g.submitOTP(r.Context(), payload)
	case "registration/account-transfer/refresh":
		result, err = g.refreshAccountTransferChallenge(r.Context(), payload)
	case "registration/account-transfer/poll":
		result, err = g.pollAccountTransferRegistration(r.Context(), payload)
	case "registration/cleanup-failed-account":
		result, err = g.cleanupFailedRegistration(r.Context(), payload)
	case "registration/persist-login-state":
		result, err = g.persistLoginState(r.Context(), payload)
	case "registration/check-login-state":
		result, err = g.checkLoginState(r.Context(), payload)
	default:
		writeActionJSON(w, http.StatusNotFound, map[string]string{"error": "unknown WA action"})
		return
	}
	if err != nil {
		writeActionJSON(w, http.StatusOK, actionError(err))
		return
	}
	writeActionJSON(w, http.StatusOK, result)
}

func (g *actionGateway) proxySettings(ctx context.Context, payload map[string]any) (map[string]any, error) {
	route, useProxy := g.server.resolveWAProxyRoute(waProxyResolveRequest{
		Payload:     payload,
		CountryCode: proxyCountryCodeFromPayload(payload),
	})
	out := waProxySummary(route, useProxy)
	out["preflight"] = false
	return out, nil
}

func (g *actionGateway) generateTransientFingerprint(ctx context.Context, payload map[string]any) (map[string]any, error) {
	engine, err := g.nativeEngine()
	if err != nil {
		return nil, err
	}
	phone := normalizePhone(phoneFromAction(payload))
	if phone.GetE164Number() == "" {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "phone is required", false)
	}
	state, err := engine.newState(phone)
	if err != nil {
		return nil, err
	}
	data, err := marshalNativeState(state)
	if err != nil {
		return nil, err
	}
	ref := g.server.ids.NewID("wafp_")
	if err := g.server.runtime.SaveTransientState(ctx, ref, data, transientStateTTL); err != nil {
		return nil, err
	}
	profile := phoneProfileToProto(phone, state.Profile)
	return map[string]any{
		"success":                   true,
		"fingerprint_ref":           ref,
		"transient_fingerprint_ref": ref,
		"fingerprint_persistence":   "TRANSIENT_NOT_COMMITTED",
		"fingerprint":               fingerprintSummary(profile),
	}, nil
}

func fingerprintSummary(profile *waappv1.PhoneFingerprintProfile) map[string]any {
	return map[string]any{
		"schema":          profile.GetSchema(),
		"profile_sha256":  profile.GetProfileSha256(),
		"phone_sha256":    profile.GetPhoneSha256(),
		"device_vendor":   profile.GetDeviceVendor(),
		"device_model":    profile.GetDeviceModel(),
		"android_version": profile.GetAndroidVersion(),
	}
}

func (g *actionGateway) commitFingerprint(ctx context.Context, payload map[string]any) (map[string]any, error) {
	ref := textField(payload, "transient_fingerprint_ref")
	state, err := g.loadTransientState(ctx, ref)
	if err != nil {
		return nil, err
	}
	account, profile, protocol, err := g.server.commitNativeState(ctx, normalizePhone(phoneFromAction(payload)), state)
	if err != nil {
		return nil, err
	}
	_ = g.server.runtime.DeleteTransientState(ctx, ref)
	return map[string]any{
		"success":             true,
		"wa_account_id":       waAccountID(account),
		"client_profile_id":   profile.GetClientProfileId(),
		"protocol_profile_id": protocol.GetProtocolProfileId(),
		"client_profile":      protoMap(profile),
	}, nil
}

func (g *actionGateway) requestSMSOTP(ctx context.Context, payload map[string]any) (map[string]any, error) {
	method := registrationMethodFromPayload(payload)
	if reason := directRegistrationMethodUnsupportedReason(method); reason != "" {
		return registrationMethodUnsupportedMap(method, reason), nil
	}
	runner, route, managedRoute, err := g.registrationRunner(payload)
	if err != nil {
		return nil, err
	}
	reqCtx := actionContext(payload)
	resp, err := g.server.requestVerificationCode(ctx, &waappv1.RequestVerificationCodeRequest{
		Context:           reqCtx,
		WaAccountId:       textField(payload, "wa_account_id"),
		ClientProfileId:   textField(payload, "client_profile_id"),
		ProtocolProfileId: textField(payload, "protocol_profile_id"),
		DeliveryMethod:    method,
	}, runner)
	runner.CloseIdleConnections()
	if err != nil {
		return nil, err
	}
	if resp.GetError() != nil {
		return map[string]any{"success": false, "error": protoMap(resp.GetError()), "error_message": resp.GetError().GetMessage()}, nil
	}
	record := resp.GetVerificationRequest()
	success := record.GetStatus() == waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT || record.GetStatus() == waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_WAITING
	response := map[string]any{
		"success":                 success,
		"status":                  record.GetStatus().String(),
		"verification_request_id": record.GetVerificationRequestId(),
		"verification_request":    protoMap(record),
		"method_statuses":         protoMethodStatusMaps(record.GetMethodStatuses()),
		"proxy":                   registrationProxyRouteMap(route, managedRoute),
	}
	if challenge := resp.GetAccountTransferChallenge(); challenge != nil {
		response["account_transfer_challenge"] = protoMap(challenge)
		response["registration_phase"] = "ACCOUNT_TRANSFER_WAITING"
	}
	if seconds := durationSeconds(record.GetRetryAfter()); seconds > 0 {
		response["retry_after_seconds"] = seconds
	}
	return response, nil
}

func (g *actionGateway) awaitOTP(ctx context.Context, payload map[string]any) (map[string]any, error) {
	wait, ttl, err := registrationOTPWaitFromPayload(payload)
	if err != nil {
		return nil, err
	}
	if err := g.saveRegistrationOTPWait(ctx, wait, ttl); err != nil {
		return nil, err
	}
	return map[string]any{
		"success":                 true,
		"wa_account_id":           wait.WAAccountID,
		"verification_request_id": wait.VerificationRequestID,
		"timeout_seconds":         int(ttl.Seconds()),
	}, nil
}

func (g *actionGateway) resumeOTP(ctx context.Context, payload map[string]any) (map[string]any, error) {
	code := firstNonEmpty(textField(payload, "otp"), textField(payload, "code"), textField(payload, "verification_code"))
	if strings.TrimSpace(code) == "" {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "otp is required", false)
	}
	wait, err := g.loadRegistrationOTPWait(ctx, textField(payload, "wa_account_id"), textField(payload, "verification_request_id"))
	if err != nil {
		return nil, err
	}
	if wait.ResumeURL != "" {
		if err := postRegistrationOTPResume(ctx, wait, code); err != nil {
			return nil, err
		}
		_ = g.deleteRegistrationOTPWait(ctx, wait)
		return map[string]any{"success": true, "wa_account_id": wait.WAAccountID, "verification_request_id": wait.VerificationRequestID}, nil
	}
	submitPayload := cloneActionPayload(payload)
	submitPayload["verification_request_id"] = wait.VerificationRequestID
	submitPayload["code"] = code
	result, err := g.submitOTP(ctx, submitPayload)
	if err != nil {
		return nil, err
	}
	if result["success"] == true {
		_ = g.deleteRegistrationOTPWait(ctx, wait)
	}
	return result, nil
}

func registrationOTPWaitFromPayload(payload map[string]any) (registrationOTPWait, time.Duration, error) {
	wait := registrationOTPWait{
		WAAccountID:           textField(payload, "wa_account_id"),
		VerificationRequestID: textField(payload, "verification_request_id"),
		ResumeURL:             textField(payload, "resume_url"),
		CreatedAtUnix:         time.Now().UTC().Unix(),
	}
	if wait.VerificationRequestID == "" {
		return registrationOTPWait{}, 0, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "verification_request_id is required", false)
	}
	if wait.WAAccountID != "" {
		accountID, err := requireWAAccountID(wait.WAAccountID)
		if err != nil {
			return registrationOTPWait{}, 0, err
		}
		wait.WAAccountID = accountID
	}
	ttl := time.Duration(numberField(payload, "timeout_seconds")) * time.Second
	if ttl <= 0 {
		ttl = registrationOTPWaitDefaultTTL
	}
	return wait, ttl, nil
}

func (g *actionGateway) saveRegistrationOTPWait(ctx context.Context, wait registrationOTPWait, ttl time.Duration) error {
	data, err := json.Marshal(wait)
	if err != nil {
		return err
	}
	if err := g.server.runtime.SaveTransientState(ctx, registrationOTPWaitKey(wait.VerificationRequestID), data, ttl); err != nil {
		return err
	}
	if wait.WAAccountID != "" {
		if err := g.server.runtime.SaveTransientState(ctx, registrationOTPWaitAccountKey(wait.WAAccountID), data, ttl); err != nil {
			return err
		}
	}
	return nil
}

func (g *actionGateway) loadRegistrationOTPWait(ctx context.Context, waAccountIDValue string, verificationRequestID string) (registrationOTPWait, error) {
	key := ""
	if verificationRequestID != "" {
		key = registrationOTPWaitKey(verificationRequestID)
	} else if waAccountIDValue != "" {
		accountID, err := requireWAAccountID(waAccountIDValue)
		if err != nil {
			return registrationOTPWait{}, err
		}
		key = registrationOTPWaitAccountKey(accountID)
	}
	if key == "" {
		return registrationOTPWait{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "wa_account_id or verification_request_id is required", false)
	}
	data, err := g.server.runtime.GetTransientState(ctx, key)
	if err != nil {
		return registrationOTPWait{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_PROFILE_NOT_FOUND, "registration otp wait not found", false)
	}
	var wait registrationOTPWait
	if err := json.Unmarshal(data, &wait); err != nil {
		return registrationOTPWait{}, err
	}
	return wait, nil
}

func (g *actionGateway) deleteRegistrationOTPWait(ctx context.Context, wait registrationOTPWait) error {
	_ = g.server.runtime.DeleteTransientState(ctx, registrationOTPWaitKey(wait.VerificationRequestID))
	if wait.WAAccountID != "" {
		_ = g.server.runtime.DeleteTransientState(ctx, registrationOTPWaitAccountKey(wait.WAAccountID))
	}
	return nil
}

func postRegistrationOTPResume(ctx context.Context, wait registrationOTPWait, code string) error {
	body, err := json.Marshal(map[string]any{
		"otp":                     code,
		"code":                    code,
		"verification_code":       code,
		"verification_request_id": wait.VerificationRequestID,
		"otp_source":              "manual_frontend",
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wait.ResumeURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("resume registration otp wait: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("resume registration otp wait returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func registrationOTPWaitKey(verificationRequestID string) string {
	return "wa-registration-otp-wait:verification:" + verificationRequestID
}

func registrationOTPWaitAccountKey(waAccountIDValue string) string {
	return "wa-registration-otp-wait:account:" + waAccountIDValue
}

func (g *actionGateway) submitOTP(ctx context.Context, payload map[string]any) (map[string]any, error) {
	runner, route, managedRoute, err := g.registrationRunner(payload)
	if err != nil {
		return nil, err
	}
	resp, err := g.server.submitVerificationCode(ctx, &waappv1.SubmitVerificationCodeRequest{
		Context:               actionContext(payload),
		VerificationRequestId: textField(payload, "verification_request_id"),
		SubmittedCode:         &waappv1.SubmitVerificationCodeRequest_Code{Code: textField(payload, "code")},
	}, runner)
	runner.CloseIdleConnections()
	if err != nil {
		return nil, err
	}
	if resp.GetError() != nil {
		return map[string]any{"success": false, "error": protoMap(resp.GetError()), "error_message": resp.GetError().GetMessage(), "registration": protoMap(resp.GetRegistration())}, nil
	}
	success := resp.GetRegistration().GetStatus() == waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED && resp.GetLoginState().GetStatus() == waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE
	if success {
		_ = g.deleteRegistrationOTPWait(ctx, registrationOTPWait{
			WAAccountID:           resp.GetRegistration().GetWaAccountId(),
			VerificationRequestID: resp.GetRegistration().GetVerificationRequestId(),
		})
	}
	return map[string]any{
		"success":      success,
		"status":       resp.GetRegistration().GetStatus().String(),
		"registration": protoMap(resp.GetRegistration()),
		"login_state":  protoMap(resp.GetLoginState()),
		"proxy":        registrationProxyRouteMap(route, managedRoute),
	}, nil
}

func (g *actionGateway) refreshAccountTransferChallenge(ctx context.Context, payload map[string]any) (map[string]any, error) {
	resp, err := g.server.RefreshAccountTransferChallenge(ctx, &waappv1.RefreshAccountTransferChallengeRequest{
		Context:               actionContext(payload),
		VerificationRequestId: textField(payload, "verification_request_id"),
	})
	if err != nil {
		return nil, err
	}
	if resp.GetError() != nil {
		return map[string]any{"success": false, "error": protoMap(resp.GetError()), "error_message": resp.GetError().GetMessage()}, nil
	}
	return map[string]any{
		"success":                    true,
		"registration_phase":         "ACCOUNT_TRANSFER_WAITING",
		"account_transfer_challenge": protoMap(resp.GetAccountTransferChallenge()),
	}, nil
}

func (g *actionGateway) pollAccountTransferRegistration(ctx context.Context, payload map[string]any) (map[string]any, error) {
	attempts := int(numberField(payload, "max_attempts"))
	if attempts <= 0 {
		attempts = 1
	}
	if attempts > 100 {
		attempts = 100
	}
	interval := time.Duration(numberField(payload, "interval_seconds")) * time.Second
	var result map[string]any
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 && interval > 0 {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		submitPayload := cloneActionPayload(payload)
		submitPayload["code"] = ""
		resultValue, err := g.submitOTP(ctx, submitPayload)
		if err != nil {
			return nil, err
		}
		result = resultValue
		if boolField(result, "success") {
			_ = g.deleteRegistrationOTPWait(ctx, registrationOTPWait{WAAccountID: textField(payload, "wa_account_id"), VerificationRequestID: textField(payload, "verification_request_id")})
			result["attempts"] = attempt + 1
			return result, nil
		}
		if !accountTransferPollRetryable(result) {
			result["attempts"] = attempt + 1
			return result, nil
		}
	}
	if result == nil {
		result = map[string]any{"success": false}
	}
	result["registration_phase"] = "ACCOUNT_TRANSFER_WAITING"
	result["attempts"] = attempts
	return result, nil
}

func accountTransferPollRetryable(result map[string]any) bool {
	if result == nil {
		return true
	}
	if textField(result, "status") == waappv1.RegistrationStatus_REGISTRATION_STATUS_SUBMITTED.String() {
		return true
	}
	errorMap := objectField(result, "error")
	if boolField(errorMap, "retryable") {
		return true
	}
	message := strings.ToLower(firstNonEmpty(textField(result, "error_message"), textField(errorMap, "message")))
	return strings.Contains(message, "pending") || strings.Contains(message, "temporarily") || strings.Contains(message, "too_recent")
}

func (g *actionGateway) cleanupFailedRegistration(ctx context.Context, payload map[string]any) (map[string]any, error) {
	reqCtx := actionContext(payload)
	accountID := cleanupWAAccountID(payload)
	verificationRequestID := cleanupVerificationRequestID(payload)
	if verificationRequestID != "" || accountID != "" {
		_ = g.deleteRegistrationOTPWait(ctx, registrationOTPWait{
			WAAccountID:           accountID,
			VerificationRequestID: verificationRequestID,
		})
	}
	if accountID == "" {
		return map[string]any{"success": true, "deleted": false, "reason": "missing_wa_account_id"}, nil
	}
	normalizedAccountID, err := requireWAAccountID(accountID)
	if err != nil {
		return nil, err
	}
	account, err := g.server.getWAAccount(ctx, normalizedAccountID)
	if isWAAccountNotFound(err) {
		return map[string]any{"success": true, "deleted": false, "wa_account_id": normalizedAccountID, "reason": "already_deleted"}, nil
	}
	if err != nil {
		return nil, err
	}
	status := waAccountStatus(account)
	if status != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_PENDING_REGISTRATION {
		return map[string]any{"success": true, "deleted": false, "wa_account_id": normalizedAccountID, "status": status.String(), "reason": "not_pending_registration"}, nil
	}
	resp, err := g.server.DeleteWAAccount(ctx, &waappv1.DeleteWAAccountRequest{Context: reqCtx, WaAccountId: normalizedAccountID})
	if err != nil {
		return nil, err
	}
	if resp.GetError() != nil {
		return map[string]any{"success": false, "deleted": false, "wa_account_id": normalizedAccountID, "error": protoMap(resp.GetError()), "error_message": resp.GetError().GetMessage()}, nil
	}
	return map[string]any{"success": true, "deleted": resp.GetSuccess(), "wa_account_id": normalizedAccountID}, nil
}

func (g *actionGateway) persistLoginState(ctx context.Context, payload map[string]any) (map[string]any, error) {
	registration := objectField(payload, "registration")
	if nested := objectField(registration, "registration"); len(nested) > 0 {
		registration = nested
	}
	registrationID := textField(registration, "registration_id")
	var loginState *waappv1.LoginState
	var err error
	if registrationID != "" {
		loginState, err = g.server.store.GetLoginStateByRegistration(ctx, registrationID)
	} else if clientProfileID := textField(payload, "client_profile_id"); clientProfileID != "" {
		loginState, err = g.server.store.GetActiveLoginState(ctx, textField(registration, "wa_account_id"), clientProfileID)
	} else {
		err = NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "registration_id or client_profile_id is required", false)
	}
	if err != nil {
		return map[string]any{"success": false, "error": protoMap(ToProtoError(err)), "error_message": ToProtoError(err).GetMessage()}, nil
	}
	ok := loginState.GetStatus() == waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE
	return map[string]any{"success": ok, "status": loginState.GetStatus().String(), "login_state": protoMap(loginState)}, nil
}

func (g *actionGateway) checkLoginState(ctx context.Context, payload map[string]any) (map[string]any, error) {
	runner, err := g.nativeEngineForPayload(payload)
	if err != nil {
		return nil, err
	}
	loginStatePayload := objectField(payload, "login_state")
	req := &waappv1.CheckLoginStateRequest{
		Context:              actionContext(payload),
		LoginStateId:         firstNonEmpty(textField(payload, "login_state_id"), textField(loginStatePayload, "login_state_id")),
		WaAccountId:          firstNonEmpty(textField(payload, "wa_account_id"), textField(loginStatePayload, "wa_account_id")),
		ClientProfileId:      firstNonEmpty(textField(payload, "client_profile_id"), textField(loginStatePayload, "client_profile_id")),
		RegisteredIdentityId: firstNonEmpty(textField(payload, "registered_identity_id"), textField(loginStatePayload, "registered_identity_id")),
	}
	if timeout := numberField(payload, "remote_timeout_seconds"); timeout > 0 {
		req.RemoteTimeout = durationpb.New(time.Duration(timeout) * time.Second)
	}
	resp, err := g.server.checkLoginState(ctx, req, runner)
	if err != nil {
		return nil, err
	}
	check := resp.GetCheck()
	ok := resp.GetError() == nil && check.GetStatus() == waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE && resp.GetLoginState().GetStatus() == waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE
	out := map[string]any{
		"success":     ok,
		"status":      check.GetStatus().String(),
		"login_state": protoMap(resp.GetLoginState()),
		"check":       protoMap(check),
	}
	if resp.GetError() != nil {
		out["error"] = protoMap(resp.GetError())
		out["error_message"] = resp.GetError().GetMessage()
	}
	return out, nil
}

func (s *Server) commitNativeState(ctx context.Context, phone *waappv1.PhoneTarget, state nativeState) (*waappv1.WAAccount, *waappv1.ClientProfile, *waappv1.ProtocolProfile, error) {
	engine, ok := s.runner.(nativeStateSaver)
	if !ok {
		return nil, nil, nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "native engine is required", false)
	}
	if phone.GetE164Number() == "" {
		return nil, nil, nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "phone is required", false)
	}
	account, err := s.store.FindWAAccountByPhone(ctx, phone.GetE164Number())
	if err != nil {
		now := s.clock.Now()
		account = newWAAccount(s.ids.NewID("waacc_"), "", phone, waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_PENDING_REGISTRATION, &waappv1.AuditStamp{CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)})
		account, err = s.saveWAAccount(ctx, account)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	protocol, err := s.ensureDefaultProtocolProfile(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	now := s.clock.Now()
	profile := &waappv1.ClientProfile{ClientProfileId: s.ids.NewID("wacp_"), WaAccountId: waAccountID(account), ProtocolProfileId: protocol.GetProtocolProfileId(), Status: waappv1.ClientProfileStatus_CLIENT_PROFILE_STATUS_PREPARING, RegistrationKeyState: waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_PENDING, MessagingKeyState: waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_PENDING, Audit: &waappv1.AuditStamp{CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}}
	if err := s.store.SaveClientProfile(ctx, profile); err != nil {
		return nil, nil, nil, err
	}
	state.CC = firstNonEmpty(state.CC, phoneCC(phone))
	state.Phone = firstNonEmpty(state.Phone, phoneNational(phone))
	if err := engine.saveState(ctx, profile.GetClientProfileId(), state); err != nil {
		profile.Status = waappv1.ClientProfileStatus_CLIENT_PROFILE_STATUS_REJECTED
		profile.LastError = ToProtoError(err)
		_ = s.store.SaveClientProfile(ctx, profile)
		return nil, nil, nil, err
	}
	profile.Status = waappv1.ClientProfileStatus_CLIENT_PROFILE_STATUS_READY
	profile.RegistrationKeyState = waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_READY
	profile.MessagingKeyState = waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_READY
	profile.Audit.UpdatedAt = timestamppb.New(s.clock.Now())
	if err := s.store.SaveClientProfile(ctx, profile); err != nil {
		return nil, nil, nil, err
	}
	return account, profile, protocol, nil
}

type nativeStateSaver interface {
	saveState(context.Context, string, nativeState) error
}

func (s *Server) ensureDefaultProtocolProfile(ctx context.Context) (*waappv1.ProtocolProfile, error) {
	protocolID := "waproto_native"
	if profile, err := s.store.GetProtocolProfile(ctx, protocolID); err == nil {
		if nativeAppVersion(profile.GetAppVersion()) != defaultWAAppVersion {
			profile.AppVersion = defaultWAAppVersion
			_ = s.store.SaveProtocolProfile(ctx, profile)
		}
		return profile, nil
	}
	now := s.clock.Now()
	artifactID := "waart_native"
	artifact := &waappv1.AppArtifact{ArtifactId: artifactID, Label: "WA native app", VersionLabel: "native", ObservedAt: timestamppb.New(now)}
	if err := s.store.SaveAppArtifact(ctx, artifact); err != nil {
		return nil, err
	}
	profile := &waappv1.ProtocolProfile{
		ProtocolProfileId: protocolID,
		AppArtifactId:     artifactID,
		DisplayName:       "WA native protocol",
		AppVersion:        defaultWAAppVersion,
		Status:            waappv1.ProtocolProfileStatus_PROTOCOL_PROFILE_STATUS_ACTIVE,
		Capabilities: []waappv1.ProtocolCapability{
			waappv1.ProtocolCapability_PROTOCOL_CAPABILITY_ACCOUNT_PROBE,
			waappv1.ProtocolCapability_PROTOCOL_CAPABILITY_CODE_REQUEST,
			waappv1.ProtocolCapability_PROTOCOL_CAPABILITY_CODE_SUBMIT,
			waappv1.ProtocolCapability_PROTOCOL_CAPABILITY_MESSAGE_SESSION,
			waappv1.ProtocolCapability_PROTOCOL_CAPABILITY_ACCOUNT_SETTINGS,
		},
		RegistrationFlows: []waappv1.RegistrationFlowKind{waappv1.RegistrationFlowKind_REGISTRATION_FLOW_KIND_NEW_ACCOUNT, waappv1.RegistrationFlowKind_REGISTRATION_FLOW_KIND_EXISTING_ACCOUNT},
		MessageTransports: []waappv1.MessageTransportKind{waappv1.MessageTransportKind_MESSAGE_TRANSPORT_KIND_LONG_CONNECTION},
		DiscoveredAt:      timestamppb.New(now),
		Audit:             &waappv1.AuditStamp{CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)},
	}
	if err := s.store.SaveProtocolProfile(ctx, profile); err != nil {
		return nil, err
	}
	return profile, nil
}

func (g *actionGateway) nativeEngineForPayload(payload map[string]any) (*NativeEngine, error) {
	engine, err := g.nativeEngine()
	if err != nil {
		return nil, err
	}
	proxyURL := actionProxyURL(payload)
	if proxyURL == "" {
		return engine, nil
	}
	return engine.WithProxyURL(proxyURL)
}

func (g *actionGateway) registrationRunner(payload map[string]any) (*NativeEngine, WAProxyRoute, bool, error) {
	engine, err := g.nativeEngine()
	if err != nil {
		return nil, WAProxyRoute{}, false, err
	}
	route, useProxy := g.server.resolveWAProxyRoute(waProxyResolveRequest{
		Payload:     payload,
		CountryCode: proxyCountryCodeFromPayload(payload),
	})
	if !useProxy {
		return engine, route, false, nil
	}
	proxied, err := engine.WithProxyURL(route.ProxyURL)
	if err != nil {
		return nil, WAProxyRoute{}, false, err
	}
	return proxied, route, true, nil
}

func registrationProxyRouteMap(route WAProxyRoute, managed bool) map[string]any {
	if !managed {
		return map[string]any{}
	}
	result := map[string]any{
		"proxy_mode":   firstNonEmpty(route.ProxyMode, "PROXY"),
		"country_code": firstNonEmpty(route.CountryCode, "UNKNOWN"),
	}
	if strings.TrimSpace(route.AccountID) != "" {
		result["account_id"] = route.AccountID
	}
	if strings.TrimSpace(route.RouteID) != "" {
		result["route_id"] = route.RouteID
	}
	if strings.TrimSpace(route.Source) != "" {
		result["source"] = route.Source
	}
	if strings.TrimSpace(route.PolicyMode) != "" {
		result["policy_mode"] = route.PolicyMode
	}
	return result
}

func actionProxyURL(payload map[string]any) string {
	if proxyURL := firstNonEmpty(textField(payload, "proxy_url"), textField(objectField(payload, "proxy"), "proxy_url")); proxyURL != "" {
		return proxyURL
	}
	rawState := firstNonEmpty(textField(payload, "proxy_state_json"), textField(payload, "state_json"), textField(objectField(payload, "proxy"), "proxy_state_json"), textField(objectField(payload, "proxy"), "state_json"))
	if rawState == "" {
		return ""
	}
	state := map[string]any{}
	if err := json.Unmarshal([]byte(rawState), &state); err != nil {
		return ""
	}
	return firstNonEmpty(textField(state, "_gopay_proxy"), textField(state, "proxy_url"), textField(objectField(state, "proxy"), "proxy_url"))
}

func cleanupWAAccountID(payload map[string]any) string {
	registration := objectField(payload, "registration")
	if nested := objectField(registration, "registration"); len(nested) > 0 {
		registration = nested
	}
	verificationRequest := objectField(payload, "verification_request")
	data := objectField(payload, "data")
	return firstNonEmpty(
		textField(payload, "wa_account_id"),
		textField(registration, "wa_account_id"),
		textField(verificationRequest, "wa_account_id"),
		textField(objectField(payload, "account"), "wa_account_id"),
		textField(data, "wa_account_id"),
		textField(objectField(data, "registration"), "wa_account_id"),
		textField(objectField(data, "verification_request"), "wa_account_id"),
	)
}

func cleanupVerificationRequestID(payload map[string]any) string {
	verificationRequest := objectField(payload, "verification_request")
	data := objectField(payload, "data")
	return firstNonEmpty(
		textField(payload, "verification_request_id"),
		textField(verificationRequest, "verification_request_id"),
		textField(data, "verification_request_id"),
		textField(objectField(data, "verification_request"), "verification_request_id"),
	)
}

func (g *actionGateway) nativeEngine() (*NativeEngine, error) {
	engine, ok := g.server.runner.(*NativeEngine)
	if !ok {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "native engine is required", false)
	}
	return engine, nil
}

func (g *actionGateway) loadTransientState(ctx context.Context, ref string) (nativeState, error) {
	data, err := g.server.runtime.GetTransientState(ctx, ref)
	if err != nil {
		return nativeState{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_PROFILE_NOT_FOUND, "transient fingerprint state not found", false)
	}
	return unmarshalNativeState(data)
}

func readActionPayload(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeActionJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return nil, false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return map[string]any{}, true
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	payload := map[string]any{}
	if err := dec.Decode(&payload); err != nil {
		writeActionJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be json"})
		return nil, false
	}
	return payload, true
}

func writeActionJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func actionContext(payload map[string]any) *waappv1.RequestContext {
	return &waappv1.RequestContext{
		RequestId:     textField(payload, "request_id"),
		ActorId:       textField(payload, "actor_id"),
		CorrelationId: firstNonEmpty(textField(payload, "correlation_id"), textField(payload, "job_id")),
		TraceId:       textField(payload, "trace_id"),
	}
}

func phoneFromAction(payload map[string]any) *waappv1.PhoneTarget {
	phone := objectField(payload, "phone")
	if len(phone) == 0 {
		phone = payload
	}
	return &waappv1.PhoneTarget{
		E164Number:         firstNonEmpty(textField(phone, "e164_number"), textField(payload, "e164_number")),
		CountryCallingCode: firstNonEmpty(textField(phone, "country_calling_code"), textField(payload, "country_calling_code"), textField(payload, "cc")),
		NationalNumber:     firstNonEmpty(textField(phone, "national_number"), textField(payload, "national_number"), textField(payload, "phone"), textField(payload, "number")),
		CountryIso2:        firstNonEmpty(textField(phone, "country_iso2"), textField(payload, "country_iso2"), textField(payload, "country_code")),
	}
}

func objectField(data map[string]any, key string) map[string]any {
	if data == nil {
		return map[string]any{}
	}
	if value, ok := data[key].(map[string]any); ok {
		return value
	}
	return map[string]any{}
}

func textField(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func numberField(data map[string]any, key string) int64 {
	switch value := data[key].(type) {
	case json.Number:
		n, _ := value.Int64()
		return n
	case float64:
		return int64(value)
	case string:
		var n int64
		_, _ = fmt.Sscan(value, &n)
		return n
	default:
		return 0
	}
}

func protoMap(msg proto.Message) map[string]any {
	if msg == nil {
		return map[string]any{}
	}
	data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(msg)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return out
}

func actionError(err error) map[string]any {
	protoErr := ToProtoError(err)
	return map[string]any{"success": false, "error": protoMap(protoErr), "error_message": protoErr.GetMessage()}
}

func enumNames[T interface{ String() string }](values []T) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	return out
}
