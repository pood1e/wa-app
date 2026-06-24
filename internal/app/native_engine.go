package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultWAAppVersion  = "2.26.24.77"
	defaultWAABPropURL   = "https://y9yrsygcg6.execute-api.us-east-1.amazonaws.com/s/s?_=/v2/reg_onboard_abprop&"
	defaultWAExistURL    = "https://y9yrsygcg6.execute-api.us-east-1.amazonaws.com/s/s?_=/v2/exist&"
	defaultWACodeURL     = "https://y9yrsygcg6.execute-api.us-east-1.amazonaws.com/s/s?_=/v2/code&"
	defaultWARegisterURL = "https://y9yrsygcg6.execute-api.us-east-1.amazonaws.com/s/s?_=/v2/register&"

	nativeDefaultSMSCodeLength int32 = 6
)

var nativeSensitiveDigitsPattern = regexp.MustCompile(`\b[0-9]{4,8}\b`)
var chatdNodeTokenErrorPattern = regexp.MustCompile(`(?i)(readstring could not match token|invalid list-size token)\s+([0-9]{1,3})`)

type NativeEngine struct {
	stateStore     NativeStateStore
	activeProxyURL string
	http           *nativeHTTPClient
	clock          Clock
	ids            IDGenerator
	wamsys         wamsysMaterialProvider
}

func NewNativeEngine(stateStore NativeStateStore, clock Clock, ids IDGenerator) (*NativeEngine, error) {
	if stateStore == nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "native state store is required", false)
	}
	if clock == nil {
		clock = SystemClock{}
	}
	if ids == nil {
		ids = RandomIDGenerator{}
	}
	hc, err := newNativeHTTPClient("")
	if err != nil {
		return nil, err
	}
	return &NativeEngine{stateStore: stateStore, http: hc, clock: clock, ids: ids, wamsys: localWamsysMaterialProvider{}}, nil
}

func (e *NativeEngine) WithProxyURL(proxyURL string) (*NativeEngine, error) {
	proxyURL, err := normalizeProxyURLString(proxyURL)
	if err != nil {
		return nil, err
	}
	hc, err := newNativeHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	return &NativeEngine{stateStore: e.stateStore, activeProxyURL: proxyURL, http: hc, clock: e.clock, ids: e.ids, wamsys: e.wamsysProvider()}, nil
}

func (e *NativeEngine) wamsysProvider() wamsysMaterialProvider {
	if e != nil && e.wamsys != nil {
		return e.wamsys
	}
	return localWamsysMaterialProvider{}
}

func (e *NativeEngine) CloseIdleConnections() {
	if e == nil || e.http == nil {
		return
	}
	e.http.CloseIdleConnections()
}

func (e *NativeEngine) PrepareClientProfile(ctx context.Context, input EngineProfileInput) error {
	_ = ctx
	state, err := newNativeState(input.Phone)
	if err != nil {
		return err
	}
	return e.saveState(ctx, input.ClientProfileID, state)
}

func (e *NativeEngine) ProbeAccount(ctx context.Context, input EngineRegistrationInput) EngineProbeResult {
	state, err := e.newState(input.Phone)
	if err != nil {
		return EngineProbeResult{Status: waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED, Err: err}
	}
	result, _ := e.probeAccountWithState(ctx, input, state)
	return result
}

func (e *NativeEngine) probeAccountWithState(ctx context.Context, input EngineRegistrationInput, state nativeState) (EngineProbeResult, nativeState) {
	if err := ensureNativeSoftwareAttestation(&state, e.clock.Now()); err != nil {
		return EngineProbeResult{Status: waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED, Err: err}, state
	}
	params, rawKeys := e.existParams(input.Phone, state)
	if err := e.applyRuntimeWamsys(ctx, waappv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_EXIST, input.Phone, state, params, rawKeys); err != nil {
		return EngineProbeResult{Status: waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED, Err: err}, state
	}
	logNativeRegistrationMapShape("exist", input.Phone, input.DeliveryMethod, params, rawKeys)
	plain := renderNativePlain(params, rawKeys)
	client, err := e.httpForProxy()
	if err != nil {
		return EngineProbeResult{Status: waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED, Err: err}, state
	}
	data, _, err := client.postWASafe(ctx, defaultWAExistURL, plain, nativeUserAgentForState(state, input.AppVersion), state.Attestation)
	result := parseExistProbeResult(data)
	if err != nil {
		if result.Err != nil || parsedExistApplicationOutcome(result) {
			return result, state
		}
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED
		result.AccountFlow = accountProbeFlowProbeFailed
		result.Err = classifyHTTPError(data, err)
	}
	return result, state
}

func parsedExistApplicationOutcome(result EngineProbeResult) bool {
	return result.Status != waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_UNKNOWN ||
		result.AccountFlow != accountProbeFlowUnknown ||
		result.RawStatus != "" ||
		result.RawReason != "" ||
		len(result.MethodStatuses) > 0
}

func (e *NativeEngine) RequestVerificationCode(ctx context.Context, input EngineRegistrationInput) EngineCodeResult {
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: err}
	}
	result, updated := e.requestVerificationCodeWithState(ctx, input, state)
	_ = e.saveState(ctx, input.ClientProfileID, updated)
	return result
}

func (e *NativeEngine) requestVerificationCodeWithState(ctx context.Context, input EngineRegistrationInput, state nativeState) (EngineCodeResult, nativeState) {
	if err := ensureNativeSoftwareAttestation(&state, e.clock.Now()); err != nil {
		return EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: err}, state
	}
	state.nextGenerateCodeAttempt()
	params, err := e.codeRequestOrderedParams(ctx, input.Phone, input.DeliveryMethod, state, input.AuthCodeContext)
	if err != nil {
		return EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: err}, state
	}
	logNativeRegistrationOrderedShape("code", input.Phone, input.DeliveryMethod, params)
	plain := params.render()
	client, err := e.httpForProxy()
	if err != nil {
		return EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: err}, state
	}
	data, enc, err := client.postWASafe(ctx, defaultWACodeURL, plain, nativeUserAgentForState(state, input.AppVersion), state.Attestation)
	state.LastCodeParams = params.toMap()
	state.LastCodeResult = sanitizeResponse(data)
	if enc != "" {
		state.LastCodeResult["enc_sha256"] = encHash(enc)
	}
	if input.DeliveryMethod != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
		state.AccountTransfer = nativeAccountTransferState{}
	}
	retryAfter := verificationCodeRetryAfter(data, input.DeliveryMethod)
	now := e.clock.Now()
	if err != nil {
		if verificationCodeRateLimited(data) {
			return verificationCodeRejectedResult(data, input.DeliveryMethod, now, retryAfter, "verification request is cooling down"), state
		}
		return EngineCodeResult{
			Status:         waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED,
			RetryAfter:     retryAfter,
			MethodStatuses: verificationCodeMethodStatuses(data, input.DeliveryMethod),
			RawStatus:      responseStatus(data),
			RawReason:      responseReason(data),
			Err:            classifyHTTPError(data, err),
		}, state
	}
	status := waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_WAITING
	s := responseStatus(data)
	if s == "sent" || s == "ok" {
		status = waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT
	} else if verificationCodeRateLimited(data) {
		return verificationCodeRejectedResult(data, input.DeliveryMethod, now, retryAfter, "verification request is cooling down"), state
	} else if s != "" {
		return EngineCodeResult{
			Status:         waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED,
			RetryAfter:     retryAfter,
			MethodStatuses: verificationCodeMethodStatuses(data, input.DeliveryMethod),
			RawStatus:      responseStatus(data),
			RawReason:      responseReason(data),
			Err:            waProtocolError(data, "verification request was rejected"),
		}, state
	}
	result := verificationCodeResult(status, data, input.DeliveryMethod, now, retryAfter)
	if input.DeliveryMethod == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
		challenge, challengeErr := e.prepareAccountTransferChallenge(input.Phone, &state, data, now)
		if challengeErr != nil {
			result.Status = waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED
			result.Err = challengeErr
			result.RawStatus = responseStatus(data)
			result.RawReason = responseReason(data)
			return result, state
		}
		result.AccountTransferChallenge = challenge
		result.ExpectedCodeLength = challenge.GetCurrentCodeLength()
		result.ExpiresAt = challenge.GetExpiresAt().AsTime()
		result.MethodStatuses = upsertVerificationMethodStatus(result.MethodStatuses, "acc_tr", verificationWaitStatus{Present: true})
	}
	return result, state
}

func (e *NativeEngine) prepareAccountTransferChallenge(phone *waappv1.PhoneTarget, state *nativeState, data map[string]any, now time.Time) (*waappv1.AccountTransferChallenge, error) {
	codes := accountTransferCodesFromResponse(data)
	if len(codes) == 0 {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "account transfer code list is missing", false)
	}
	state.AccountTransfer = newNativeAccountTransferState(phone, codes, now)
	return state.AccountTransfer.challenge("", now)
}

func (e *NativeEngine) RefreshAccountTransferChallenge(ctx context.Context, input EngineAccountTransferChallengeInput) EngineAccountTransferChallengeResult {
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return EngineAccountTransferChallengeResult{Err: err}
	}
	if state.AccountTransfer.empty() {
		return EngineAccountTransferChallengeResult{Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_EXPIRED, "account transfer challenge is not available", false)}
	}
	challenge, err := state.AccountTransfer.challenge(input.VerificationRequestID, e.clock.Now())
	if err != nil {
		return EngineAccountTransferChallengeResult{Err: err}
	}
	_ = e.saveState(ctx, input.ClientProfileID, state)
	return EngineAccountTransferChallengeResult{Challenge: challenge}
}

func (e *NativeEngine) SubmitVerificationCode(ctx context.Context, input EngineSubmitInput) EngineRegisterResult {
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: err}
	}
	if err := ensureNativeSoftwareAttestation(&state, e.clock.Now()); err != nil {
		return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: err}
	}
	code := strings.TrimSpace(input.Code)
	if input.DeliveryMethod == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
		code, _, err = state.AccountTransfer.currentCode(e.clock.Now())
		if err != nil {
			return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: err}
		}
	}
	if code == "" {
		return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "verification code is required", false)}
	}
	params, rawKeys := e.registerParams(input.Phone, input.DeliveryMethod, code, state, input.AuthCodeContext)
	logNativeRegistrationMapShape("register", input.Phone, input.DeliveryMethod, params, rawKeys)
	plain := renderNativePlain(params, rawKeys)
	client, err := e.httpForProxy()
	if err != nil {
		return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: err}
	}
	data, enc, err := postRegisterWithRetry(ctx, client, plain, nativeUserAgentForState(state, input.AppVersion), state.Attestation)
	state.LastRegister = sanitizeResponse(data)
	if routingInfo := chatRoutingInfoFromValue(data["edge_routing_info"]); routingInfo != "" {
		state.ChatRoutingInfo = routingInfo
	}
	if enc != "" {
		state.LastRegister["enc_sha256"] = encHash(enc)
	}
	if err != nil {
		_ = e.saveState(ctx, input.ClientProfileID, state)
		return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: classifyHTTPError(data, err)}
	}
	if status := responseStatus(data); status != "ok" && status != "registered" {
		if input.DeliveryMethod == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER && !accountTransferRegisterTerminalFailure(data) {
			_ = e.saveState(ctx, input.ClientProfileID, state)
			return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_SUBMITTED, Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "account transfer confirmation is pending", true)}
		}
		if input.DeliveryMethod == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
			state.AccountTransfer = nativeAccountTransferState{}
		}
		_ = e.saveState(ctx, input.ClientProfileID, state)
		return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: waProtocolError(data, "registration was rejected")}
	}
	login := firstNonEmpty(jsonString(data["login"]), jsonString(data["jid"]), jsonString(data["registration_jid"]), state.CC+state.Phone)
	lid := firstNonEmpty(jsonString(data["lid"]), login)
	if login != "" {
		state.RegistrationJID = normalizeJID(login)
	}
	if input.DeliveryMethod == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
		state.AccountTransfer = nativeAccountTransferState{}
	}
	_ = e.saveState(ctx, input.ClientProfileID, state)
	completedAt := e.clock.Now()
	return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED, RegisteredID: "waid_" + stableID(login), ServiceAccountID: lid, ServiceLoginID: login, CompletedAt: completedAt}
}

func postRegisterWithRetry(ctx context.Context, client *nativeHTTPClient, plain string, userAgent string, attestation nativeSoftwareAttestation) (map[string]any, string, error) {
	const maxRegisterRetries = 2
	data, enc, err := client.postWASafe(ctx, defaultWARegisterURL, plain, userAgent, attestation)
	for attempt := 1; err != nil && retryableRegisterHTTPFailure(data, err) && attempt <= maxRegisterRetries; attempt++ {
		log.Printf(
			"wa_registration_register_retry status=scheduled attempt=%d http_status=%d wa_status=%s wa_reason=%s",
			attempt,
			int(jsonNumber(data["status_code"])),
			probeLogValue(responseStatus(data)),
			probeLogValue(responseReason(data)),
		)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return data, enc, ctxErr
		}
		data, enc, err = client.postWASafe(ctx, defaultWARegisterURL, plain, userAgent, attestation)
		if err == nil {
			log.Printf(
				"wa_registration_register_retry status=accepted attempt=%d http_status=%d wa_status=%s wa_reason=%s",
				attempt,
				int(jsonNumber(data["status_code"])),
				probeLogValue(responseStatus(data)),
				probeLogValue(responseReason(data)),
			)
		} else {
			log.Printf(
				"wa_registration_register_retry status=failed attempt=%d http_status=%d wa_status=%s wa_reason=%s retryable=%t",
				attempt,
				int(jsonNumber(data["status_code"])),
				probeLogValue(responseStatus(data)),
				probeLogValue(responseReason(data)),
				retryableRegisterHTTPFailure(data, err),
			)
		}
	}
	return data, enc, err
}

func retryableRegisterHTTPFailure(data map[string]any, err error) bool {
	if err == nil {
		return false
	}
	statusCode := int(jsonNumber(data["status_code"]))
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

func (e *NativeEngine) CheckLoginState(ctx context.Context, input EngineLoginCheckInput) EngineLoginCheckResult {
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return EngineLoginCheckResult{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_INVALID, Err: err}
	}
	proxyURL, err := e.proxyURL()
	if err != nil {
		return EngineLoginCheckResult{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNSPECIFIED, Err: err}
	}
	timeout := defaultChatdReadWindow
	if input.RemoteTimeout > 0 {
		timeout = input.RemoteTimeout
	}
	client := newChatdClient(chatdConfigForState(proxyURL, state, timeout))
	update, err := client.checkLoginState(ctx, state, input, input.AppVersion)
	if applyChatdSessionUpdateState(&state, update) {
		_ = e.saveState(ctx, input.ClientProfileID, state)
	}
	if err != nil {
		status := loginCheckStatusForError(err)
		message := "login state remote check failed"
		if snippet := chatdSafeFailureMessage(err); snippet != "" {
			message += ": " + snippet
		}
		return EngineLoginCheckResult{Status: status, Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, message, status == waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE)}
	}
	return EngineLoginCheckResult{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE}
}

func loginCheckStatusForError(err error) waappv1.LoginStateCheckStatus {
	if err == nil {
		return waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{"timeout", "deadline", "proxy", "dial", "connect", "network", "tls", "no such host", "connection refused", "temporary"} {
		if strings.Contains(lower, marker) {
			return waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE
		}
	}
	return waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_INVALID
}

func (e *NativeEngine) ReceiveMessageBatch(ctx context.Context, input EngineMessageInput) EngineMessageBatchResult {
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return EngineMessageBatchResult{Err: err}
	}
	state.ensureMaps()
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = ensureChatStatic(state.ChatStatic)
		_ = e.saveState(ctx, input.ClientProfileID, state)
	}
	proxyURL, err := e.proxyURL()
	if err != nil {
		return EngineMessageBatchResult{Err: err}
	}
	client := newChatdClient(chatdConfigForState(proxyURL, state, 0))
	now := e.clock.Now()
	messages, payloads, otps, update, err := client.receiveBatch(ctx, state, input, input.AppVersion, now)
	if err != nil {
		return EngineMessageBatchResult{Err: chatdReceiveError(err)}
	}
	if applyChatdReceiveState(&state, input, payloads, update) {
		_ = e.saveState(ctx, input.ClientProfileID, state)
	}
	return EngineMessageBatchResult{Messages: messages, Contacts: contactsFromContactHints(input.WAAccountID, nil, update.ContactHints, now), OTPMessages: otps}
}

func applyChatdReceiveState(state *nativeState, input EngineMessageInput, payloads []chatdEncPayload, update chatdSessionUpdate) bool {
	if state == nil {
		return false
	}
	changed := false
	state.ensureMaps()
	for _, payload := range payloads {
		ref := payloadRefForEnc(input.WAAccountID, payload.Payload)
		state.MessagePayloads[ref] = nativeMessagePayload{
			Contact:             payload.Contact,
			Sender:              payload.Sender,
			ContactPN:           payload.ContactPN,
			SenderPN:            payload.SenderPN,
			NotifyName:          payload.NotifyName,
			ParticipantUsername: payload.ParticipantUsername,
			ContactHints:        dedupeWAContactHints(payload.ContactHints),
			EncType:             payload.EncType,
			Path:                payload.Path,
			Payload:             b64u(payload.Payload),
		}
		changed = true
	}
	if len(update.ContactHints) > 0 {
		state.ContactHints = dedupeWAContactHints(append(state.ContactHints, update.ContactHints...))
		changed = true
	}
	if applyChatdSessionUpdateState(state, update) {
		changed = true
	}
	return changed
}

func applyChatdSessionUpdateState(state *nativeState, update chatdSessionUpdate) bool {
	changed := false
	if applyChatdConnectionState(state, update) {
		changed = true
	}
	if applyPrivacyTokenUpdates(state, update.PrivacyTokens) {
		changed = true
	}
	return changed
}

func mergeChatdSessionUpdate(current chatdSessionUpdate, next chatdSessionUpdate) chatdSessionUpdate {
	if next.RoutingInfo != "" {
		current.RoutingInfo = next.RoutingInfo
	}
	if next.Endpoint.Host != "" {
		current.Endpoint = next.Endpoint
	}
	if next.ServerStaticPublic != "" {
		current.ServerStaticPublic = next.ServerStaticPublic
	}
	if len(next.ContactHints) > 0 {
		current.ContactHints = append(current.ContactHints, next.ContactHints...)
	}
	if len(next.PrivacyTokens) > 0 {
		current.PrivacyTokens = dedupePrivacyTokenUpdates(append(current.PrivacyTokens, next.PrivacyTokens...))
	}
	return current
}

func hasChatdSessionUpdate(update chatdSessionUpdate) bool {
	return update.RoutingInfo != "" || update.Endpoint.Host != "" || update.ServerStaticPublic != "" || len(update.ContactHints) > 0 || len(update.PrivacyTokens) > 0
}

func applyChatdConnectionState(state *nativeState, update chatdSessionUpdate) bool {
	if state == nil {
		return false
	}
	changed := false
	if update.RoutingInfo != "" && state.ChatRoutingInfo != update.RoutingInfo {
		state.ChatRoutingInfo = update.RoutingInfo
		changed = true
	}
	if update.Endpoint.Host != "" && (state.ChatConnection.LastHost != update.Endpoint.Host || state.ChatConnection.LastPort != update.Endpoint.Port) {
		state.ChatConnection.LastHost = update.Endpoint.Host
		state.ChatConnection.LastPort = update.Endpoint.Port
		changed = true
	}
	if update.ServerStaticPublic != "" && state.ChatConnection.ServerStaticPublic != update.ServerStaticPublic {
		state.ChatConnection.ServerStaticPublic = update.ServerStaticPublic
		changed = true
	}
	return changed
}

func chatRoutingInfoFromValue(value any) string {
	switch typed := value.(type) {
	case []byte:
		if len(typed) == 0 || len(typed) > 256 {
			return ""
		}
		return b64u(typed)
	case string:
		return normalizeChatRoutingInfo(typed)
	default:
		return ""
	}
}

func chatdReceiveError(err error) error {
	message := "native chatd receive failed"
	if snippet := chatdSafeFailureMessage(err); snippet != "" {
		message += ": " + snippet
	}
	return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, message, true)
}

func chatdSafeFailureMessage(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	phase := chatdFailurePhase(text)
	reason := chatdFailureReason(text)
	switch {
	case phase != "" && reason != "":
		return phase + ": " + reason
	case phase != "":
		return phase
	default:
		return safeResponseSnippet(upstreamFailureMessage(nil, err))
	}
}

func chatdFailurePhase(text string) string {
	for _, phase := range []string{
		"prepare chatd static identity",
		"resolve chatd login identity",
		"decode chatd routing info",
		"chatd dial",
		"chatd encode routing info",
		"chatd write routing info",
		"chatd write noise prologue",
		"chatd flush noise prologue",
		"chatd generate ephemeral",
		"chatd write client hello",
		"chatd read server hello",
		"chatd parse server hello",
		"chatd mix ee",
		"chatd decrypt server static",
		"chatd mix es",
		"chatd decrypt server payload",
		"chatd encrypt client static",
		"chatd mix se",
		"chatd encrypt login payload",
		"chatd write client finish",
		"chatd noise handshake",
		"chatd ping write",
		"chatd frame read",
		"chatd ack write",
		"chatd iq write",
		"chatd iq read",
		"chatd message write",
		"chatd message read",
	} {
		if strings.Contains(text, phase) {
			return phase + " failed"
		}
	}
	return ""
}

func chatdFailureReason(text string) string {
	switch {
	case strings.Contains(text, "connection reset by peer"):
		return "connection reset by peer"
	case strings.Contains(text, "eof"):
		return "EOF"
	case strings.Contains(text, "i/o timeout") || strings.Contains(text, "deadline") || strings.Contains(text, "timeout"):
		return "timeout"
	case strings.Contains(text, "server returned goa"):
		return "server returned GOA"
	case strings.Contains(text, "message authentication failed") || strings.Contains(text, "authentication failed") || strings.Contains(text, "cipher"):
		return "decrypt authentication failed"
	case strings.Contains(text, "invalid list-size token"):
		return chatdNodeTokenFailureReason(text, "invalid list-size token")
	case strings.Contains(text, "readstring could not match token"):
		return chatdNodeTokenFailureReason(text, "unsupported node string token")
	case strings.Contains(text, "unexpected end of binary node"):
		return "unexpected end of binary node"
	case strings.Contains(text, "truncated"):
		return "truncated binary node"
	case strings.Contains(text, "fragmented stanza"):
		return "fragmented stanza"
	case strings.Contains(text, "zlib") || strings.Contains(text, "flate"):
		return "compressed stanza decode failed"
	case strings.Contains(text, "server static public length"):
		return "invalid server static"
	case strings.Contains(text, "server ephemeral length"):
		return "invalid server ephemeral"
	case strings.Contains(text, "no such host"):
		return "DNS lookup failed"
	case strings.Contains(text, "socks5 connect failed"):
		return "SOCKS5 connect failed"
	case strings.Contains(text, "proxy rejected"):
		return "proxy rejected"
	case strings.Contains(text, "tls"):
		return "TLS handshake failed"
	default:
		return ""
	}
}

func chatdNodeTokenFailureReason(text string, fallback string) string {
	match := chatdNodeTokenErrorPattern.FindStringSubmatch(text)
	if len(match) < 3 {
		return fallback
	}
	return fallback + " " + match[2]
}

func (e *NativeEngine) DecryptMessage(ctx context.Context, input EngineDecryptInput) EngineDecryptResult {
	_ = ctx
	if strings.HasPrefix(input.PayloadRef, "plaintext:") {
		plain := strings.TrimPrefix(input.PayloadRef, "plaintext:")
		decryptedID := e.ids.NewID("wadec_")
		text := &waappv1.SensitiveText{RedactedValue: redacted(plain), SecretRef: "plaintext:" + decryptedID}
		if input.IncludePlaintextText {
			text.Value = plain
		}
		msg := &waappv1.DecryptedMessage{DecryptedMessageId: decryptedID, MessageId: input.MessageID, Status: waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED, PlaintextRef: "inline:" + decryptedID, PlaintextText: text, DecryptedAt: timestamppb.New(e.clock.Now())}
		return EngineDecryptResult{DecryptedMessage: msg, Candidates: extractCandidates(input.MessageID, decryptedID, plain, input.IncludePlaintextText, e.clock.Now(), e.ids)}
	}
	if strings.HasPrefix(input.PayloadRef, "native-enc:") {
		state, err := e.loadState(ctx, input.ClientProfileID)
		if err != nil {
			return EngineDecryptResult{Err: err}
		}
		payload, ok := state.MessagePayloads[input.PayloadRef]
		if !ok {
			return EngineDecryptResult{Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "encrypted message payload ref not found", false)}
		}
		commit := input.SessionCommitPolicy == waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE
		output, err := decryptNativeSignalPayload(&state, payload, commit)
		if err != nil {
			return EngineDecryptResult{Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_DECRYPTION_FAILED, "native Signal message decryption failed", true)}
		}
		if commit {
			_ = applyNativeAppStateKeys(&state, output.plaintext)
			_ = e.saveState(ctx, input.ClientProfileID, state)
		}
		decryptedID := e.ids.NewID("wadec_")
		plain := nativePlaintextText(output.plaintext)
		text := &waappv1.SensitiveText{RedactedValue: redacted(plain), SecretRef: "native-plain:" + decryptedID}
		if input.IncludePlaintextText {
			text.Value = plain
		}
		msg := &waappv1.DecryptedMessage{DecryptedMessageId: decryptedID, MessageId: input.MessageID, Status: waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED, PlaintextRef: "native-plain:" + decryptedID, PlaintextText: text, DecryptedAt: timestamppb.New(e.clock.Now())}
		contactHints := nativeContactHints(output.plaintext)
		contactHints = append(contactHints, nativeAppStateContactHints(&state, output.plaintext)...)
		contactHints = append(contactHints, contactHintsFromNativePayloadMetadata(payload)...)
		return EngineDecryptResult{DecryptedMessage: msg, Candidates: extractCandidates(input.MessageID, decryptedID, plain, input.IncludePlaintextText, e.clock.Now(), e.ids), ContactHints: dedupeWAContactHints(contactHints)}
	}
	return EngineDecryptResult{Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "payload ref scheme is not supported by native decryptor", false)}
}

func nativePlaintextText(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if text, ok := nativeMessageDisplayText(raw); ok {
		return text
	}
	plain := string(raw)
	if text := waJSONDisplayText(plain); text != "" {
		return text
	}
	if readableText(plain) {
		return strings.TrimSpace(plain)
	}
	return nativePrintableDisplayText(raw)
}

func readableText(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return false
	}
	total := 0
	readable := 0
	for _, r := range value {
		total++
		if r == '\n' || r == '\r' || r == '\t' || (r >= 0x20 && r != 0x7f) {
			readable++
		}
	}
	return total > 0 && readable*100/total >= 90
}

func printableSegments(raw []byte) []string {
	segments := []string{}
	var current strings.Builder
	flush := func() {
		value := strings.TrimSpace(current.String())
		current.Reset()
		if len(value) >= 4 {
			segments = append(segments, value)
		}
	}
	for _, b := range raw {
		if b == '\n' || b == '\r' || b == '\t' || (b >= 0x20 && b <= 0x7e) {
			current.WriteByte(b)
			continue
		}
		flush()
	}
	flush()
	if len(segments) > 32 {
		return segments[:32]
	}
	return segments
}

func omitEmptyNativeOperatorField(key string, value string) bool {
	if strings.TrimSpace(value) != "" {
		return false
	}
	switch key {
	case "mcc", "mnc", "sim_mcc", "sim_mnc":
		return true
	default:
		return false
	}
}

func (e *NativeEngine) registerParams(phone *waappv1.PhoneTarget, method waappv1.VerificationDeliveryMethod, code string, state nativeState, authCodeContext string) (map[string]string, map[string]struct{}) {
	methodName := firstNonEmpty(state.LastCodeParams["method"], registrationMethodName(method, "sms"))
	lg, lc := registrationLocale(phone)
	params := map[string]string{
		"cc":                phoneCC(phone),
		"in":                phoneNational(phone),
		"method":            methodName,
		"lg":                firstNonEmpty(state.LastCodeParams["lg"], lg),
		"lc":                firstNonEmpty(state.LastCodeParams["lc"], lc),
		"fdid":              firstNonEmpty(state.LastCodeParams["fdid"], state.Profile.FDID),
		"expid":             firstNonEmpty(state.LastCodeParams["expid"], state.Profile.ExpID),
		"access_session_id": firstNonEmpty(state.LastCodeParams["access_session_id"], state.Profile.AccessSessionID),
		"id":                nativeRegistrationRequestID(state),
		"backup_token":      firstNonEmpty(state.LastCodeParams["backup_token"], state.Profile.BackupToken),
		"code":              code,
		"authkey":           firstNonEmpty(state.LastCodeParams["authkey"], state.AuthKey),
		"e_ident":           firstNonEmpty(state.LastCodeParams["e_ident"], state.KeyBundle.IdentityPublic),
		"e_keytype":         firstNonEmpty(state.LastCodeParams["e_keytype"], state.KeyBundle.KeyType),
		"e_regid":           firstNonEmpty(state.LastCodeParams["e_regid"], state.KeyBundle.RegID),
		"e_skey_id":         firstNonEmpty(state.LastCodeParams["e_skey_id"], state.KeyBundle.SignedKeyID),
		"e_skey_val":        firstNonEmpty(state.LastCodeParams["e_skey_val"], state.KeyBundle.SignedKeyValue),
		"e_skey_sig":        firstNonEmpty(state.LastCodeParams["e_skey_sig"], state.KeyBundle.SignedKeySig),
	}
	if nativeRegistrationMethodUsesToken(methodName) {
		if token := e.registrationToken(phone, state); token != "" {
			params["token"] = token
		}
	}
	if nativeRegistrationMethodUsesAuthContext(methodName) {
		if contextValue := firstNonEmpty(authCodeContext, state.LastCodeParams["context"]); contextValue != "" {
			params["context"] = contextValue
		}
	}
	if methodName != "acc_tr" {
		applyRegisterCodeResultParams(params, state)
	}
	raw := map[string]struct{}{"id": {}, "backup_token": {}}
	applyNativeRawParamMap(params, raw, registerDeviceMap(methodName, state), true)
	return params, raw
}

func applyRegisterCodeResultParams(params map[string]string, state nativeState) {
	for _, key := range []string{"auth_response", "context", "advertising_id", "login", "type"} {
		value := jsonString(state.LastCodeResult[key])
		if value == "" {
			continue
		}
		params[key] = value
	}
}

func (e *NativeEngine) loadState(ctx context.Context, clientProfileID string) (nativeState, error) {
	state, err := e.stateStore.GetNativeState(ctx, clientProfileID)
	if err != nil {
		return nativeState{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_PROFILE_NOT_FOUND, "native client profile state not found", false)
	}
	return state, nil
}

func (e *NativeEngine) newState(phone *waappv1.PhoneTarget) (nativeState, error) {
	return newNativeState(phone)
}

func (e *NativeEngine) saveState(ctx context.Context, clientProfileID string, state nativeState) error {
	return e.stateStore.SaveNativeState(ctx, clientProfileID, state)
}

func sanitizeResponse(data map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range data {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "key") || strings.Contains(lower, "auth") || strings.Contains(lower, "code") || strings.Contains(lower, "sig") || strings.Contains(lower, "routing") {
			out[key] = "<redacted>"
			continue
		}
		if lower == "response_text" {
			out[key] = safeResponseSnippet(jsonString(value))
			continue
		}
		out[key] = value
	}
	return out
}

func verificationCodeResult(status waappv1.VerificationRequestStatus, data map[string]any, method waappv1.VerificationDeliveryMethod, now time.Time, retryAfter time.Duration) EngineCodeResult {
	return EngineCodeResult{
		Status:             status,
		ExpectedCodeLength: int32(jsonNumber(data["length"])),
		ExpiresAt:          now.Add(10 * time.Minute),
		RetryAfter:         retryAfter,
		MethodStatuses:     verificationCodeMethodStatuses(data, method),
		RawStatus:          responseStatus(data),
		RawReason:          responseReason(data),
	}
}

func verificationCodeRejectedResult(data map[string]any, method waappv1.VerificationDeliveryMethod, now time.Time, retryAfter time.Duration, fallback string) EngineCodeResult {
	return EngineCodeResult{
		Status:             waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED,
		ExpectedCodeLength: int32(jsonNumber(data["length"])),
		ExpiresAt:          now.Add(10 * time.Minute),
		RetryAfter:         retryAfter,
		MethodStatuses:     verificationCodeMethodStatuses(data, method),
		RawStatus:          responseStatus(data),
		RawReason:          responseReason(data),
		Err:                waProtocolError(data, fallback),
	}
}

func verificationCodeRateLimited(data map[string]any) bool {
	switch responseStatus(data) {
	case "too_recent", "too_many", "temporarily_unavailable":
		return true
	}
	switch responseReason(data) {
	case "too_recent", "too_many", "temporarily_unavailable":
		return true
	default:
		return false
	}
}

func verificationCodeRetryAfter(data map[string]any, method waappv1.VerificationDeliveryMethod) time.Duration {
	seconds := verificationMethodWaitStatus(data, registrationMethodName(method, "sms"), true).Seconds
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func classifyHTTPError(data map[string]any, err error) error {
	status := responseStatus(data)
	switch status {
	case "no_routes":
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "no_routes: verification route is unavailable", false)
	case "too_recent":
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, "verification request is too recent", true)
	case "blocked", "rejected":
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, status+": request was rejected", false)
	}
	return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, upstreamFailureMessage(data, err), true)
}

func jsonString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func upstreamFailureMessage(data map[string]any, err error) string {
	if statusCode := jsonNumber(data["status_code"]); statusCode > 0 {
		if snippet := safeResponseSnippet(jsonString(data["response_text"])); snippet != "" {
			return fmt.Sprintf("wasafe upstream http %d: %s", statusCode, snippet)
		}
		return fmt.Sprintf("wasafe upstream http %d", statusCode)
	}
	return err.Error()
}

func safeResponseSnippet(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return ""
	}
	text = nativeSensitiveDigitsPattern.ReplaceAllString(text, "<digits>")
	for _, marker := range []string{"token", "auth", "key", "code", "sig"} {
		if strings.Contains(strings.ToLower(text), marker) {
			return "<redacted>"
		}
	}
	if utf8.RuneCountInString(text) <= 160 {
		return text
	}
	out := make([]rune, 0, 160)
	for _, ch := range text {
		if len(out) >= 160 {
			break
		}
		out = append(out, ch)
	}
	return string(out) + "..."
}

func jsonNumber(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func extractCandidates(messageID string, decryptedID string, text string, includeValue bool, now time.Time, ids IDGenerator) []*waappv1.ExtractedCandidate {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	patterns := []struct {
		kind      waappv1.CandidateKind
		re        *regexp.Regexp
		normalize func(string) string
	}{
		{kind: waappv1.CandidateKind_CANDIDATE_KIND_FLAG, re: regexp.MustCompile(`(?i)(flag|ctf)\{[^\s}]{1,120}\}`)},
		{kind: waappv1.CandidateKind_CANDIDATE_KIND_OTP, re: regexp.MustCompile(`\b\d{4,8}\b`), normalize: digitsOnly},
		{kind: waappv1.CandidateKind_CANDIDATE_KIND_OTP, re: regexp.MustCompile(`\b\d{3}[-\s]\d{3}\b`), normalize: digitsOnly},
	}
	out := []*waappv1.ExtractedCandidate{}
	seen := map[string]struct{}{}
	for _, pattern := range patterns {
		for _, match := range pattern.re.FindAllString(text, -1) {
			value := match
			if pattern.normalize != nil {
				value = pattern.normalize(match)
			}
			if value == "" {
				continue
			}
			key := pattern.kind.String() + ":" + value
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidateID := ids.NewID("wacand_")
			sensitive := &waappv1.SensitiveText{RedactedValue: redacted(value), SecretRef: "candidate:" + candidateID}
			if includeValue {
				sensitive.Value = value
			}
			out = append(out, &waappv1.ExtractedCandidate{CandidateId: candidateID, MessageId: messageID, DecryptedMessageId: decryptedID, Kind: pattern.kind, Text: sensitive, Confidence: 0.9, ExtractedAt: timestamppb.New(now)})
		}
	}
	return out
}
