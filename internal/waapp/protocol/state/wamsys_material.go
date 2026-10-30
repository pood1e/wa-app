package state

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type WamsysMaterialInput struct {
	Capture    *protocolv1.WamsysCapture
	Kind       protocolv1.RegistrationRequestKind
	Phone      *waappv1.PhoneTarget
	State      NativeState
	AppVersion string
	Build      *protocolv1.AndroidBuildMaterial
	Client     *protocolv1.AndroidClientParameters
	Integrity  *waappv1.AndroidIntegrityResult

	Now time.Time
}

type WamsysMaterialProvider interface {
	RegistrationMaterial(context.Context, WamsysMaterialInput) (*protocolv1.WamsysCapture, error)
}

type LocalWamsysMaterialProvider struct{}

const (
	// Native WAMSYS records path ages as time-now minus source/data/external
	// filesystem mtimes. Fresh registration captures show data-dir age as a
	// short running-session value, source-dir slightly older, and external-dir
	// older than both. Do not bind these values to a long-lived profile age.
	NativeWamsysAgeBucketSeconds           = int64(300)
	NativeWamsysFreshProfileMaxAgeSeconds  = int64(600)
	NativeWamsysDataAgeMinSeconds          = int64(30)
	NativeWamsysDataAgeBaseSeconds         = int64(54)
	NativeWamsysDataAgeSpreadSeconds       = uint64(36)
	NativeWamsysSourceAheadBaseSeconds     = int64(8)
	NativeWamsysSourceAheadSpreadSeconds   = uint64(24)
	NativeWamsysExternalAheadBaseSeconds   = int64(8400)
	NativeWamsysExternalAheadSpreadSeconds = uint64(1800)
)

func (p LocalWamsysMaterialProvider) RegistrationMaterial(ctx context.Context, input WamsysMaterialInput) (*protocolv1.WamsysCapture, error) {
	if input.Capture != nil {
		return input.Capture, nil
	}
	switch input.Kind {
	case protocolv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_EXIST,
		protocolv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_CODE,
		protocolv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_REGISTER:
		return p.BuildLocalWamsysCapture(ctx, input)
	default:
		return nil, nil
	}
}

func (p LocalWamsysMaterialProvider) BuildLocalWamsysCapture(ctx context.Context, input WamsysMaterialInput) (*protocolv1.WamsysCapture, error) {
	var gpia NativeGPIAMaterial
	if input.Integrity != nil &&
		NativeABBoolFromConfigs(NativePreChatdABConfigs(input.State), NativeABCodeGPIA, false) {
		var err error
		gpia, err = p.RegistrationGPIAMaterial(ctx, input)
		if err != nil {
			return nil, err
		}
	}
	ga, err := BuildLocalWamsysGA(input)
	if err != nil {
		return nil, err
	}
	fields := []*protocolv1.WamsysMapParam{
		{Key: "gpia", Value: []byte(gpia.Primary)},
		{Key: "_ga", Value: ga},
		{Key: "_gi", Value: []byte(gpia.DeviceCompact)},
		{Key: "_gp", Value: []byte(B64Std(input.Client.GetRequestedPermissionsSha256()))},
		{Key: "_ge", Value: []byte(`{"sb":false,"sv":false}`)},
		{Key: "aid", Value: []byte(NativeWamsysAID(input.State))},
		{Key: "_gg", Value: []byte(gpia.CodeCompact)},
	}
	capture := new(protocolv1.WamsysCapture)
	for _, field := range fields {
		if len(field.GetValue()) != 0 {
			capture.MapParams = append(capture.MapParams, field)
		}
	}
	return capture, nil
}

func (p LocalWamsysMaterialProvider) RegistrationGPIAMaterial(ctx context.Context, input WamsysMaterialInput) (NativeGPIAMaterial, error) {
	return BuildNativeGPIAMaterial(input)
}

func NativeWamsysAID(state NativeState) string {
	sum := sha256.Sum256([]byte(NativeSyntheticAndroidID(state)))
	return B64Std(sum[:])
}

func NativeSyntheticAndroidID(state NativeState) string {
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

func BuildLocalWamsysGA(input WamsysMaterialInput) ([]byte, error) {
	keySource := NativeGPIAKeySource(input.State)
	bootIDMaterial := NativeWamsysBootIDMaterial(input)
	bi, err := EncryptNativeGPIAData(keySource, []byte(bootIDMaterial))
	if err != nil {
		return nil, err
	}
	pathAges := NativeWamsysPathAges(input)
	fields := []NativeGPIAJSONField{
		{Key: "bi", Value: bi},
		{Key: "ap", Value: pathAges.Source},
		{Key: "ai", Value: pathAges.Data},
		{Key: "mp", Value: false},
		{Key: "ae", Value: pathAges.External},
		{Key: "mu", Value: false},
	}
	LogNativeWamsysGAPlaintextShape(input, keySource, bootIDMaterial, fields)
	return RenderNativeGPIAJSONObject(fields)
}

type NativeWamsysPathAgeSet struct {
	Source   int64
	Data     int64
	External int64
}

func NativeWamsysPathAges(input WamsysMaterialInput) NativeWamsysPathAgeSet {
	dataAge := NativeWamsysDataPathAgeSeconds(input)
	sourceAge := dataAge + NativeWamsysRuntimeOffset(input, "source-data-age-delta", NativeWamsysSourceAheadBaseSeconds, NativeWamsysSourceAheadSpreadSeconds)
	externalAge := dataAge + NativeWamsysRuntimeOffset(input, "external-data-age-delta", NativeWamsysExternalAheadBaseSeconds, NativeWamsysExternalAheadSpreadSeconds)
	return NativeWamsysPathAgeSet{Source: sourceAge, Data: dataAge, External: externalAge}
}

func NativeWamsysDataPathAgeSeconds(input WamsysMaterialInput) int64 {
	createdUnix := NativeWamsysStateCreatedUnix(input.State)
	nowUnix := NativeWamsysNow(input).Unix()
	if createdUnix > 0 {
		age := nowUnix - createdUnix
		if age >= NativeWamsysDataAgeMinSeconds && age <= NativeWamsysFreshProfileMaxAgeSeconds {
			return age
		}
	}
	return NativeWamsysRuntimeOffset(input, "data-dir-age", NativeWamsysDataAgeBaseSeconds, NativeWamsysDataAgeSpreadSeconds)
}

func NativeWamsysStateCreatedUnix(state NativeState) int64 {
	if state.Profile.CreatedAtUnix > 0 {
		return state.Profile.CreatedAtUnix
	}
	return state.CreatedAtUnix
}

func NativeWamsysRuntimeOffset(input WamsysMaterialInput, label string, base int64, spread uint64) int64 {
	if spread == 0 {
		return base
	}
	bucket := NativeWamsysNow(input).Unix() / NativeWamsysAgeBucketSeconds
	seed := strings.Join([]string{
		"byte-v-forge-wa-wamsys-runtime-path-age/v1",
		label,
		fmt.Sprintf("%d", input.Kind),
		fmt.Sprintf("%d", bucket),
		shared.PhoneCC(input.Phone),
		shared.PhoneNational(input.Phone),
		input.State.Profile.PhoneSHA256,
		input.State.Profile.FDID,
		input.State.Profile.AccessSessionIDUUID,
		input.State.AuthKey,
	}, "|")
	sum := sha256.Sum256([]byte(seed))
	return base + int64(binary.BigEndian.Uint64(sum[:8])%spread)
}

func NativeStableRuntimeSeed(state NativeState, label string) string {
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

func NativeWamsysNow(input WamsysMaterialInput) time.Time {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	return now.UTC()
}

func NativeWamsysBootIDMaterial(input WamsysMaterialInput) string {
	sum := sha256.Sum256(NativeWamsysBootIDFileBytes(input.State))
	return B64Std(sum[:])
}

func NativeWamsysBootIDFileBytes(state NativeState) []byte {
	return []byte(NativeStableWamsysBootID(state) + "\n")
}

func NativeStableWamsysBootID(state NativeState) string {
	sum := sha256.Sum256([]byte(NativeStableRuntimeSeed(state, "boot-id")))
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
