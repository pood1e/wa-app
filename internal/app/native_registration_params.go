package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (e *NativeEngine) existParams(phone *waappv1.PhoneTarget, state nativeState) (map[string]string, map[string]struct{}) {
	params := map[string]string{
		"cc":                phoneCC(phone),
		"in":                phoneNational(phone),
		"lg":                "en",
		"lc":                "US",
		"fdid":              state.Profile.FDID,
		"expid":             state.Profile.ExpID,
		"access_session_id": state.Profile.AccessSessionID,
		"id":                nativeRegistrationRequestID(state),
		"backup_token":      state.Profile.BackupToken,
		"authkey":           state.AuthKey,
		"e_ident":           state.KeyBundle.IdentityPublic,
		"e_keytype":         state.KeyBundle.KeyType,
		"e_regid":           state.KeyBundle.RegID,
		"e_skey_id":         state.KeyBundle.SignedKeyID,
		"e_skey_val":        state.KeyBundle.SignedKeyValue,
		"e_skey_sig":        state.KeyBundle.SignedKeySig,
	}
	if token := e.registrationToken(phone, state); token != "" {
		params["token"] = token
	}
	raw := map[string]struct{}{"id": {}, "backup_token": {}}
	applyNativeRawParamMap(params, raw, existDeviceMap(state), true)
	return params, raw
}

func (e *NativeEngine) registrationToken(phone *waappv1.PhoneTarget, state nativeState) string {
	if token := deriveDefaultRegistrationToken(phoneNational(phone)); token != "" {
		return token
	}
	return state.LastCodeParams["token"]
}

func (e *NativeEngine) codeRequestOrderedParams(ctx context.Context, phone *waappv1.PhoneTarget, method waappv1.VerificationDeliveryMethod, state nativeState, authCodeContext string) (orderedParams, error) {
	return e.codeRequestOrderedParamsWithWamsys(ctx, phone, method, state, authCodeContext, nil, true)
}

func (e *NativeEngine) codeRequestOrderedParamsWithWamsys(ctx context.Context, phone *waappv1.PhoneTarget, method waappv1.VerificationDeliveryMethod, state nativeState, authCodeContext string, wamsysCapture *waappv1.WamsysCapture, includeWamsys bool) (orderedParams, error) {
	methodName := registrationMethodName(method, "sms")
	fields := nativeDeviceMapFields(state)
	attempts := nativeCodeRequestAttempts(state)
	params := orderedParams{}
	params.set("cc", phoneCC(phone), false)
	params.set("in", phoneNational(phone), false)
	params.set("lg", "en", false)
	params.set("lc", "US", false)
	params.set("fdid", state.Profile.FDID, false)
	params.set("expid", state.Profile.ExpID, false)
	if state.Profile.AccessSessionID != "" {
		params.set("access_session_id", state.Profile.AccessSessionID, false)
	}
	params.set("id", nativeRegistrationRequestID(state), true)
	params.set("backup_token", state.Profile.BackupToken, true)
	if token := e.registrationToken(phone, state); token != "" {
		params.set("token", token, false)
	}
	params.set("method", methodName, false)
	if nativeRegistrationMethodUsesAuthContext(methodName) {
		if contextValue := strings.TrimSpace(authCodeContext); contextValue != "" {
			params.set("context", contextValue, false)
		}
	}
	if advertisingID := nativeAdvertisingID(state); advertisingID != "" {
		params.set("advertising_id", advertisingID, false)
	}
	applyNativeE2EParams(&params, state)
	applyNativeCodeRequestMapParams(&params, fields, methodName, attempts, nativeCodeRequestReason(state))
	var capture *waappv1.WamsysCapture
	if includeWamsys {
		var err error
		capture, err = e.wamsysProvider().RegistrationMaterial(ctx, wamsysMaterialInput{Capture: wamsysCapture, Kind: waappv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_CODE, Phone: phone, State: state, Now: e.clock.Now()})
		if err != nil {
			return nil, err
		}
	}
	if nativeShouldSendRegistrationGPIA(state) {
		applyOrderedWamsysKey(&params, capture, "gpia")
	}
	applyOrderedWamsysExcept(&params, capture, map[string]struct{}{"gpia": {}})
	addOptionalRawParam(&params, "feo2_query_status", fields["feo2_query_status"])
	addOptionalRawParam(&params, "code_entrypoint", fields["code_entrypoint"])
	return params, nil
}

func nativeRegistrationMethodUsesToken(methodName string) bool {
	return true
}

func nativeRegistrationMethodUsesAuthContext(methodName string) bool {
	return methodName != "acc_tr"
}

func applyNativeE2EParams(params *orderedParams, state nativeState) {
	params.set("authkey", state.AuthKey, false)
	params.set("e_ident", state.KeyBundle.IdentityPublic, false)
	params.set("e_keytype", state.KeyBundle.KeyType, false)
	params.set("e_regid", state.KeyBundle.RegID, false)
	params.set("e_skey_id", state.KeyBundle.SignedKeyID, false)
	params.set("e_skey_val", state.KeyBundle.SignedKeyValue, false)
	params.set("e_skey_sig", state.KeyBundle.SignedKeySig, false)
}

// applyNativeCodeRequestMapParams 装配 /v2/code 的设备 Map，字段集与顺序对齐 APK
// KotlinRegistrationBridge 经 IAo.A0E/A0H/A0L/A0O/A0W 装配的 code 路径 Map。
// 注意：mcc/mnc/sim_mcc/sim_mnc、rc2/rc、airplane_mode_on 等均属 code 路径；
// device_ram/old_phone_number/db/recaptcha/fid/preloads_*/tos_version/entrypoint/cred_token
// 是 exist 路径字段，不在 /v2/code，发送会让请求偏离官方端 shape 触发 no_routes。
func applyNativeCodeRequestMapParams(params *orderedParams, fields map[string]string, method string, attempts int, reason string) {
	addOptionalRawParam(params, "mistyped", fields["mistyped"])
	addRawParam(params, "reason", reason)
	addOptionalRawParam(params, "hasav", fields["hasav"])
	addRawParam(params, "client_metrics", nativeCodeClientMetrics(attempts))
	addOptionalRawParam(params, "mcc", fields["mcc"])
	addOptionalRawParam(params, "mnc", fields["mnc"])
	addOptionalRawParam(params, "sim_mcc", fields["sim_mcc"])
	addOptionalRawParam(params, "sim_mnc", fields["sim_mnc"])
	addRawParam(params, "education_screen_displayed", "false")
	addRawParam(params, "prefer_sms_over_flash", nativePreferSMSOverFlash(method, fields))
	addOptionalRawParam(params, "network_radio_type", fields["network_radio_type"])
	addOptionalRawParam(params, "simnum", fields["simnum"])
	addOptionalRawParam(params, "rc2", fields["rc2"])
	addOptionalRawParam(params, "hasinrc", fields["hasinrc"])
	addOptionalRawParam(params, "pid", fields["pid"])
	addOptionalRawParam(params, "rc", fields["rc"])
	applyNativeCodeRequestSIMSignalParams(params, fields)
	applyNativeCodeRequestStoredParams(params, fields)
}

func applyNativeCodeRequestSIMSignalParams(params *orderedParams, fields map[string]string) {
	addOptionalRawParam(params, "sim_type", fields["sim_type"])
	addOptionalRawParam(params, "airplane_mode_on", fields["airplane_mode_on"])
	addOptionalRawParam(params, "airplane_mode_type", fields["airplane_mode_type"])
	addOptionalRawParam(params, "cellular_strength", fields["cellular_strength"])
	addOptionalRawParam(params, "roaming_type", fields["roaming_type"])
}

func applyNativeCodeRequestStoredParams(params *orderedParams, fields map[string]string) {
	addOptionalRawParam(params, "push_code", fields["push_code"])
	addOptionalRawParam(params, "new_acc_uuid", fields["new_acc_uuid"])
}

func addOptionalRawParam(params *orderedParams, key string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	addRawParam(params, key, value)
}

func addRawParam(params *orderedParams, key string, value string) {
	params.set(key, pctBytes([]byte(value)), true)
}

func registrationMethodName(method waappv1.VerificationDeliveryMethod, fallback string) string {
	switch method {
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS:
		return "sms"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE:
		return "voice"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE,
		waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD:
		return "wa_old"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_FLASH:
		return "flash"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP:
		return "email_otp"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SEND_SMS:
		return "send_sms"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_PASSKEY:
		return "passkey"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH:
		return "silent_auth"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH_TS43:
		return "silent_auth_ts_43"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_AUTOCONF:
		return "autoconf"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DEEPLINK_OTP:
		return "deeplink_otp"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_RECAPTCHA:
		return "recaptcha"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_OAUTH_EMAIL:
		return "oauth_email"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DISCOVERABLE_CREDENTIAL:
		return "discoverable_credential"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER:
		return "acc_tr"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_STANDALONE_APP:
		return "standalone"
	default:
		return fallback
	}
}

func registrationMethodFromName(name string) waappv1.VerificationDeliveryMethod {
	switch verificationMethodCode(name) {
	case "sms":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS
	case "voice":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE
	case "flash":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_FLASH
	case "wa_old":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
	case "email_otp":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP
	case "send_sms":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SEND_SMS
	case "passkey":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_PASSKEY
	case "silent_auth":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH
	case "silent_auth_ts_43":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH_TS43
	case "autoconf":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_AUTOCONF
	case "deeplink_otp":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DEEPLINK_OTP
	case "recaptcha":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_RECAPTCHA
	case "oauth_email":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_OAUTH_EMAIL
	case "discoverable_credential":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DISCOVERABLE_CREDENTIAL
	case "acc_tr":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER
	case "standalone":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_STANDALONE_APP
	default:
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED
	}
}

func applyNativeRawParamMap(params map[string]string, raw map[string]struct{}, values map[string]string, omitEmptyOperator bool) {
	for key, value := range values {
		if isOpaqueWamsysMapKey(key) {
			continue
		}
		if omitEmptyOperator && omitEmptyNativeOperatorField(key, value) {
			continue
		}
		if key == "token" {
			if value != "" {
				params[key] = value
			}
			continue
		}
		params[key] = pctBytes([]byte(value))
		raw[key] = struct{}{}
	}
}

func codeDeviceMap(method string, state nativeState) map[string]string {
	fields := nativeDeviceMapFields(state)
	out := map[string]string{
		"reason":                     nativeCodeRequestReason(state),
		"client_metrics":             nativeCodeClientMetrics(nativeCodeRequestAttempts(state)),
		"education_screen_displayed": "false",
		"prefer_sms_over_flash":      nativePreferSMSOverFlash(method, fields),
		"network_radio_type":         fields["network_radio_type"],
		"simnum":                     fields["simnum"],
		"rc2":                        fields["rc2"],
		"hasinrc":                    fields["hasinrc"],
		"pid":                        fields["pid"],
		"rc":                         fields["rc"],
		"mcc":                        fields["mcc"],
		"mnc":                        fields["mnc"],
		"sim_mcc":                    fields["sim_mcc"],
		"sim_mnc":                    fields["sim_mnc"],
	}
	addNonEmptyNativeCodeField(out, fields, "mistyped")
	addNonEmptyNativeCodeField(out, fields, "hasav")
	for _, key := range []string{
		"sim_type", "airplane_mode_on", "airplane_mode_type", "cellular_strength", "roaming_type",
		"push_code", "new_acc_uuid", "code_entrypoint",
	} {
		addNonEmptyNativeCodeField(out, fields, key)
	}
	return out
}

func nativePreferSMSOverFlash(method string, fields map[string]string) string {
	_ = method
	return firstNonEmpty(fields["prefer_sms_over_flash"], "false")
}

func addNonEmptyNativeCodeField(out map[string]string, fields map[string]string, key string) {
	if value := strings.TrimSpace(fields[key]); value != "" {
		out[key] = value
	}
}

func registerDeviceMap(method string, state nativeState) map[string]string {
	fields := nativeDeviceMapFields(state)
	return map[string]string{
		"mistyped":              "7",
		"client_metrics":        nativeRegisterClientMetrics(method),
		"entered":               nativeCodeEntryMethod(method),
		"mcc":                   fields["mcc"],
		"mnc":                   fields["mnc"],
		"sim_mcc":               fields["sim_mcc"],
		"sim_mnc":               fields["sim_mnc"],
		"network_operator_name": fields["network_operator_name"],
		"sim_operator_name":     fields["sim_operator_name"],
		"network_radio_type":    fields["network_radio_type"],
		"simnum":                fields["simnum"],
		"hasinrc":               fields["hasinrc"],
		"pid":                   fields["pid"],
		"rc":                    fields["rc"],
	}
}

func nativeDeviceMapFields(state nativeState) map[string]string {
	fields := map[string]string{}
	for key, value := range state.Profile.AdditionalMapFields {
		if isOpaqueWamsysMapKey(key) {
			continue
		}
		if isRuntimeNativeDeviceMapKey(key) {
			continue
		}
		fields[key] = value
	}
	for key, value := range nativeDefaultDeviceMapFields() {
		fields[key] = firstNonEmpty(fields[key], value)
	}
	applyNativePreChatdABDeviceFields(fields, state)
	for key, value := range nativeRuntimeDeviceMapFields(state) {
		fields[key] = value
	}
	return fields
}

func nativeRuntimeDeviceMapFields(state nativeState) map[string]string {
	return map[string]string{
		"pid":               nativeRuntimeProcessID(state),
		"feo2_query_status": nativeDefaultFeo2QueryStatus,
	}
}

func isRuntimeNativeDeviceMapKey(key string) bool {
	switch key {
	case "pid", "feo2_query_status":
		return true
	default:
		return false
	}
}

func nativeRuntimeProcessID(state nativeState) string {
	_ = state
	return strconv.Itoa(os.Getpid())
}

const (
	nativeDefaultFeo2QueryStatus   = "did_not_query"
	nativeDefaultDebugBridgeStatus = "1"
)

func nativeDefaultDeviceMapFields() map[string]string {
	return map[string]string{
		"network_radio_type":    "1",
		"mistyped":              "7",
		"hasav":                 "2",
		"simnum":                "0",
		"hasinrc":               "1",
		"rc":                    "0",
		"rc2":                   "0",
		"airplane_mode_on":      "0",
		"device_ram":            nativeDefaultDeviceRAMGiB,
		"db":                    nativeDefaultDebugBridgeStatus,
		"recaptcha":             `{"stage":"ABPROP_DISABLED"}`,
		"feo2_query_status":     nativeDefaultFeo2QueryStatus,
		"network_operator_name": "",
		"sim_operator_name":     "",
		"mcc":                   "000",
		"mnc":                   "000",
		"sim_mcc":               "000",
		"sim_mnc":               "000",
	}
}

func nativeCodeRequestAttempts(state nativeState) int {
	if state.GenerateCodeAttempts > 0 {
		return state.GenerateCodeAttempts
	}
	return nativeCodeClientMetricAttempts(nativeCodeRequestAttemptsFromLastParams(state.LastCodeParams))
}

func (s *nativeState) nextGenerateCodeAttempt() int {
	previous := s.GenerateCodeAttempts
	if previous < 1 {
		previous = nativeCodeRequestAttemptsFromLastParams(s.LastCodeParams)
	}
	if previous < 0 {
		previous = 0
	}
	s.GenerateCodeAttempts = previous + 1
	return s.GenerateCodeAttempts
}

func nativeCodeRequestAttemptsFromLastParams(params map[string]string) int {
	metrics := strings.TrimSpace(params["client_metrics"])
	if metrics == "" {
		return 0
	}
	var payload struct {
		Attempts int `json:"attempts"`
	}
	if err := json.Unmarshal([]byte(metrics), &payload); err != nil {
		return 0
	}
	return payload.Attempts
}

func nativeCodeRequestReason(state nativeState) string {
	if len(state.LastCodeResult) == 0 {
		return ""
	}
	switch responseStatus(state.LastCodeResult) {
	case "", "sent", "ok":
		return ""
	default:
		return "server-send-request-error-unspecified"
	}
}

func nativeCodeClientMetricAttempts(attempts int) int {
	if attempts < 1 {
		return 1
	}
	return attempts
}

func nativeCodeClientMetrics(attempts int) string {
	body, err := json.Marshal(struct {
		Attempts                  int    `json:"attempts"`
		AppCampaignDownloadSource string `json:"app_campaign_download_source"`
	}{
		Attempts:                  nativeCodeClientMetricAttempts(attempts),
		AppCampaignDownloadSource: nativeDefaultAppCampaignDownloadSource,
	})
	if err != nil {
		return `{"attempts":1,"app_campaign_download_source":"unknown|unknown"}`
	}
	return string(body)
}

func nativeRegisterClientMetrics(method string) string {
	body, err := json.Marshal(struct {
		Attempts             int    `json:"attempts"`
		VerifyMethod         string `json:"verify_method"`
		WasActivatedFromStub bool   `json:"was_activated_from_stub"`
	}{Attempts: 1, VerifyMethod: firstNonEmpty(method, "sms"), WasActivatedFromStub: false})
	if err != nil {
		return `{"attempts":1,"verify_method":"sms","was_activated_from_stub":false}`
	}
	return string(body)
}

func nativeCodeEntryMethod(method string) string {
	switch method {
	case "voice", "email_otp":
		return "1"
	default:
		return "2"
	}
}

const nativeDefaultAppCampaignDownloadSource = "unknown|unknown"

const defaultRegistrationTokenHMACKeyHex = "44539b934347b6f12609296e69145b58309df94ed0a8a5a2d94078a8eaff87013e3d95a69644aa1b924646532c279f8bcd2855ab55f2c8bc1693adb7800c88ff"

// defaultRegistrationTokenSigningCertHex 是 com.whatsapp 官方签名证书(serial 4c2536a4,
// CN=Brian Acton)的整段 X.509 DER —— 注册 token HMAC message 的第 1 段。签名身份长期稳定。
const defaultRegistrationTokenSigningCertHex = "" +
	"30820332308202f0a00302010202044c2536a4300b06072a8648ce3804030500307c310b300906035504061302555331" +
	"1330110603550408130a43616c69666f726e6961311430120603550407130b53616e746120436c617261311630140603" +
	"55040a130d576861747341707020496e632e31143012060355040b130b456e67696e656572696e673114301206035504" +
	"03130b427269616e204163746f6e301e170d3130303632353233303731365a170d3434303231353233303731365a307c" +
	"310b3009060355040613025553311330110603550408130a43616c69666f726e6961311430120603550407130b53616e" +
	"746120436c61726131163014060355040a130d576861747341707020496e632e31143012060355040b130b456e67696e" +
	"656572696e67311430120603550403130b427269616e204163746f6e308201b83082012c06072a8648ce380401308201" +
	"1f02818100fd7f53811d75122952df4a9c2eece4e7f611b7523cef4400c31e3f80b6512669455d402251fb593d8d58fa" +
	"bfc5f5ba30f6cb9b556cd7813b801d346ff26660b76b9950a5a49f9fe8047b1022c24fbba9d7feb7c61bf83b57e7c6a8" +
	"a6150f04fb83f6d3c51ec3023554135a169132f675f3ae2b61d72aeff22203199dd14801c70215009760508f15230bcc" +
	"b292b982a2eb840bf0581cf502818100f7e1a085d69b3ddecbbcab5c36b857b97994afbbfa3aea82f9574c0b3d078267" +
	"5159578ebad4594fe67107108180b449167123e84c281613b7cf09328cc8a6e13c167a8b547c8d28e0a3ae1e2bb3a675" +
	"916ea37f0bfa213562f1fb627a01243bcca4f1bea8519089a883dfe15ae59f06928b665e807b552564014c3bfecf492a" +
	"0381850002818100d1198b4b81687bcf246d41a8a725f0a989a51bce326e84c828e1f556648bd71da487054d6de70fff" +
	"4b49432b6862aa48fc2a93161b2c15a2ff5e671672dfb576e9d12aaff7369b9a99d04fb29d2bbbb2a503ee41b1ff3788" +
	"7064f41fe2805609063500a8e547349282d15981cdb58a08bede51dd7e9867295b3dfb45ffc6b259300b06072a8648ce" +
	"3804030500032f00302c021400a602a7477acf841077237be090df436582ca2f0214350ce0268d07e71e55774ab4eacd" +
	"4d071cd1efad"

// defaultRegistrationTokenClassesDexMD5Hex 是本 APK(2.26.24.77 / versionCode 262407730)
// classes.dex 的 MD5 —— 注册 token HMAC message 的第 2 段。**随 APK 版本变化**:每次升级 APK
// 都要用新 classes.dex 重算,否则服务端按 versionCode 校验 token 失败,/v2/code 返回 bad_token。
const defaultRegistrationTokenClassesDexMD5Hex = "f9d51293993c4312324f87d3cf8bb931"

// deriveDefaultRegistrationToken 复刻 APK 2.26.24.77 注册 token 生成(LX/HxB.A01):
//
//	base64_std( HMAC-SHA1(key, certDER || MD5(classes.dex) || national) )
//
// key 是 PBKDF2WithHmacSHA1And8BIT(salt=HTA.A00, pw=utf8(pkg)||about_logo_hdpi.png,
// iter=128, 512bit) 的派生结果,已离线预算并固化为 defaultRegistrationTokenHMACKeyHex。
func deriveDefaultRegistrationToken(phone string) string {
	key, err := hex.DecodeString(defaultRegistrationTokenHMACKeyHex)
	if err != nil {
		return ""
	}
	cert, err := hex.DecodeString(defaultRegistrationTokenSigningCertHex)
	if err != nil {
		return ""
	}
	classesDexMD5, err := hex.DecodeString(defaultRegistrationTokenClassesDexMD5Hex)
	if err != nil {
		return ""
	}
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(cert)
	_, _ = mac.Write(classesDexMD5)
	_, _ = mac.Write([]byte(phone))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// existDeviceMap 装配 /v2/exist 设备 Map,字段集对齐 APK 的 IAo.A0h(exist 路径)。
// 关键:exist 不发 mcc/mnc/sim_mcc/sim_mnc —— 产出这 4 个的 IAo.A0H 仅被 code(A0E)
// 与 verify(A0F)调用,A0h 全链不调(已 smali 核验);exist 用 sim_state /
// network_operator_name / sim_operator_name / device_name 表征网络态。误发运营商码
// (且默认 000)会让 exist 请求偏离官方端 shape,服务端判 incorrect、检测失准。
func existDeviceMap(state nativeState) map[string]string {
	fields := nativeDeviceMapFields(state)
	return map[string]string{
		"mistyped":                        "7",
		"offline_ab":                      `{"exposure":[],"exp_hash":[],"metrics":{}}`,
		"client_metrics":                  `{"attempts":1,"app_campaign_download_source":"unknown|unknown","was_activated_from_stub":false}`,
		"read_phone_permission_granted":   "0",
		"sim_state":                       "1",
		"network_operator_name":           fields["network_operator_name"],
		"sim_operator_name":               fields["sim_operator_name"],
		"device_name":                     nativeDeviceDisplayName(state),
		"feo2_query_status":               fields["feo2_query_status"],
		"is_foa_fdid_app_installed":       "false",
		"device_ram":                      fields["device_ram"],
		"language_selector_time_spent":    "0",
		"language_selector_clicked_count": "0",
		"db":                              fields["db"],
		"recaptcha":                       fields["recaptcha"],
		"network_radio_type":              fields["network_radio_type"],
		"simnum":                          fields["simnum"],
		"hasinrc":                         fields["hasinrc"],
		"pid":                             fields["pid"],
		"rc":                              fields["rc"],
	}
}

func parseExistProbeResult(data map[string]any) EngineProbeResult {
	status := responseStatus(data)
	reason := responseReason(data)
	methodStatuses := verificationMethodStatuses(data, nil)
	smsWait := verificationSMSCooldownSeconds(data)
	smsWaitExhausted := verificationSMSWaitExhausted(data)
	baseProtocolRejected := existProtocolRejected(status, reason)
	blocked := status == "blocked" || reason == "blocked" || existConsentBlockedReason(reason)
	invalidNumber := existInvalidNumberReason(reason)
	rateLimited := existRateLimitedReason(reason)
	consentRequired := !baseProtocolRejected && !blocked && existConsentReason(reason)
	challengeRequired := !baseProtocolRejected && !blocked && existChallengeReason(reason)
	gated := consentRequired || challengeRequired
	registered := !baseProtocolRejected && !blocked && !invalidNumber && !rateLimited && !gated && (waOldFallbackEligible(data) || accountTransferFallbackEligible(data) || existRegisteredSignal(status, reason, data))
	if registered {
		methodStatuses = upsertVerificationMethodStatus(methodStatuses, "acc_tr", verificationWaitStatus{Present: true})
	}
	protocolRejected := baseProtocolRejected
	notRegistered := !baseProtocolRejected && !blocked && !invalidNumber && !rateLimited && !gated && !registered && existNotRegisteredReason(reason)
	registeredKnown := registered || invalidNumber || notRegistered
	canSendSMS := smsProbeAvailableByCooldownOnly(smsWait, smsWaitExhausted, blocked, protocolRejected, invalidNumber, rateLimited)
	methods := methodsFromStatuses(methodStatuses)
	reachable := !protocolRejected && !blocked && !invalidNumber && !rateLimited && (existReachableStatus(status) || registered || notRegistered || gated)
	accountFlow := existAccountFlow(existFlowClass{
		protocolRejected:  protocolRejected,
		registered:        registered,
		notRegistered:     notRegistered,
		blocked:           blocked,
		invalidNumber:     invalidNumber,
		rateLimited:       rateLimited,
		consentRequired:   consentRequired,
		challengeRequired: challengeRequired,
	})
	result := EngineProbeResult{
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
		result.Err = existProtocolError(data)
	case blocked:
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_UNREACHABLE
	case invalidNumber || rateLimited:
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_UNREACHABLE
	case reachable:
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REACHABLE
	}
	return result
}

func responseReason(data map[string]any) string {
	if value, ok := data["reason"].(string); ok {
		return strings.ToLower(value)
	}
	if value, ok := data["failure_reason"].(string); ok {
		return strings.ToLower(value)
	}
	return ""
}

func existReachableStatus(status string) bool {
	switch status {
	case "ok", "sent", "valid", "exists", "registered":
		return true
	default:
		return false
	}
}

func existRegisteredStatus(status string) bool {
	switch status {
	case "exists", "registered":
		return true
	default:
		return false
	}
}

func existProtocolRejected(status string, reason string) bool {
	if status == "" && reason == "" {
		return false
	}
	switch reason {
	case "missing_param", "bad_param", "bad_token", "old_version", "invalid_skey":
		return true
	default:
		return false
	}
}

func existInvalidNumberReason(reason string) bool {
	switch reason {
	case "format_wrong", "length_short", "length_long":
		return true
	default:
		return false
	}
}

func existRateLimitedReason(reason string) bool {
	switch reason {
	case "too_recent", "too_many", "temporarily_unavailable":
		return true
	default:
		return false
	}
}

// existNotRegisteredReason reports reasons that the same-device check
// (KotlinRegistrationBridge.parseSameDeviceCheckResponse) treats as "no
// existing account on this device, proceed to normal registration".
// "incorrect" is the canonical such verdict: it is an expected /v2/exist
// response, not a request error, so the probe resolves to a definitive
// not-registered/registrable result instead of the reachable catch-all.
func existNotRegisteredReason(reason string) bool {
	return reason == "incorrect"
}

func existRegisteredSignal(status string, reason string, data map[string]any) bool {
	if existRegisteredReason(reason) {
		return true
	}
	if existRegisteredStatus(status) {
		return true
	}
	return firstNonEmpty(jsonString(data["new_jid"]), jsonString(data["jid"]), jsonString(data["registration_jid"])) != ""
}

func existRegisteredReason(reason string) bool {
	switch reason {
	case "security_code", "second_code", "device_confirm_or_second_code", "consent_parent_linking_already_registered":
		return true
	default:
		return false
	}
}

// existConsentReason reports reasons where the number is registrable but the
// same-device check (KotlinRegistrationBridge.parseSameDeviceCheckResponse)
// requires the consent (age/parental) flow before a code can be requested.
func existConsentReason(reason string) bool {
	switch reason {
	case "consent", "consent_minor", "app_store_age":
		return true
	default:
		return false
	}
}

// existConsentBlockedReason reports consent verdicts that hard-block
// registration: underage, impossible age, parental block, linking ineligible.
func existConsentBlockedReason(reason string) bool {
	switch reason {
	case "consent_underage_block", "consent_impossible_age", "consent_parent_block", "consent_parent_linking_ineligible":
		return true
	default:
		return false
	}
}

// existChallengeReason reports reasons that require the challenge flow
// (email/captcha checkpoint) before registration can proceed.
func existChallengeReason(reason string) bool {
	switch reason {
	case "challenge", "challenge_email_start":
		return true
	default:
		return false
	}
}

type existFlowClass struct {
	protocolRejected  bool
	registered        bool
	notRegistered     bool
	blocked           bool
	invalidNumber     bool
	rateLimited       bool
	consentRequired   bool
	challengeRequired bool
}

func existAccountFlow(c existFlowClass) string {
	switch {
	case c.protocolRejected:
		return accountProbeFlowProbeFailed
	case c.blocked:
		return accountProbeFlowBlocked
	case c.invalidNumber:
		return accountProbeFlowInvalidNumber
	case c.rateLimited:
		return accountProbeFlowRateLimited
	case c.consentRequired:
		return accountProbeFlowConsentRequired
	case c.challengeRequired:
		return accountProbeFlowChallengeRequired
	case c.registered:
		return accountProbeFlowRegistered
	case c.notRegistered:
		return accountProbeFlowNotRegistered
	default:
		return accountProbeFlowUnknown
	}
}

func existProtocolError(data map[string]any) error {
	return waProtocolError(data, "WA exist probe rejected")
}

func waProtocolError(data map[string]any, fallback string) error {
	reason := responseReason(data)
	param := jsonString(data["param"])
	message := fallback
	if reason != "" {
		message += ": reason=" + reason
	}
	if param != "" {
		message += " param=" + param
	}
	code := waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED
	retryable := false
	switch reason {
	case "too_recent", "too_many", "temporarily_unavailable":
		code = waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED
		retryable = true
	case "no_routes":
		code = waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE
	}
	return NewError(code, message, retryable)
}

func accountTransferRegisterTerminalFailure(data map[string]any) bool {
	reason := responseReason(data)
	status := responseStatus(data)
	switch reason {
	case "mismatch", "bad_code", "bad_token", "fail_mismatch", "blocked", "fail_blocked", "missing", "fail_missing", "guessed_too_fast", "fail_guessed_too_fast", "security_code", "second_code", "device_confirm_or_second_code", "verified_standalone":
		return true
	case "too_recent", "too_many", "temporarily_unavailable":
		return false
	}
	switch status {
	case "rejected", "blocked", "fail", "failed":
		return true
	case "", "pending", "sent", "retry", "waiting", "temporarily_unavailable":
		return false
	default:
		return false
	}
}

func methodsFromStatuses(statuses []VerificationMethodStatus) []waappv1.VerificationDeliveryMethod {
	seen := map[waappv1.VerificationDeliveryMethod]struct{}{}
	out := make([]waappv1.VerificationDeliveryMethod, 0, len(statuses))
	for _, status := range statuses {
		if status.Method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
			continue
		}
		if _, ok := seen[status.Method]; ok {
			continue
		}
		seen[status.Method] = struct{}{}
		out = append(out, status.Method)
	}
	return out
}

func verificationMethod(name string) waappv1.VerificationDeliveryMethod {
	switch verificationMethodCode(name) {
	case "sms":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS
	case "voice":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE
	case "flash":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_FLASH
	case "wa_old":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
	case "email_otp":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP
	case "send_sms":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SEND_SMS
	case "passkey":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_PASSKEY
	case "silent_auth":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH
	case "silent_auth_ts_43":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH_TS43
	case "autoconf":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_AUTOCONF
	case "deeplink_otp":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DEEPLINK_OTP
	case "recaptcha":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_RECAPTCHA
	case "oauth_email":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_OAUTH_EMAIL
	case "discoverable_credential":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DISCOVERABLE_CREDENTIAL
	case "acc_tr":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER
	case "standalone":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_STANDALONE_APP
	default:
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED
	}
}

type verificationWaitStatus struct {
	Seconds   int64
	Exhausted bool
	Present   bool
}

var apkDefaultRegistrationMethodOrder = []string{"flash", "sms", "voice", "wa_old", "acc_tr", "send_sms", "email_otp"}

func verificationMethodStatuses(data map[string]any, _ []waappv1.VerificationDeliveryMethod) []VerificationMethodStatus {
	out := []VerificationMethodStatus{}
	for _, code := range apkVisibleFallbackMethodCodes(data) {
		out = upsertVerificationMethodStatus(out, code, verificationMethodWaitStatus(data, code, false))
	}
	return out
}

func verificationCodeMethodStatuses(data map[string]any, _ waappv1.VerificationDeliveryMethod) []VerificationMethodStatus {
	return verificationMethodStatuses(data, nil)
}

func upsertVerificationMethodStatus(statuses []VerificationMethodStatus, code string, wait verificationWaitStatus) []VerificationMethodStatus {
	method := verificationMethod(code)
	if method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
		return statuses
	}
	for i := range statuses {
		if statuses[i].Code == code || statuses[i].Method == method {
			statuses[i] = VerificationMethodStatus{Method: method, Code: code, Available: wait.Seconds <= 0 && !wait.Exhausted, CooldownSeconds: wait.Seconds}
			return statuses
		}
	}
	return append(statuses, VerificationMethodStatus{Method: method, Code: code, Available: wait.Seconds <= 0 && !wait.Exhausted, CooldownSeconds: wait.Seconds})
}

func verificationMethodCode(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.TrimPrefix(normalized, "verification_delivery_method_")
	normalized = strings.TrimPrefix(normalized, "registration_login_method_")
	switch normalized {
	case "sms":
		return "sms"
	case "send_sms", "send-sms", "send_sms_to_wa", "send-sms-to-wa":
		return "send_sms"
	case "voice", "call", "phone_call":
		return "voice"
	case "flash":
		return "flash"
	case "wa_old", "wa-old", "old_wa":
		return "wa_old"
	case "email", "email_otp", "email-otp":
		return "email_otp"
	case "passkey":
		return "passkey"
	case "silent_auth", "silent-auth":
		return "silent_auth"
	case "silent_auth_ts_43", "silent-auth-ts-43", "silent_auth_ts43":
		return "silent_auth_ts_43"
	case "autoconf", "auto_conf", "auto-confirm":
		return "autoconf"
	case "deeplink_otp", "deeplink-otp", "deep_link_otp":
		return "deeplink_otp"
	case "recaptcha":
		return "recaptcha"
	case "oauth_email", "oauth-email":
		return "oauth_email"
	case "discoverable_credential", "discoverable-credential":
		return "discoverable_credential"
	case "acc_tr", "account_transfer", "account-transfer":
		return "acc_tr"
	case "standalone", "acverify", "app":
		return "standalone"
	default:
		return ""
	}
}

func fallbackVerificationMethodCodes(data map[string]any) []string {
	return verificationMethodCodesFromValue(data["fallback_methods"])
}

func prefRegistrationMethodOrderCodes(data map[string]any) []string {
	if codes := verificationMethodCodesFromValue(data["pref_reg_methods_order"]); len(codes) > 0 {
		return codes
	}
	return append([]string(nil), apkDefaultRegistrationMethodOrder...)
}

func verificationMethodCodesFromValue(value any) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, raw := range stringList(value) {
		code := verificationMethodCode(raw)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

func apkVisibleFallbackMethodCodes(data map[string]any) []string {
	fallback := fallbackVerificationMethodCodeSet(data)
	if len(fallback) == 0 {
		return nil
	}
	out := []string{}
	for _, code := range prefRegistrationMethodOrderCodes(data) {
		if !fallback[code] {
			continue
		}
		wait := verificationMethodWaitStatus(data, code, false)
		if !wait.Present || wait.Exhausted {
			continue
		}
		if !verificationMethodEligibleForAPKUI(data, code) {
			continue
		}
		out = append(out, code)
	}
	return out
}

func fallbackVerificationMethodCodeSet(data map[string]any) map[string]bool {
	codes := fallbackVerificationMethodCodes(data)
	if len(codes) == 0 {
		return nil
	}
	out := make(map[string]bool, len(codes))
	for _, code := range codes {
		out[code] = true
	}
	return out
}

func waOldFallbackEligible(data map[string]any) bool {
	for _, code := range fallbackVerificationMethodCodes(data) {
		if code == "wa_old" {
			return verificationMethodEligibleForAPKUI(data, code)
		}
	}
	return false
}

func accountTransferFallbackEligible(data map[string]any) bool {
	for _, code := range fallbackVerificationMethodCodes(data) {
		if code == "acc_tr" {
			return verificationMethodEligibleForAPKUI(data, code)
		}
	}
	return false
}

func verificationMethodEligibleForAPKUI(data map[string]any, code string) bool {
	switch code {
	case "sms", "voice", "flash":
		return true
	case "wa_old":
		eligibility, ok := firstPresentJSONInt64(data["pref_wa_old_eligibility"], data["wa_old_eligible"])
		if !ok {
			return false
		}
		return eligibility != 0 && eligibility != 4
	case "acc_tr":
		if verificationExplicitlyEligible(data, "pref_acc_tr_eligibility", "acc_tr_eligible", "account_transfer_eligible") {
			return true
		}
		return waOldFallbackEligible(data)
	case "send_sms":
		return verificationExplicitlyEligible(data, "pref_send_sms_eligibility", "send_sms_eligible", "can_send_sms_to_wa") && !verificationExplicitlyExhausted(data, "send_sms_attempts_exhausted", "pref_send_sms_attempts_exhausted")
	case "email_otp":
		eligibility, ok := firstPresentJSONInt64(data["pref_email_otp_eligibility"], data["email_otp_eligible"])
		return ok && eligibility == 1
	case "silent_auth", "silent_auth_ts_43":
		return verificationExplicitlyEligible(data, "pref_silent_auth_eligibility", "silent_auth_eligible", "silent_auth_available")
	default:
		return false
	}
}

func verificationExplicitlyEligible(data map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := data[key].(bool); ok {
			return value
		}
		if value, ok := firstPresentJSONInt64(data[key]); ok {
			return value == 1
		}
	}
	return false
}

func verificationExplicitlyExhausted(data map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := data[key].(bool); ok && value {
			return true
		}
		if value, ok := firstPresentJSONInt64(data[key]); ok && value != 0 {
			return true
		}
	}
	return false
}

func verificationMethodWaitStatus(data map[string]any, code string, includeRetryAfter bool) verificationWaitStatus {
	wait := firstJSONWaitStatus(verificationMethodWaitValues(data, code)...)
	if wait.Present || !includeRetryAfter {
		return wait
	}
	return firstJSONWaitStatus(data["retry_after"])
}

func verificationMethodWaitValues(data map[string]any, code string) []any {
	switch code {
	case "sms":
		return []any{data["sms_wait"], data["sms_wait_time"], data["sms_retry_time"], data["pref_sms_wait_time"], data["EXTRA_SMS_RETRY_TIME"]}
	case "send_sms":
		return []any{data["send_sms_wait"], data["send_sms_retry_time"], data["pref_send_sms_wait_time"], data["EXTRA_SEND_SMS_RETRY_TIME"]}
	case "voice":
		return []any{data["voice_wait"], data["voice_wait_time"], data["voice_retry_time"], data["pref_voice_wait_time"], data["EXTRA_VOICE_RETRY_TIME"]}
	case "flash":
		return []any{data["flash_wait"], data["flash_wait_time"], data["flash_retry_time"], data["pref_flash_wait_time"], data["EXTRA_FLASH_RETRY_TIME"]}
	case "wa_old":
		return []any{data["wa_old_wait"], data["wa_old_retry_time"], data["pref_wa_old_wait_time"], data["EXTRA_WA_OLD_RETRY_TIME"]}
	case "acc_tr":
		return []any{data["acc_tr_wait"], data["account_transfer_wait"], data["pref_acc_tr_wait_time"], data["EXTRA_ACC_TR_RETRY_TIME"]}
	case "email_otp":
		return []any{data["email_otp_wait"], data["email_otp_retry_time"], data["pref_email_otp_wait_time"], data["EXTRA_EMAIL_OTP_RETRY_TIME"]}
	case "silent_auth":
		return []any{data["silent_auth_wait"], data["pref_silent_auth_wait_time"]}
	default:
		return nil
	}
}

func verificationSMSCooldownSeconds(data map[string]any) int64 {
	return verificationMethodWaitStatus(data, "sms", true).Seconds
}

func verificationSMSWaitExhausted(data map[string]any) bool {
	return verificationMethodWaitStatus(data, "sms", true).Exhausted
}

func smsProbeAvailableByCooldownOnly(smsWait int64, smsWaitExhausted bool, blocked bool, protocolRejected bool, invalidNumber bool, rateLimited bool) bool {
	return smsWait <= 0 && !smsWaitExhausted && !blocked && !protocolRejected && !invalidNumber && !rateLimited
}

func stringList(value any) []string {
	switch v := value.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return v
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			out = append(out, strings.TrimSpace(part))
		}
		return out
	default:
		return nil
	}
}

func jsonInt64(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func firstPresentJSONInt64(values ...any) (int64, bool) {
	for _, value := range values {
		if jsonValuePresent(value) {
			return jsonInt64(value), true
		}
	}
	return 0, false
}

func firstJSONWaitStatus(values ...any) verificationWaitStatus {
	for _, value := range values {
		if !jsonValuePresent(value) {
			continue
		}
		raw := jsonInt64(value)
		if raw < 0 {
			return verificationWaitStatus{Exhausted: true, Present: true}
		}
		return verificationWaitStatus{Seconds: normalizeWaitSeconds(raw), Present: true}
	}
	return verificationWaitStatus{}
}

func normalizeWaitSeconds(value int64) int64 {
	if value <= 0 {
		return 0
	}
	now := time.Now()
	nowMS := now.UnixMilli()
	if value >= 1_000_000_000_000 {
		if value <= nowMS {
			return 0
		}
		return (value - nowMS + 999) / 1000
	}
	nowSeconds := now.Unix()
	if value >= 1_000_000_000 {
		if value <= nowSeconds {
			return 0
		}
		return value - nowSeconds
	}
	return value
}

func jsonValuePresent(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}
