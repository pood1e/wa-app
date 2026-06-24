package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

const nativeStateSchema = "byte-v-forge-wa-app-native-state/v1"

var nativeUserAgentDevicePattern = regexp.MustCompile(`(?:^|\s)Android/([^ ]+)\s+Device/([^- \t/]+)-([^/\s]+)`)

type nativeState struct {
	Schema               string                          `json:"schema"`
	CreatedAtUnix        int64                           `json:"created_at_unix"`
	CC                   string                          `json:"cc"`
	Phone                string                          `json:"phone"`
	AuthKey              string                          `json:"authkey"`
	PushName             string                          `json:"push_name,omitempty"`
	Profile              nativePhoneProfile              `json:"profile"`
	KeyBundle            nativeKeyBundle                 `json:"key_bundle"`
	GenerateCodeAttempts int                             `json:"reg_attempts_generate_code,omitempty"`
	LastCodeParams       map[string]string               `json:"last_code_params,omitempty"`
	LastCodeResult       map[string]any                  `json:"last_code_result,omitempty"`
	LastRegister         map[string]any                  `json:"last_register,omitempty"`
	AccountTransfer      nativeAccountTransferState      `json:"account_transfer,omitempty"`
	PreChatdAB           nativePreChatdABState           `json:"pre_chatd_ab,omitempty"`
	RegistrationJID      string                          `json:"registration_jid,omitempty"`
	ChatRoutingInfo      string                          `json:"chat_routing_info,omitempty"`
	ChatConnection       nativeChatConnectionState       `json:"chat_connection,omitempty"`
	ChatStatic           nativeCurveKeyPair              `json:"chat_static"`
	Attestation          nativeSoftwareAttestation       `json:"attestation,omitempty"`
	Signal               nativeSignalState               `json:"signal"`
	AppState             nativeAppState                  `json:"app_state,omitempty"`
	ContactHints         []waContactHint                 `json:"contact_hints,omitempty"`
	MessagePayloads      map[string]nativeMessagePayload `json:"message_payloads,omitempty"`
	MessagePlainRef      map[string]string               `json:"message_plain_ref,omitempty"`
	PrivacyTokens        map[string]nativePrivacyToken   `json:"privacy_tokens,omitempty"`
}

type nativePhoneProfile struct {
	Schema              string            `json:"schema"`
	CreatedAtUnix       int64             `json:"created_at_unix"`
	PhoneSHA256         string            `json:"phone_sha256"`
	DeviceVendor        string            `json:"device_vendor"`
	DeviceModel         string            `json:"device_model"`
	AndroidVersion      string            `json:"android_version"`
	BuildDisplayID      string            `json:"build_display_id,omitempty"`
	FDID                string            `json:"fdid"`
	ExpID               string            `json:"expid"`
	ExpIDUUID           string            `json:"expid_uuid"`
	AccessSessionID     string            `json:"access_session_id"`
	AccessSessionIDUUID string            `json:"access_session_id_uuid"`
	ID                  string            `json:"id"`
	IDHex               string            `json:"id_hex"`
	BackupToken         string            `json:"backup_token"`
	BackupTokenHex      string            `json:"backup_token_hex"`
	AdvertisingID       string            `json:"advertising_id,omitempty"`
	AdditionalMapFields map[string]string `json:"additional_map_fields"`
}

type nativeKeyBundle struct {
	RegistrationID int32  `json:"registration_id"`
	SignedPreKeyID int32  `json:"signed_prekey_id"`
	IdentityPublic string `json:"e_ident"`
	KeyType        string `json:"e_keytype"`
	RegID          string `json:"e_regid"`
	SignedKeyID    string `json:"e_skey_id"`
	SignedKeyValue string `json:"e_skey_val"`
	SignedKeySig   string `json:"e_skey_sig"`
}

type nativeSignalPreKey struct {
	ID      int32              `json:"id"`
	KeyPair nativeCurveKeyPair `json:"key_pair"`
}

type nativeSignalState struct {
	RegistrationID   int32                          `json:"registration_id"`
	Identity         nativeCurveKeyPair             `json:"identity"`
	SignedPreKey     nativeSignalPreKey             `json:"signed_prekey"`
	OneTimePreKeys   []nativeSignalPreKey           `json:"one_time_prekeys,omitempty"`
	RemoteIdentities map[string]string              `json:"remote_identities,omitempty"`
	Sessions         map[string]nativeSignalSession `json:"sessions,omitempty"`
}

type nativeSignalSession struct {
	Sender               string                         `json:"sender"`
	Version              int                            `json:"version"`
	RemoteIdentityPublic string                         `json:"remote_identity_public"`
	RootKey              string                         `json:"root_key,omitempty"`
	SenderRatchetPublic  string                         `json:"sender_ratchet_public,omitempty"`
	SenderRatchetPrivate string                         `json:"sender_ratchet_private,omitempty"`
	SenderChain          *nativeSenderChain             `json:"sender_chain,omitempty"`
	ReceiverChains       map[string]nativeReceiverChain `json:"receiver_chains,omitempty"`
	PreviousCounter      *int                           `json:"previous_counter,omitempty"`
	RemoteRegistrationID *int                           `json:"remote_registration_id,omitempty"`
	AliceBaseKey         string                         `json:"alice_base_key,omitempty"`
}

type nativeAppState struct {
	Keys        map[string]nativeAppStateKey        `json:"keys,omitempty"`
	Collections map[string]nativeAppStateCollection `json:"collections,omitempty"`
}

type nativeAppStateKey struct {
	KeyID       string `json:"key_id"`
	KeyData     string `json:"key_data"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Timestamp   int64  `json:"timestamp,omitempty"`
}

type nativeAppStateCollection struct {
	Version        uint64            `json:"version,omitempty"`
	Hash           string            `json:"hash,omitempty"`
	IndexValueMACs map[string]string `json:"index_value_macs,omitempty"`
}

type nativeReceiverChain struct {
	RatchetKey string `json:"ratchet_key"`
	ChainKey   string `json:"chain_key"`
	Index      int    `json:"index"`
	RootKey    string `json:"root_key,omitempty"`
}

type nativeSenderChain struct {
	RatchetKey string `json:"ratchet_key"`
	ChainKey   string `json:"chain_key"`
	Index      int    `json:"index"`
}

type nativeMessagePayload struct {
	Contact             string          `json:"contact,omitempty"`
	Sender              string          `json:"sender,omitempty"`
	ContactPN           string          `json:"contact_pn,omitempty"`
	SenderPN            string          `json:"sender_pn,omitempty"`
	NotifyName          string          `json:"notify_name,omitempty"`
	ParticipantUsername string          `json:"participant_username,omitempty"`
	ContactHints        []waContactHint `json:"contact_hints,omitempty"`
	EncType             string          `json:"enc_type,omitempty"`
	Path                string          `json:"path,omitempty"`
	Payload             string          `json:"payload"`
}

type nativePrivacyToken struct {
	Token     string `json:"token"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type nativeAccountTransferState struct {
	Codes                  []string `json:"codes,omitempty"`
	CurrentIndex           int      `json:"current_index,omitempty"`
	RequestedAtUnix        int64    `json:"requested_at_unix,omitempty"`
	ExpiresAtUnix          int64    `json:"expires_at_unix,omitempty"`
	RotationIntervalSec    int64    `json:"rotation_interval_sec,omitempty"`
	SessionID              string   `json:"session_id,omitempty"`
	Certificate            string   `json:"certificate,omitempty"`
	AuthToken              string   `json:"auth_token,omitempty"`
	PeerID                 string   `json:"peer_id,omitempty"`
	EncryptionKeyVersion   string   `json:"enc_key_version,omitempty"`
	EncryptionAccountHash  string   `json:"enc_key_account_hash,omitempty"`
	EncryptionKeySalt      string   `json:"enc_key_salt,omitempty"`
	DeeplinkBase           string   `json:"deeplink_base,omitempty"`
	AccountPhoneNumber     string   `json:"account_phone_number,omitempty"`
	LastChallengeIssuedSec int64    `json:"last_challenge_issued_sec,omitempty"`
}

type nativeChatConnectionState struct {
	LastHost           string `json:"last_host,omitempty"`
	LastPort           int    `json:"last_port,omitempty"`
	ServerStaticPublic string `json:"server_static_public,omitempty"`
}

func (s *nativeState) ensureMaps() {
	if s.MessagePayloads == nil {
		s.MessagePayloads = map[string]nativeMessagePayload{}
	}
	if s.MessagePlainRef == nil {
		s.MessagePlainRef = map[string]string{}
	}
	if s.PrivacyTokens == nil {
		s.PrivacyTokens = map[string]nativePrivacyToken{}
	}
	if s.Signal.RemoteIdentities == nil {
		s.Signal.RemoteIdentities = map[string]string{}
	}
	if s.Signal.Sessions == nil {
		s.Signal.Sessions = map[string]nativeSignalSession{}
	}
	if s.AppState.Keys == nil {
		s.AppState.Keys = map[string]nativeAppStateKey{}
	}
	if s.AppState.Collections == nil {
		s.AppState.Collections = map[string]nativeAppStateCollection{}
	}
}

func buildNativeOneTimePreKeys(count int) []nativeSignalPreKey {
	out := make([]nativeSignalPreKey, 0, count)
	for i := 0; i < count; i++ {
		kp, err := newNativeCurveKeyPair()
		if err != nil {
			break
		}
		out = append(out, nativeSignalPreKey{ID: int32(i + 1), KeyPair: kp})
	}
	return out
}

func marshalNativeState(state nativeState) ([]byte, error) {
	state.Profile = normalizeNativePhoneProfile(state.Profile, "")
	return json.MarshalIndent(state, "", "  ")
}

func unmarshalNativeState(data []byte) (nativeState, error) {
	var state nativeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nativeState{}, err
	}
	var disk struct {
		UserAgent string `json:"user_agent"`
		Profile   struct {
			UserAgent string `json:"user_agent"`
		} `json:"profile"`
	}
	_ = json.Unmarshal(data, &disk)
	state.Profile = normalizeNativePhoneProfile(state.Profile, firstNonEmpty(disk.Profile.UserAgent, disk.UserAgent))
	state.ensureMaps()
	return state, nil
}

func newNativeState(phone *waappv1.PhoneTarget) (nativeState, error) {
	chatStatic, err := newNativeCurveKeyPair()
	if err != nil {
		return nativeState{}, err
	}
	identity, err := newNativeCurveKeyPair()
	if err != nil {
		return nativeState{}, err
	}
	signedPreKey, err := newNativeCurveKeyPair()
	if err != nil {
		return nativeState{}, err
	}
	regID := randomInt32()
	spkID := randomInt24()
	identityPublic, err := identity.publicBytes()
	if err != nil {
		return nativeState{}, err
	}
	identityPrivate, err := identity.privateBytes()
	if err != nil {
		return nativeState{}, err
	}
	signedPublic, err := signedPreKey.publicBytes()
	if err != nil {
		return nativeState{}, err
	}
	signedPublicWithPrefix, err := withSignalCurvePrefix(signedPublic)
	if err != nil {
		return nativeState{}, err
	}
	signature, err := xeddsaSignCurve25519(identityPrivate, signedPublicWithPrefix)
	if err != nil {
		return nativeState{}, err
	}
	verified, err := xeddsaVerifyCurve25519(identityPublic, signedPublicWithPrefix, signature)
	if err != nil {
		return nativeState{}, err
	}
	if !verified {
		return nativeState{}, fmt.Errorf("generated signed prekey signature did not verify")
	}
	profile := buildNativePhoneProfile(phone)
	state := nativeState{
		Schema:        nativeStateSchema,
		CreatedAtUnix: time.Now().UTC().Unix(),
		CC:            phoneCC(phone),
		Phone:         phoneNational(phone),
		AuthKey:       chatStatic.Public,
		Profile:       profile,
		ChatStatic:    chatStatic,
		Signal: nativeSignalState{
			RegistrationID:   regID,
			Identity:         identity,
			SignedPreKey:     nativeSignalPreKey{ID: spkID, KeyPair: signedPreKey},
			OneTimePreKeys:   buildNativeOneTimePreKeys(20),
			RemoteIdentities: map[string]string{},
			Sessions:         map[string]nativeSignalSession{},
		},
		KeyBundle: nativeKeyBundle{
			RegistrationID: regID,
			SignedPreKeyID: spkID,
			IdentityPublic: b64u(identityPublic),
			KeyType:        b64u([]byte{0x05}),
			RegID:          b64u(binary.BigEndian.AppendUint32(nil, uint32(regID))),
			SignedKeyID:    b64u([]byte{byte(spkID >> 16), byte(spkID >> 8), byte(spkID)}),
			SignedKeyValue: b64u(signedPublic),
			SignedKeySig:   b64u(signature),
		},
		MessagePayloads: map[string]nativeMessagePayload{},
		MessagePlainRef: map[string]string{},
	}
	return state, nil
}

type nativeDeviceModel struct {
	Vendor         string
	Model          string
	Android        string
	BuildDisplayID string
	MinRAMGiB      float64
	MaxRAMGiB      float64
}

var nativeDeviceModels = []nativeDeviceModel{
	{Vendor: "Xiaomi", Model: "M2007J3SC", Android: "11", BuildDisplayID: "M2007J3SC_11.0.14(CN01)", MinRAMGiB: 5.5, MaxRAMGiB: 7.8},
	{Vendor: "HUAWEI", Model: "TRT-AL00A", Android: "7.0", BuildDisplayID: "TRT-AL00A_C00B220(CN01)", MinRAMGiB: 2.8, MaxRAMGiB: 3.9},
	{Vendor: "samsung", Model: "SM-G991B", Android: "13", BuildDisplayID: "SM-G991B_TP1A.014(EUX1)", MinRAMGiB: 6.8, MaxRAMGiB: 7.6},
	{Vendor: "OPPO", Model: "CPH2305", Android: "12", BuildDisplayID: "CPH2305_12.1.0.210(EX1)", MinRAMGiB: 3.6, MaxRAMGiB: 7.4},
	{Vendor: "vivo", Model: "V2145A", Android: "12", BuildDisplayID: "V2145A_12.0.8.7(CN01XX)", MinRAMGiB: 5.5, MaxRAMGiB: 7.7},
	{Vendor: "OnePlus", Model: "LE2100", Android: "14", BuildDisplayID: "LE2100_14.0.0.605(CN01)", MinRAMGiB: 11.24, MaxRAMGiB: 11.24},
}

const nativeDefaultDeviceRAMGiB = "6.58"

func buildNativePhoneProfile(phone *waappv1.PhoneTarget) nativePhoneProfile {
	model := newNativeRegistrationDeviceModel()
	expIDUUID, expID := uuidPair()
	accessUUID, accessID := uuidPair()
	id := randomBytes(20)
	backup := randomBytes(20)
	phoneHash := sha256.Sum256([]byte(fullPhoneKey(phoneCC(phone), phoneNational(phone))))
	additionalFields := map[string]string{
		"network_radio_type":    "1",
		"simnum":                "0",
		"hasinrc":               "1",
		"rc":                    "0",
		"device_ram":            nativeDeviceRAMGiB(model),
		"db":                    nativeDefaultDebugBridgeStatus,
		"recaptcha":             `{"stage":"ABPROP_DISABLED"}`,
		"feo2_query_status":     nativeDefaultFeo2QueryStatus,
		"network_operator_name": "",
		"sim_operator_name":     "",
	}
	return nativePhoneProfile{
		Schema:              "ctf-whatsapp-phone-profile/v1",
		CreatedAtUnix:       time.Now().UTC().Unix(),
		PhoneSHA256:         hex.EncodeToString(phoneHash[:]),
		DeviceVendor:        model.Vendor,
		DeviceModel:         model.Model,
		AndroidVersion:      model.Android,
		BuildDisplayID:      model.BuildDisplayID,
		FDID:                newUUIDString(),
		ExpID:               expID,
		ExpIDUUID:           expIDUUID,
		AccessSessionID:     accessID,
		AccessSessionIDUUID: accessUUID,
		ID:                  pctBytes(id),
		IDHex:               hex.EncodeToString(id),
		BackupToken:         pctBytes(backup),
		BackupTokenHex:      hex.EncodeToString(backup),
		AdvertisingID:       newUUIDString(),
		AdditionalMapFields: additionalFields,
	}
}

func nativeRegistrationEphemeralID() string {
	return pctBytes(randomBytes(20))
}

func nativeRegistrationRequestID(state nativeState) string {
	if nativeShouldRandomizeRegistrationRequestID(state) {
		return nativeRegistrationEphemeralID()
	}
	if id := strings.TrimSpace(state.Profile.ID); id != "" {
		return id
	}
	return nativeRegistrationEphemeralID()
}

func nativeAdvertisingID(state nativeState) string {
	if value := strings.TrimSpace(state.Profile.AdvertisingID); value != "" {
		return value
	}
	seed := sha256.Sum256([]byte(strings.Join([]string{
		"byte-v-forge-wa-advertising-id/v1",
		state.Profile.PhoneSHA256,
		state.Profile.FDID,
		state.Profile.ExpIDUUID,
	}, "|")))
	raw := append([]byte{}, seed[:16]...)
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:])
}

func uuidPair() (string, string) {
	raw := randomBytes(16)
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	text := fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:])
	return text, b64u(raw)
}

func randomBytes(length int) []byte {
	out := make([]byte, length)
	_, _ = rand.Read(out)
	return out
}

func randomInt32() int32 {
	max := big.NewInt(0x7ffffffe)
	value, err := rand.Int(rand.Reader, max)
	if err != nil {
		return int32(time.Now().UnixNano() & 0x7fffffff)
	}
	return int32(value.Int64() + 1)
}

func randomInt24() int32 {
	max := big.NewInt(0xfffffe)
	value, err := rand.Int(rand.Reader, max)
	if err != nil {
		return int32(time.Now().UnixNano() & 0xffffff)
	}
	return int32(value.Int64() + 1)
}

func randomIndex(length int) int {
	if length <= 1 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(length)))
	if err != nil {
		return int(time.Now().UnixNano() % int64(length))
	}
	return int(value.Int64())
}

func newUUIDString() string {
	raw := randomBytes(16)
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:])
}

func b64u(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func b64Std(raw []byte) string {
	return base64.StdEncoding.EncodeToString(raw)
}

var formSafe = map[byte]bool{}

func init() {
	for _, ch := range []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~") {
		formSafe[ch] = true
	}
}

func pctBytes(raw []byte) string {
	var b strings.Builder
	for _, ch := range raw {
		if formSafe[ch] {
			b.WriteByte(ch)
		} else {
			b.WriteByte('%')
			b.WriteString(strings.ToUpper(hex.EncodeToString([]byte{ch})))
		}
	}
	return b.String()
}

func quoteForm(value string) string {
	return pctBytes([]byte(value))
}

func renderNativePlain(params map[string]string, rawKeys map[string]struct{}) string {
	keys := stableParamOrder(params)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := params[key]
		encodedValue := quoteForm(value)
		if _, ok := rawKeys[key]; ok {
			encodedValue = value
		}
		parts = append(parts, quoteForm(key)+"="+encodedValue)
	}
	return strings.Join(parts, "&")
}

func stableParamOrder(params map[string]string) []string {
	preferred := []string{
		"cc", "in", "lg", "lc", "fdid", "expid", "access_session_id",
		"id", "backup_token", "code", "auth_response", "token", "method", "context",
		"clicked_education_link", "manage_call_permission", "call_log_permission", "client_start_message",
		"advertising_id", "login", "type", "authkey", "e_ident", "e_keytype", "e_regid",
		"e_skey_id", "e_skey_val", "e_skey_sig",
		"mistyped", "reason", "hasav", "offline_ab", "client_metrics", "entered",
		"read_phone_permission_granted", "sim_state", "network_operator_name",
		"sim_operator_name", "device_name", "backup_token_error", "mcc", "mnc",
		"sim_mcc", "sim_mnc", "education_screen_displayed", "prefer_sms_over_flash",
		"network_radio_type", "simnum", "hasinrc", "pid", "rc",
		"sim_type", "airplane_mode_type", "cellular_strength", "roaming_type",
		"push_code", "new_acc_uuid", "old_phone_number", "device_ram", "gpia",
		"db", "recaptcha", "fid", "preloads_app_manager_id", "preloads_attribution",
		"tos_version", "entrypoint", "cred_token", "_ga", "_gi", "_gp", "_ge", "aid", "_gg",
		"feo2_query_status", "is_foa_fdid_app_installed", "language_selector_time_spent",
		"language_selector_clicked_count",
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, key := range preferred {
		if _, ok := params[key]; ok {
			out = append(out, key)
			seen[key] = struct{}{}
		}
	}
	rest := make([]string, 0)
	for key := range params {
		if _, ok := seen[key]; !ok {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	out = append(out, rest...)
	return out
}

func nativeUserAgent(appVersion string) string {
	return nativeUserAgentForProfile(nativePhoneProfile{}, appVersion)
}

func nativeUserAgentForState(state nativeState, appVersion string) string {
	return nativeUserAgentForProfile(state.Profile, appVersion)
}

func nativeUserAgentForProfile(profile nativePhoneProfile, appVersion string) string {
	return "WhatsApp/" + nativeAppVersion(appVersion) + " " + nativeDeviceUserAgent(profile)
}

func nativeDeviceUserAgent(profile nativePhoneProfile) string {
	profile = normalizeNativePhoneProfile(profile, "")
	return "Android/" + profile.AndroidVersion + " Device/" + profile.DeviceVendor + "-" + profile.DeviceModel
}

func nativeAppVersion(appVersion string) string {
	if strings.TrimSpace(appVersion) == "" {
		return defaultWAAppVersion
	}
	return strings.TrimSpace(appVersion)
}

func nativeAppVersionFromUserAgent(userAgent string) string {
	userAgent = strings.TrimSpace(userAgent)
	const prefix = "WhatsApp/"
	if !strings.HasPrefix(userAgent, prefix) {
		return ""
	}
	version := strings.TrimPrefix(userAgent, prefix)
	if idx := strings.IndexByte(version, ' '); idx >= 0 {
		version = version[:idx]
	}
	return strings.TrimSpace(version)
}

func normalizeNativePhoneProfile(profile nativePhoneProfile, userAgent string) nativePhoneProfile {
	if device, ok := nativeDeviceModelFromUserAgent(userAgent); ok {
		profile.DeviceVendor = firstNonEmpty(profile.DeviceVendor, device.Vendor)
		profile.DeviceModel = firstNonEmpty(profile.DeviceModel, device.Model)
		profile.AndroidVersion = firstNonEmpty(profile.AndroidVersion, device.Android)
		profile.BuildDisplayID = firstNonEmpty(profile.BuildDisplayID, nativeBuildDisplayIDForModel(device))
	}
	device := defaultNativeDeviceModel()
	profile.DeviceVendor = firstNonEmpty(profile.DeviceVendor, device.Vendor)
	profile.DeviceModel = firstNonEmpty(profile.DeviceModel, device.Model)
	profile.AndroidVersion = firstNonEmpty(profile.AndroidVersion, device.Android)
	profile.BuildDisplayID = firstNonEmpty(profile.BuildDisplayID, nativeBuildDisplayIDForModel(nativeDeviceModel{
		Vendor:  profile.DeviceVendor,
		Model:   profile.DeviceModel,
		Android: profile.AndroidVersion,
	}), device.BuildDisplayID)
	if shouldNormalizeNativeAccessSessionID(profile) {
		profile = normalizeNativeAccessSessionID(profile)
	}
	if len(profile.AdditionalMapFields) > 0 {
		fields := make(map[string]string, len(profile.AdditionalMapFields))
		for key, value := range profile.AdditionalMapFields {
			if isRuntimeNativeDeviceMapKey(key) {
				continue
			}
			fields[key] = value
		}
		profile.AdditionalMapFields = fields
	}
	return profile
}

func shouldNormalizeNativeAccessSessionID(profile nativePhoneProfile) bool {
	return profile.AccessSessionID != "" ||
		profile.AccessSessionIDUUID != "" ||
		profile.FDID != "" ||
		profile.PhoneSHA256 != ""
}

func normalizeNativeAccessSessionID(profile nativePhoneProfile) nativePhoneProfile {
	if isUUIDText(profile.AccessSessionIDUUID) {
		profile.AccessSessionID = nativeUUIDTextToB64u(profile.AccessSessionIDUUID)
		return profile
	}
	if isUUIDText(profile.AccessSessionID) {
		profile.AccessSessionIDUUID = profile.AccessSessionID
		profile.AccessSessionID = nativeUUIDTextToB64u(profile.AccessSessionID)
		return profile
	}
	if accessSessionIDUUID, ok := nativeUUIDB64uToText(profile.AccessSessionID); ok {
		profile.AccessSessionIDUUID = accessSessionIDUUID
		return profile
	}
	accessSessionIDUUID, accessSessionID := uuidPair()
	profile.AccessSessionID = accessSessionID
	profile.AccessSessionIDUUID = accessSessionIDUUID
	return profile
}

func nativeUUIDTextToB64u(value string) string {
	value = strings.TrimSpace(value)
	if !isUUIDText(value) {
		return ""
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil || len(raw) != 16 {
		return ""
	}
	return b64u(raw)
}

func nativeUUIDB64uToText(value string) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(raw) != 16 {
		return "", false
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), true
}

func isUUIDText(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	for idx, ch := range value {
		switch idx {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
				return false
			}
		}
	}
	return true
}

func nativeDeviceModelFromUserAgent(userAgent string) (nativeDeviceModel, bool) {
	match := nativeUserAgentDevicePattern.FindStringSubmatch(strings.TrimSpace(userAgent))
	if len(match) != 4 {
		return nativeDeviceModel{}, false
	}
	return nativeDeviceModel{Android: match[1], Vendor: match[2], Model: match[3]}, true
}

// 注册设备画像池来自内嵌的 device_profiles.json(可经 WA_APP_DEVICE_PROFILES_FILE
// 指向外部文件覆盖),加载/校验逻辑见 device_profiles.go。每账号注册时随机选一台并
// 持久化到 nativeState.Profile;池由 parseDefaultDeviceProfiles 保证至少一台,故下面
// 的下标/随机取用安全。GPIA 错误态物料里设备相关字段仅 did=BuildDisplayID 字符串,
// APK digest 为常量,故多机型共用同一套 GPIA 常量自洽。
func defaultNativeDeviceModel() nativeDeviceModel {
	return registrationDeviceModels()[0]
}

func newNativeRegistrationDeviceModel() nativeDeviceModel {
	pool := registrationDeviceModels()
	return pool[randomIndex(len(pool))]
}

func nativeDeviceRAMGiB(model nativeDeviceModel) string {
	if model.MinRAMGiB <= 0 || model.MaxRAMGiB < model.MinRAMGiB {
		return nativeDefaultDeviceRAMGiB
	}
	if model.MaxRAMGiB == model.MinRAMGiB {
		return fmt.Sprintf("%.2f", model.MinRAMGiB)
	}
	scaled := int(model.MinRAMGiB*100) + randomIndex(int((model.MaxRAMGiB-model.MinRAMGiB)*100)+1)
	return fmt.Sprintf("%.2f", float64(scaled)/100)
}

func nativeBuildDisplayIDForModel(model nativeDeviceModel) string {
	for _, candidate := range nativeDeviceModels {
		if strings.EqualFold(candidate.Vendor, model.Vendor) && strings.EqualFold(candidate.Model, model.Model) && candidate.Android == model.Android {
			return candidate.BuildDisplayID
		}
	}
	return nativeSyntheticBuildDisplayID(model)
}

func nativeSyntheticBuildDisplayID(model nativeDeviceModel) string {
	modelName := strings.TrimSpace(model.Model)
	android := strings.TrimSpace(model.Android)
	if modelName == "" || android == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"byte-v-forge-wa-native-build-display-id/v1",
		strings.TrimSpace(model.Vendor),
		modelName,
		android,
	}, "|")))
	branches := []string{"GX", "GL", "EEA", "IN", "LA"}
	major := int(sum[0]%9) + 1
	minor := int(binary.BigEndian.Uint16(sum[1:3])%990) + 10
	branch := branches[int(sum[3])%len(branches)]
	return modelName + "_" + android + ".0." + strconv.Itoa(major) + "." + strconv.Itoa(minor) + "(" + branch + "01)"
}

func nativeDeviceDisplayName(state nativeState) string {
	profile := normalizeNativePhoneProfile(state.Profile, "")
	value := strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(profile.DeviceVendor),
		strings.TrimSpace(profile.DeviceModel),
	}, " "))
	return firstNonEmpty(value, defaultNativeDeviceModel().Vendor+" "+defaultNativeDeviceModel().Model)
}

func responseStatus(data map[string]any) string {
	if value, ok := data["status"].(string); ok {
		return strings.ToLower(value)
	}
	return ""
}

func fullPhoneKey(cc string, phone string) string {
	compact := regexp.MustCompile(`\D+`).ReplaceAllString(cc+phone, "")
	if compact == "" {
		return hex.EncodeToString(sha256.New().Sum(nil))
	}
	return compact
}
