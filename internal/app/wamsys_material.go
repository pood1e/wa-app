package app

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type wamsysMaterialInput struct {
	Capture *waappv1.WamsysCapture
	Kind    waappv1.RegistrationRequestKind
	Phone   *waappv1.PhoneTarget
	State   nativeState
	Now     time.Time
}

type wamsysMaterialProvider interface {
	RegistrationMaterial(context.Context, wamsysMaterialInput) (*waappv1.WamsysCapture, error)
}

type localWamsysMaterialProvider struct{}

const (
	// Native WAMSYS records path ages as time-now minus source/data/external
	// filesystem mtimes. Fresh registration captures show data-dir age as a
	// short running-session value, source-dir slightly older, and external-dir
	// older than both. Do not bind these values to a long-lived profile age.
	nativeWamsysAgeBucketSeconds           = int64(300)
	nativeWamsysFreshProfileMaxAgeSeconds  = int64(600)
	nativeWamsysDataAgeMinSeconds          = int64(30)
	nativeWamsysDataAgeBaseSeconds         = int64(54)
	nativeWamsysDataAgeSpreadSeconds       = uint64(36)
	nativeWamsysSourceAheadBaseSeconds     = int64(8)
	nativeWamsysSourceAheadSpreadSeconds   = uint64(24)
	nativeWamsysExternalAheadBaseSeconds   = int64(8400)
	nativeWamsysExternalAheadSpreadSeconds = uint64(1800)
	// SHA-256/Base64 over Android 11 PackageInfo.requestedPermissions after
	// native lexicographic sort and delimiter-free concatenation.
	nativeWamsysRequestedPermissionsDigest = "NNj5BoWX+yvZBYEY46Ze+Ad6Ykk0Z27FjgSysvkzzCU="
)

func (localWamsysMaterialProvider) RegistrationMaterial(ctx context.Context, input wamsysMaterialInput) (*waappv1.WamsysCapture, error) {
	_ = ctx
	if input.Capture != nil {
		return input.Capture, nil
	}
	switch input.Kind {
	case waappv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_EXIST,
		waappv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_CODE:
		return buildLocalWamsysCapture(input)
	default:
		return nil, nil
	}
}

func buildLocalWamsysCapture(input wamsysMaterialInput) (*waappv1.WamsysCapture, error) {
	gpia, err := buildNativeGPIAErrorMaterial(input)
	if err != nil {
		return nil, err
	}
	ga, err := buildLocalWamsysGA(input)
	if err != nil {
		return nil, err
	}
	return &waappv1.WamsysCapture{MapParams: []*waappv1.WamsysMapParam{
		{Key: "gpia", Value: []byte(gpia.Primary)},
		{Key: "_ga", Value: ga},
		{Key: "_gi", Value: []byte(gpia.DeviceCompact)},
		{Key: "_gp", Value: []byte(nativeWamsysRequestedPermissionsDigest)},
		{Key: "_ge", Value: []byte(`{"sb":false,"sv":false}`)},
		{Key: "aid", Value: []byte(nativeWamsysAID(input.State))},
		{Key: "_gg", Value: []byte(gpia.CodeCompact)},
	}}, nil
}

func nativeWamsysAID(state nativeState) string {
	sum := sha256.Sum256([]byte(nativeSyntheticAndroidID(state)))
	return b64Std(sum[:])
}

func nativeSyntheticAndroidID(state nativeState) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"byte-v-forge-wa-wamsys-android-id/v1",
		state.Profile.PhoneSHA256,
		state.Profile.FDID,
		state.Profile.ExpIDUUID,
		state.Profile.AccessSessionIDUUID,
		state.Profile.IDHex,
		state.Profile.BackupTokenHex,
		state.AuthKey,
	}, "|")))
	return hex.EncodeToString(sum[:8])
}

func buildLocalWamsysGA(input wamsysMaterialInput) ([]byte, error) {
	keySource := nativeGPIAKeySource(input.State)
	bootIDMaterial := nativeWamsysBootIDMaterial(input)
	bi, err := encryptNativeGPIAData(keySource, []byte(bootIDMaterial))
	if err != nil {
		return nil, err
	}
	pathAges := nativeWamsysPathAges(input)
	fields := []nativeGPIAJSONField{
		{Key: "bi", Value: bi},
		{Key: "ap", Value: pathAges.Source},
		{Key: "ai", Value: pathAges.Data},
		{Key: "mp", Value: false},
		{Key: "ae", Value: pathAges.External},
		{Key: "mu", Value: false},
	}
	logNativeWamsysGAPlaintextShape(input, keySource, bootIDMaterial, fields)
	return renderNativeGPIAJSONObject(fields)
}

type nativeWamsysPathAgeSet struct {
	Source   int64
	Data     int64
	External int64
}

func nativeWamsysPathAges(input wamsysMaterialInput) nativeWamsysPathAgeSet {
	dataAge := nativeWamsysDataPathAgeSeconds(input)
	sourceAge := dataAge + nativeWamsysRuntimeOffset(input, "source-data-age-delta", nativeWamsysSourceAheadBaseSeconds, nativeWamsysSourceAheadSpreadSeconds)
	externalAge := dataAge + nativeWamsysRuntimeOffset(input, "external-data-age-delta", nativeWamsysExternalAheadBaseSeconds, nativeWamsysExternalAheadSpreadSeconds)
	return nativeWamsysPathAgeSet{Source: sourceAge, Data: dataAge, External: externalAge}
}

func nativeWamsysDataPathAgeSeconds(input wamsysMaterialInput) int64 {
	createdUnix := nativeWamsysStateCreatedUnix(input.State)
	nowUnix := nativeWamsysNow(input).Unix()
	if createdUnix > 0 {
		age := nowUnix - createdUnix
		if age >= nativeWamsysDataAgeMinSeconds && age <= nativeWamsysFreshProfileMaxAgeSeconds {
			return age
		}
	}
	return nativeWamsysRuntimeOffset(input, "data-dir-age", nativeWamsysDataAgeBaseSeconds, nativeWamsysDataAgeSpreadSeconds)
}

func nativeWamsysStateCreatedUnix(state nativeState) int64 {
	if state.Profile.CreatedAtUnix > 0 {
		return state.Profile.CreatedAtUnix
	}
	return state.CreatedAtUnix
}

func nativeWamsysRuntimeOffset(input wamsysMaterialInput, label string, base int64, spread uint64) int64 {
	if spread == 0 {
		return base
	}
	bucket := nativeWamsysNow(input).Unix() / nativeWamsysAgeBucketSeconds
	seed := strings.Join([]string{
		"byte-v-forge-wa-wamsys-runtime-path-age/v1",
		label,
		fmt.Sprintf("%d", input.Kind),
		fmt.Sprintf("%d", bucket),
		phoneCC(input.Phone),
		phoneNational(input.Phone),
		input.State.Profile.PhoneSHA256,
		input.State.Profile.FDID,
		input.State.Profile.AccessSessionIDUUID,
		input.State.AuthKey,
	}, "|")
	sum := sha256.Sum256([]byte(seed))
	return base + int64(binary.BigEndian.Uint64(sum[:8])%spread)
}

func nativeStableRuntimeSeed(state nativeState, label string) string {
	return strings.Join([]string{
		"byte-v-forge-wa-native-runtime/v1",
		strings.TrimSpace(label),
		state.CC,
		state.Phone,
		state.Profile.PhoneSHA256,
		state.Profile.FDID,
		state.Profile.ExpIDUUID,
		state.Profile.AccessSessionIDUUID,
		state.AuthKey,
		state.KeyBundle.IdentityPublic,
		state.ChatStatic.Public,
	}, "|")
}

func nativeWamsysNow(input wamsysMaterialInput) time.Time {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	return now.UTC()
}

func nativeWamsysBootIDMaterial(input wamsysMaterialInput) string {
	sum := sha256.Sum256(nativeWamsysBootIDFileBytes(input.State))
	return b64Std(sum[:])
}

func nativeWamsysBootIDFileBytes(state nativeState) []byte {
	return []byte(nativeStableWamsysBootID(state) + "\n")
}

func nativeStableWamsysBootID(state nativeState) string {
	sum := sha256.Sum256([]byte(nativeStableRuntimeSeed(state, "boot-id")))
	id := append([]byte(nil), sum[:16]...)
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(id)
	return strings.Join([]string{
		encoded[0:8],
		encoded[8:12],
		encoded[12:16],
		encoded[16:20],
		encoded[20:32],
	}, "-")
}

func (e *NativeEngine) applyRuntimeWamsys(
	ctx context.Context,
	kind waappv1.RegistrationRequestKind,
	phone *waappv1.PhoneTarget,
	state nativeState,
	params map[string]string,
	rawKeys map[string]struct{},
) error {
	capture, err := e.wamsysProvider().RegistrationMaterial(ctx, wamsysMaterialInput{Kind: kind, Phone: phone, State: state, Now: e.clock.Now()})
	if err != nil {
		return err
	}
	excluded := map[string]struct{}{}
	if !nativeShouldSendRegistrationGPIA(state) {
		excluded["gpia"] = struct{}{}
	}
	applyOpaqueWamsysMapParams(params, rawKeys, capture, excluded)
	return nil
}

func applyOpaqueWamsysMapParams(params map[string]string, rawKeys map[string]struct{}, capture *waappv1.WamsysCapture, excluded map[string]struct{}) {
	if capture == nil {
		return
	}
	for _, item := range capture.GetMapParams() {
		key := item.GetKey()
		if !isOpaqueWamsysMapKey(key) {
			continue
		}
		if _, skip := excluded[key]; skip {
			continue
		}
		params[key] = pctBytes(item.GetValue())
		rawKeys[key] = struct{}{}
	}
}

func applyOrderedWamsysKey(params *orderedParams, capture *waappv1.WamsysCapture, key string) {
	if params == nil || capture == nil || !isOpaqueWamsysMapKey(key) {
		return
	}
	for _, item := range capture.GetMapParams() {
		if item.GetKey() == key {
			params.set(key, pctBytes(item.GetValue()), true)
			return
		}
	}
}

func applyOrderedWamsysExcept(params *orderedParams, capture *waappv1.WamsysCapture, excluded map[string]struct{}) {
	if params == nil || capture == nil {
		return
	}
	for _, item := range capture.GetMapParams() {
		key := item.GetKey()
		if !isOpaqueWamsysMapKey(key) {
			continue
		}
		if _, skip := excluded[key]; skip {
			continue
		}
		params.set(key, pctBytes(item.GetValue()), true)
	}
}

// Opaque WAMSYS values stay behind a dedicated material provider so registration
// maps do not leak opaque blobs into generic phone profile fields.
var opaqueWamsysMapKeys = map[string]struct{}{
	"gpia": {},
	"_ge":  {},
	"_gi":  {},
	"_gg":  {},
	"_gp":  {},
	"_ga":  {},
	"aid":  {},
	"_gs":  {},
}

func isOpaqueWamsysMapKey(key string) bool {
	_, ok := opaqueWamsysMapKeys[key]
	return ok
}
