package onboarding

import (
	"encoding/json"
	"os"
	"strconv"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func NativeDeviceMapFields(state pstate.NativeState) map[string]string {
	fields := map[string]string{}
	for key, value := range state.Profile.AdditionalMapFields {
		if IsOpaqueWamsysMapKey(key) {
			continue
		}
		if pstate.IsRuntimeNativeDeviceMapKey(key) {
			continue
		}
		fields[key] = value
	}
	for key, value := range NativeDefaultDeviceMapFields() {
		fields[key] = shared.FirstNonEmpty(fields[key], value)
	}
	ApplyNativePreChatdABDeviceFields(fields, state)
	for key, value := range NativeRuntimeDeviceMapFields(state) {
		fields[key] = value
	}
	return fields
}

func NativeRuntimeDeviceMapFields(state pstate.NativeState) map[string]string {
	return map[string]string{
		"pid":               NativeRuntimeProcessID(state),
		"feo2_query_status": pstate.NativeDefaultFeo2QueryStatus,
	}
}

func NativeRuntimeProcessID(state pstate.NativeState) string {
	_ = state
	return strconv.Itoa(os.Getpid())
}

func NativeDefaultDeviceMapFields() map[string]string {
	return map[string]string{
		"network_radio_type":    "1",
		"mistyped":              "7",
		"hasav":                 "2",
		"simnum":                "0",
		"hasinrc":               "1",
		"rc":                    "0",
		"recaptcha":             `{"stage":"ABPROP_DISABLED"}`,
		"feo2_query_status":     pstate.NativeDefaultFeo2QueryStatus,
		"network_operator_name": "",
		"sim_operator_name":     "",
		"mcc":                   "000",
		"mnc":                   "000",
		"sim_mcc":               "000",
		"sim_mnc":               "000",
	}
}

func NativeCodeRequestAttempts(state pstate.NativeState) int {
	if state.GenerateCodeAttempts > 0 {
		return state.GenerateCodeAttempts
	}
	return NativeCodeClientMetricAttempts(pstate.NativeCodeRequestAttemptsFromLastParams(state.LastCodeParams))
}

func NativeCodeRequestReason(state pstate.NativeState) string {
	if len(state.LastCodeResult) == 0 {
		return ""
	}
	switch pstate.ResponseStatus(state.LastCodeResult) {
	case "", "sent", "ok":
		return ""
	default:
		return "server-send-request-error-unspecified"
	}
}

func NativeCodeClientMetricAttempts(attempts int) int {
	if attempts < 1 {
		return 1
	}
	return attempts
}

func NativeCodeClientMetrics(attempts int) string {
	body, err := json.Marshal(struct {
		Attempts                  int    `json:"attempts"`
		AppCampaignDownloadSource string `json:"app_campaign_download_source"`
	}{
		Attempts:                  NativeCodeClientMetricAttempts(attempts),
		AppCampaignDownloadSource: NativeDefaultAppCampaignDownloadSource,
	})
	if err != nil {
		return `{"attempts":1,"app_campaign_download_source":"unknown|unknown"}`
	}
	return string(body)
}

func NativeRegisterClientMetrics(method string) string {
	body, err := json.Marshal(struct {
		Attempts             int    `json:"attempts"`
		VerifyMethod         string `json:"verify_method"`
		WasActivatedFromStub bool   `json:"was_activated_from_stub"`
	}{Attempts: 1, VerifyMethod: shared.FirstNonEmpty(method, "sms"), WasActivatedFromStub: false})
	if err != nil {
		return `{"attempts":1,"verify_method":"sms","was_activated_from_stub":false}`
	}
	return string(body)
}

func NativeCodeEntryMethod(method string) string {
	switch method {
	case "voice", "email_otp":
		return "1"
	default:
		return "2"
	}
}

const NativeDefaultAppCampaignDownloadSource = "unknown|unknown"

func ParseExistProbeResult(data map[string]any) wacore.EngineProbeResult {
	status := pstate.ResponseStatus(data)
	reason := ResponseReason(data)
	methodStatuses := VerificationMethodStatuses(data, nil)
	smsWait := VerificationSMSCooldownSeconds(data)
	smsWaitExhausted := VerificationSMSWaitExhausted(data)
	baseProtocolRejected := ExistProtocolRejected(status, reason)
	blocked := status == "blocked" || reason == "blocked" || ExistConsentBlockedReason(reason)
	invalidNumber := ExistInvalidNumberReason(reason)
	rateLimited := ExistRateLimitedReason(reason)
	consentRequired := !baseProtocolRejected && !blocked && ExistConsentReason(reason)
	challengeRequired := !baseProtocolRejected && !blocked && ExistChallengeReason(reason)
	gated := consentRequired || challengeRequired
	registered := !baseProtocolRejected && !blocked && !invalidNumber && !rateLimited && !gated && (WaOldFallbackEligible(data) || AccountTransferFallbackEligible(data) || ExistRegisteredSignal(status, reason, data))
	protocolRejected := baseProtocolRejected
	notRegistered := !baseProtocolRejected && !blocked && !invalidNumber && !rateLimited && !gated && !registered && ExistNotRegisteredReason(reason)
	registeredKnown := registered || invalidNumber || notRegistered
	canSendSMS := SmsProbeAvailableByCooldownOnly(smsWait, smsWaitExhausted, blocked, protocolRejected, invalidNumber, rateLimited)
	methods := MethodsFromStatuses(methodStatuses)
	reachable := !protocolRejected && !blocked && !invalidNumber && !rateLimited && (ExistReachableStatus(status) || registered || notRegistered || gated)
	accountFlow := ExistAccountFlow(ExistFlowClass{
		ProtocolRejected:  protocolRejected,
		Registered:        registered,
		NotRegistered:     notRegistered,
		Blocked:           blocked,
		InvalidNumber:     invalidNumber,
		RateLimited:       rateLimited,
		ConsentRequired:   consentRequired,
		ChallengeRequired: challengeRequired,
	})
	result := wacore.EngineProbeResult{
		Status:           waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_UNKNOWN,
		AccountFlow:      accountFlow,
		RawStatus:        status,
		RawReason:        reason,
		RegisteredKnown:  registeredKnown,
		Registered:       registered,
		Blocked:          blocked,
		SMSWaitSeconds:   smsWait,
		CanSendSMS:       canSendSMS,
		SupportedMethods: methods,
		MethodStatuses:   methodStatuses,
	}
	switch {
	case protocolRejected:
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED
		result.Err = ExistProtocolError(data)
	case blocked:
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_UNREACHABLE
	case invalidNumber || rateLimited:
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_UNREACHABLE
	case reachable:
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REACHABLE
	}
	return result
}
