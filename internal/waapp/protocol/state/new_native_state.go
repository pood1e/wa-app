package state

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func NewNativeState(phone *waappv1.PhoneTarget, client *protocolv1.ClientMetadata, deviceRAMGiB string) (NativeState, error) {
	if client == nil {
		return NativeState{}, errors.New("Android client metadata is required")
	}
	chatStatic, err := pwire.NewNativeCurveKeyPair()
	if err != nil {
		return NativeState{}, err
	}
	identity, err := pwire.NewNativeCurveKeyPair()
	if err != nil {
		return NativeState{}, err
	}
	signedPreKey, err := pwire.NewNativeCurveKeyPair()
	if err != nil {
		return NativeState{}, err
	}
	regID := RandomInt32()
	spkID := RandomInt24()
	identityPublic, err := identity.PublicBytes()
	if err != nil {
		return NativeState{}, err
	}
	identityPrivate, err := identity.PrivateBytes()
	if err != nil {
		return NativeState{}, err
	}
	signedPublic, err := signedPreKey.PublicBytes()
	if err != nil {
		return NativeState{}, err
	}
	signedPublicWithPrefix, err := pwire.WithSignalCurvePrefix(signedPublic)
	if err != nil {
		return NativeState{}, err
	}
	signature, err := pwire.XeddsaSignCurve25519(identityPrivate, signedPublicWithPrefix)
	if err != nil {
		return NativeState{}, err
	}
	verified, err := pwire.XeddsaVerifyCurve25519(identityPublic, signedPublicWithPrefix, signature)
	if err != nil {
		return NativeState{}, err
	}
	if !verified {
		return NativeState{}, fmt.Errorf("generated signed prekey signature did not verify")
	}
	profile := buildNativePhoneProfile(phone, client, deviceRAMGiB)
	client = proto.Clone(client).(*protocolv1.ClientMetadata)
	client.PhoneId = profile.FDID
	client.DeviceExperimentId = profile.ExpIDUUID
	state := NativeState{
		Client:        client,
		Schema:        NativeStateSchema,
		CreatedAtUnix: time.Now().UTC().Unix(),
		CC:            shared.PhoneCC(phone),
		Phone:         shared.PhoneNational(phone),
		AuthKey:       chatStatic.Public,
		Profile:       profile,
		ChatStatic:    chatStatic,
		Signal: NativeSignalState{
			RegistrationID:   regID,
			Identity:         identity,
			SignedPreKey:     NativeSignalPreKey{ID: spkID, KeyPair: signedPreKey},
			OneTimePreKeys:   BuildNativeOneTimePreKeys(20),
			RemoteIdentities: map[string]string{},
			Sessions:         map[string]NativeSignalSession{},
		},
		KeyBundle: NativeKeyBundle{
			RegistrationID: regID,
			SignedPreKeyID: spkID,
			IdentityPublic: pwire.B64u(identityPublic),
			KeyType:        pwire.B64u([]byte{0x05}),
			RegID:          pwire.B64u(binary.BigEndian.AppendUint32(nil, uint32(regID))),
			SignedKeyID:    pwire.B64u([]byte{byte(spkID >> 16), byte(spkID >> 8), byte(spkID)}),
			SignedKeyValue: pwire.B64u(signedPublic),
			SignedKeySig:   pwire.B64u(signature),
		},
		MessagePayloads: map[string]NativeMessagePayload{},
		MessagePlainRef: map[string]string{},
	}
	return state, nil
}

func buildNativePhoneProfile(phone *waappv1.PhoneTarget, client *protocolv1.ClientMetadata, deviceRAMGiB string) NativePhoneProfile {
	expIDUUID, expID := UuidPair()
	accessUUID, accessID := UuidPair()
	id := pwire.RandomBytes(20)
	backup := pwire.RandomBytes(20)
	phoneHash := sha256.Sum256([]byte(FullPhoneKey(shared.PhoneCC(phone), shared.PhoneNational(phone))))
	additionalFields := map[string]string{
		"network_radio_type":    "1",
		"simnum":                "0",
		"hasinrc":               "1",
		"rc":                    "0",
		"device_ram":            deviceRAMGiB,
		"recaptcha":             `{"stage":"ABPROP_DISABLED"}`,
		"feo2_query_status":     NativeDefaultFeo2QueryStatus,
		"network_operator_name": "",
		"sim_operator_name":     "",
	}
	return NativePhoneProfile{
		Schema:              "ctf-whatsapp-phone-profile/v1",
		CreatedAtUnix:       time.Now().UTC().Unix(),
		PhoneSHA256:         hex.EncodeToString(phoneHash[:]),
		DeviceVendor:        client.GetManufacturer(),
		DeviceModel:         client.GetModelType(),
		AndroidVersion:      client.GetOsVersion(),
		BuildDisplayID:      client.GetOsBuildNumber(),
		FDID:                pwire.NewUUIDString(),
		ExpID:               expID,
		ExpIDUUID:           expIDUUID,
		AccessSessionID:     accessID,
		AccessSessionIDUUID: accessUUID,
		ID:                  PctBytes(id),
		IDHex:               hex.EncodeToString(id),
		BackupToken:         PctBytes(backup),
		BackupTokenHex:      hex.EncodeToString(backup),
		AdvertisingID:       pwire.NewUUIDString(),
		AdditionalMapFields: additionalFields,
	}
}

func NativeRegistrationEphemeralID() string {
	return PctBytes(pwire.RandomBytes(20))
}

func NativeRegistrationRequestID(state NativeState) string {
	if NativeShouldRandomizeRegistrationRequestID(state) {
		return NativeRegistrationEphemeralID()
	}
	if id := strings.TrimSpace(state.Profile.ID); id != "" {
		return id
	}
	return NativeRegistrationEphemeralID()
}

func NativeAdvertisingID(state NativeState) string {
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

func UuidPair() (string, string) {
	raw := pwire.RandomBytes(16)
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	text := fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:])
	return text, pwire.B64u(raw)
}

func RandomInt32() int32 {
	max := big.NewInt(0x7ffffffe)
	value, err := rand.Int(rand.Reader, max)
	if err != nil {
		return int32(time.Now().UnixNano() & 0x7fffffff)
	}
	return int32(value.Int64() + 1)
}

func RandomInt24() int32 {
	max := big.NewInt(0xfffffe)
	value, err := rand.Int(rand.Reader, max)
	if err != nil {
		return int32(time.Now().UnixNano() & 0xffffff)
	}
	return int32(value.Int64() + 1)
}

func B64Std(raw []byte) string {
	return base64.StdEncoding.EncodeToString(raw)
}

var FormSafe = map[byte]bool{}

func init() {
	for _, ch := range []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~") {
		FormSafe[ch] = true
	}
}

func PctBytes(raw []byte) string {
	var b strings.Builder
	for _, ch := range raw {
		if FormSafe[ch] {
			b.WriteByte(ch)
		} else {
			b.WriteByte('%')
			b.WriteString(strings.ToUpper(hex.EncodeToString([]byte{ch})))
		}
	}
	return b.String()
}

func QuoteForm(value string) string {
	return PctBytes([]byte(value))
}

func NativeUserAgentForState(state NativeState, appVersion string) string {
	return NativeUserAgentForProfile(state.Profile, appVersion)
}
