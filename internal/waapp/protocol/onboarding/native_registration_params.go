package onboarding

import (
	"context"
	"strconv"
	"strings"

	"github.com/nyaruka/phonenumbers"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

// registrationLocale 按号码国别派生注册 locale。lc(设备 region)优先采信号码自带的
// country_iso2;否则取 libphonenumber 的 E.164→ISO-3166-1 alpha-2 权威映射(真源在标准
// 数据集,非手编国别表)。lg 取 "en" —— 英文 UI 全球通用,en + 本地 region 自洽。
// 无法解析国别时退回 en/US。
func RegistrationLocale(phone *waappv1.PhoneTarget) (lg string, lc string) {
	return "en", RegistrationDeviceCountryISO(phone)
}

func RegistrationDeviceCountryISO(phone *waappv1.PhoneTarget) string {
	if iso := strings.ToUpper(strings.TrimSpace(phone.GetCountryIso2())); len(iso) == 2 {
		return iso
	}
	code, err := strconv.Atoi(shared.PhoneCC(phone))
	if err != nil || code <= 0 {
		return "US"
	}
	if region := phonenumbers.GetRegionCodeForCountryCode(code); region != "" && region != "ZZ" {
		return region
	}
	return "US"
}

// ExistOrderedParamsWithWamsys builds the selected Android Kotlin exist request.
func (e *engineCore) ExistOrderedParamsWithWamsys(ctx context.Context, phone *waappv1.PhoneTarget, state pstate.NativeState, wamsysCapture *protocolv1.WamsysCapture, includeWamsys bool, appVersion string) (OrderedParams, error) {
	fields := NativeDeviceMapFields(state)
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
	params.Set("id", pstate.NativeRegistrationRequestID(state), true)
	params.Set("backup_token", state.Profile.BackupToken, true)
	if advertisingID := pstate.NativeAdvertisingID(state); advertisingID != "" {
		params.Set("advertising_id", advertisingID, false)
	}
	SetNonEmptyFormParam(&params, "login", e.verification.GetLogin())
	params.Set("type", "0", false)
	if err := ApplyNativeE2EParams(&params, state); err != nil {
		return nil, err
	}

	if token := e.token; token != "" {
		params.Set("token", token, false)
	}
	AddRawParam(&params, "mistyped", "7")
	AddRawParam(&params, "offline_ab", `{"exposure":[],"exp_hash":[],"metrics":{}}`)
	AddRawParam(&params, "client_metrics", `{"attempts":1,"app_campaign_download_source":"unknown|unknown","was_activated_from_stub":false}`)
	AddRawParam(&params, "read_phone_permission_granted", "0")
	AddRawParam(&params, "sim_state", "1")
	AddOptionalRawParam(&params, "network_operator_name", fields["network_operator_name"])
	AddOptionalRawParam(&params, "sim_operator_name", fields["sim_operator_name"])
	AddRawParam(&params, "device_name", state.Client.GetDevice())
	AddOptionalRawParam(&params, "backup_token_error", fields["backup_token_error"])
	AddOptionalRawParam(&params, "feo2_query_status", fields["feo2_query_status"])
	AddRawParam(&params, "is_foa_fdid_app_installed", "false")
	AddOptionalRawParam(&params, "device_ram", fields["device_ram"])
	AddRawParam(&params, "language_selector_time_spent", "0")
	AddRawParam(&params, "language_selector_clicked_count", "0")
	capture, err := e.RegistrationCapture(ctx, protocolv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_EXIST, phone, state, wamsysCapture, includeWamsys, appVersion)
	if err != nil {
		return nil, err
	}
	if NativeShouldSendRegistrationGPIA(state) {
		ApplyOrderedWamsysKey(&params, capture, "gpia")
	}
	ApplyAndroidADBState(&params, e.parameters.GetOptions(), state)
	AddOptionalRawParam(&params, "recaptcha", fields["recaptcha"])
	AddOptionalRawParam(&params, "network_radio_type", fields["network_radio_type"])
	AddOptionalRawParam(&params, "simnum", fields["simnum"])
	AddOptionalRawParam(&params, "hasinrc", fields["hasinrc"])
	AddOptionalRawParam(&params, "pid", fields["pid"])
	AddOptionalRawParam(&params, "rc", fields["rc"])
	AddOptionalRawParam(&params, "sim_type", fields["sim_type"])
	AddOptionalRawParam(&params, "airplane_mode_type", fields["airplane_mode_type"])
	AddOptionalRawParam(&params, "cellular_strength", fields["cellular_strength"])
	AddOptionalRawParam(&params, "roaming_type", fields["roaming_type"])
	AddOptionalRawParam(&params, "tos_version", fields["tos_version"])
	ApplyOrderedWamsysExcept(&params, capture, map[string]struct{}{"gpia": {}})
	return params, nil
}

// registrationCapture fetches the WAMSYS integrity material for a registration
// request kind. Shared by the /v2/exist, /v2/code and /v2/register ordered
// builders so the provider call lives in one place; returns nil when the caller
// opts out of the WAMSYS map.
func (e *engineCore) RegistrationCapture(ctx context.Context, kind protocolv1.RegistrationRequestKind, phone *waappv1.PhoneTarget, state pstate.NativeState, wamsysCapture *protocolv1.WamsysCapture, includeWamsys bool, appVersion string) (*protocolv1.WamsysCapture, error) {
	if !includeWamsys {
		return nil, nil
	}
	return e.WamsysProvider().RegistrationMaterial(ctx, pstate.WamsysMaterialInput{Capture: wamsysCapture, Kind: kind, Phone: phone, State: state, AppVersion: appVersion, Build: e.build, Client: e.parameters, Now: e.clock.Now()})
}

func (e *engineCore) CodeRequestOrderedParams(ctx context.Context, phone *waappv1.PhoneTarget, method waappv1.VerificationDeliveryMethod, state pstate.NativeState, authCodeContext string, appVersion string) (OrderedParams, error) {
	return e.CodeRequestOrderedParamsWithWamsys(ctx, phone, method, state, authCodeContext, nil, true, appVersion)
}

func (e *engineCore) CodeRequestOrderedParamsWithWamsys(ctx context.Context, phone *waappv1.PhoneTarget, method waappv1.VerificationDeliveryMethod, state pstate.NativeState, authCodeContext string, wamsysCapture *protocolv1.WamsysCapture, includeWamsys bool, appVersion string) (OrderedParams, error) {
	methodName := RegistrationMethodName(method, "sms")
	fields := NativeDeviceMapFields(state)
	attempts := NativeCodeRequestAttempts(state)
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
	params.Set("id", pstate.NativeRegistrationRequestID(state), true)
	params.Set("backup_token", state.Profile.BackupToken, true)
	if token := e.token; token != "" {
		params.Set("token", token, false)
	}
	params.Set("method", methodName, false)
	if NativeRegistrationMethodUsesAuthContext(methodName) {
		if contextValue := strings.TrimSpace(authCodeContext); contextValue != "" {
			params.Set("context", contextValue, false)
		}
	}
	ApplyAndroidCodeOptions(&params, e.parameters.GetOptions())
	if advertisingID := pstate.NativeAdvertisingID(state); advertisingID != "" {
		params.Set("advertising_id", advertisingID, false)
	}
	SetNonEmptyFormParam(&params, "login", e.verification.GetLogin())
	params.Set("type", "0", false)
	if err := ApplyNativeE2EParams(&params, state); err != nil {
		return nil, err
	}
	ApplyNativeCodeRequestMapParams(&params, fields, methodName, attempts, NativeCodeRequestReason(state))

	AddOptionalRawParam(&params, "old_phone_number", fields["old_phone_number"])
	AddOptionalRawParam(&params, "device_ram", fields["device_ram"])
	capture, err := e.RegistrationCapture(ctx, protocolv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_CODE, phone, state, wamsysCapture, includeWamsys, appVersion)
	if err != nil {
		return nil, err
	}

	if NativeShouldSendRegistrationGPIA(state) {
		ApplyOrderedWamsysKey(&params, capture, "gpia")
	}
	ApplyAndroidADBState(&params, e.parameters.GetOptions(), state)
	ApplyNativeCodeRequestTailParams(&params, fields)

	ApplyOrderedWamsysExcept(&params, capture, map[string]struct{}{"gpia": {}})
	AddOptionalRawParam(&params, "feo2_query_status", fields["feo2_query_status"])
	return params, nil
}

// ApplyNativeCodeRequestTailParams appends observed installation and PAA fields.
func ApplyNativeCodeRequestTailParams(params *OrderedParams, fields map[string]string) {
	AddOptionalRawParam(params, "recaptcha", fields["recaptcha"])
	AddOptionalRawParam(params, "fid", fields["fid"])
	AddOptionalRawParam(params, "preloads_app_manager_id", fields["preloads_app_manager_id"])
	AddOptionalRawParam(params, "preloads_attribution", fields["preloads_attribution"])
	AddOptionalRawParam(params, "tos_version", fields["tos_version"])
	AddOptionalRawParam(params, "create_paa", fields["create_paa"])
	AddOptionalRawParam(params, "entrypoint", fields["entrypoint"])
	AddOptionalRawParam(params, "suma", fields["suma"])
	AddOptionalRawParam(params, "cred_token", fields["cred_token"])
}

func (e *engineCore) RegisterOrderedParams(ctx context.Context, phone *waappv1.PhoneTarget, method waappv1.VerificationDeliveryMethod, code string, state pstate.NativeState, authCodeContext string, appVersion string) (OrderedParams, error) {
	return e.RegisterOrderedParamsWithWamsys(ctx, phone, method, code, state, authCodeContext, nil, true, appVersion)
}

// RegisterOrderedParamsWithWamsys follows KotlinRegistrationBridge.A07.
func (e *engineCore) RegisterOrderedParamsWithWamsys(ctx context.Context, phone *waappv1.PhoneTarget, method waappv1.VerificationDeliveryMethod, code string, state pstate.NativeState, authCodeContext string, wamsysCapture *protocolv1.WamsysCapture, includeWamsys bool, appVersion string) (OrderedParams, error) {
	methodName := shared.FirstNonEmpty(state.LastCodeParams["method"], RegistrationMethodName(method, "sms"))
	fields := NativeDeviceMapFields(state)
	lg, lc := RegistrationLocale(phone)
	params := OrderedParams{}
	params.Set("cc", shared.PhoneCC(phone), false)
	params.Set("in", shared.PhoneNational(phone), false)
	params.Set("lg", shared.FirstNonEmpty(state.LastCodeParams["lg"], lg), false)
	params.Set("lc", shared.FirstNonEmpty(state.LastCodeParams["lc"], lc), false)
	params.Set("fdid", shared.FirstNonEmpty(state.LastCodeParams["fdid"], state.Profile.FDID), false)
	params.Set("expid", shared.FirstNonEmpty(state.LastCodeParams["expid"], state.Profile.ExpID), false)
	if value := shared.FirstNonEmpty(state.LastCodeParams["access_session_id"], state.Profile.AccessSessionID); value != "" {
		params.Set("access_session_id", value, false)
	}
	params.Set("id", pstate.NativeRegistrationRequestID(state), true)
	params.Set("backup_token", shared.FirstNonEmpty(state.LastCodeParams["backup_token"], state.Profile.BackupToken), true)
	params.Set("code", code, false)
	SetNonEmptyFormParam(&params, "auth_response", e.verification.GetAuthResponse())
	if NativeRegistrationMethodUsesAuthContext(methodName) {
		if contextValue := shared.FirstNonEmpty(e.verification.GetContext(), authCodeContext, state.LastCodeParams["context"]); contextValue != "" {
			params.Set("context", contextValue, false)
		}
	}
	params.Set("method", methodName, false)
	if advertisingID := shared.FirstNonEmpty(e.verification.GetAdvertisingId(), pstate.NativeAdvertisingID(state)); advertisingID != "" {
		params.Set("advertising_id", advertisingID, false)
	}
	SetNonEmptyFormParam(&params, "login", e.verification.GetLogin())
	params.Set("type", "0", false)
	if err := ApplyNativeE2EParams(&params, state); err != nil {
		return nil, err
	}
	ApplyNativeRegisterRequestMapParams(&params, fields, methodName)

	AddOptionalRawParam(&params, "old_phone_number", fields["old_phone_number"])
	AddOptionalRawParam(&params, "device_ram", fields["device_ram"])
	capture, err := e.RegistrationCapture(ctx, protocolv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_REGISTER, phone, state, wamsysCapture, includeWamsys, appVersion)
	if err != nil {
		return nil, err
	}

	if NativeShouldSendRegistrationGPIA(state) {
		ApplyOrderedWamsysKey(&params, capture, "gpia")
	}
	ApplyAndroidADBState(&params, e.parameters.GetOptions(), state)
	ApplyNativeRegisterRequestTailParams(&params, fields)

	ApplyOrderedWamsysExcept(&params, capture, map[string]struct{}{"gpia": {}})
	return params, nil
}
