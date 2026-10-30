package ios

import (
	"crypto/ecdh"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type parameter struct{ name, value string }
type parameters []parameter

func (p parameters) plaintext() []byte {
	var out strings.Builder
	for index, item := range p {
		if index != 0 {
			out.WriteByte('&')
		}
		out.WriteString(item.name)
		out.WriteByte('=')
		out.WriteString(strings.ReplaceAll(item.value, "+", "%2B"))
	}
	return []byte(out.String())
}

type registrationParameters struct {
	base         parameters
	iosID        string
	id           string
	token        string
	phoneID      string
	experimentID string
}

func newRegistrationParameters(phone *waappv1.PhoneTarget, profile *protocolv1.IOSRegistrationProfile, keys *protocolv1.RegistrationKeyMaterial, now time.Time) (registrationParameters, error) {
	noise, err := ecdh.X25519().NewPrivateKey(keys.GetNoisePrivateKey())
	if err != nil {
		return registrationParameters{}, err
	}
	identity, err := ecdh.X25519().NewPrivateKey(keys.GetIdentityPrivateKey())
	if err != nil {
		return registrationParameters{}, err
	}
	signed, err := ecdh.X25519().NewPrivateKey(keys.GetSignedPreKeyPrivateKey())
	if err != nil {
		return registrationParameters{}, err
	}
	var ids [32]byte
	if _, err := rand.Read(ids[:]); err != nil {
		return registrationParameters{}, err
	}
	fdid, err := uuid.NewRandom()
	if err != nil {
		return registrationParameters{}, err
	}
	experiment, err := uuid.NewRandom()
	if err != nil {
		return registrationParameters{}, err
	}
	access, err := uuid.NewRandom()
	if err != nil {
		return registrationParameters{}, err
	}
	encode := base64.URLEncoding.EncodeToString
	prekeyID := keys.GetSignedPreKeyId()
	result := registrationParameters{
		phoneID: strings.ToUpper(fdid.String()), experimentID: strings.ToUpper(experiment.String()),
		base: parameters{
			{"cc", phone.GetCountryCallingCode()}, {"in", phone.GetNationalNumber()},
			{"type", "0"}, {"rc", "0"}, {"lg", profile.GetLanguage()}, {"lc", profile.GetLocale()},
			{"authkey", encode(noise.PublicKey().Bytes())},
			{"e_regid", encode(binary.BigEndian.AppendUint32(nil, keys.GetRegistrationId()))},
			{"e_keytype", encode([]byte{5})}, {"e_ident", encode(identity.PublicKey().Bytes())},
			{"e_skey_id", encode([]byte{byte(prekeyID >> 16), byte(prekeyID >> 8), byte(prekeyID)})},
			{"e_skey_val", encode(signed.PublicKey().Bytes())}, {"e_skey_sig", encode(keys.GetSignedPreKeySignature())},
			{"fdid", strings.ToUpper(fdid.String())}, {"expid", base64.RawURLEncoding.EncodeToString(experiment[:])},
			{"access_session_id", strings.ToUpper(access.String())},
			{"reg_client_event_ts", strconv.FormatInt(now.UnixMilli(), 10)},
		},
		iosID: base64.RawURLEncoding.EncodeToString(ids[:16]),
		id:    percentBytes(ids[16:]),
		token: registrationToken(profile, phone.GetNationalNumber()),
	}
	return result, nil
}

func (p registrationParameters) request(path, method, code string, now time.Time) parameters {
	query := append(parameters(nil), p.base...)
	switch path {
	case "exist":
		query = append(query, parameter{"token", p.token}, parameter{"ios_id", p.iosID})
	case "code":
		query = append(query, parameter{"method", method}, parameter{"sim_mcc", "000"}, parameter{"sim_mnc", "000"},
			parameter{"ios_id", p.iosID}, parameter{"token", p.token})
	case "register":
		query = append(query, parameter{"code", code}, parameter{"entered", "1"})
		if method != "" {
			query = append(query, parameter{"method", method})
		}
		query = append(query, parameter{"ios_id", p.iosID})
	case "password":
		query = append(query, parameter{"code", url.QueryEscape(code)}, parameter{"context", "twofac_dynamic"}, parameter{"method", "password"}, parameter{"ios_id", p.iosID})
	}
	return append(query, parameter{"id", p.id}, parameter{"t", strconv.FormatInt(now.Unix(), 10)})
}

func registrationToken(profile *protocolv1.IOSRegistrationProfile, nationalNumber string) string {
	digest := md5.Sum([]byte(profile.GetBuildTokenSecret() + profile.GetBuildHash() + nationalNumber))
	return hex.EncodeToString(digest[:])
}

func percentBytes(value []byte) string {
	const digits = "0123456789ABCDEF"
	var result strings.Builder
	result.Grow(len(value) * 3)
	for _, b := range value {
		result.WriteByte('%')
		result.WriteByte(digits[b>>4])
		result.WriteByte(digits[b&15])
	}
	return result.String()
}

func validateRegistration(phone *waappv1.PhoneTarget, profile *protocolv1.IOSRegistrationProfile) error {
	if !decimal(phone.GetCountryCallingCode(), 1, 3) || !decimal(phone.GetNationalNumber(), 4, 14) ||
		len(phone.GetCountryCallingCode())+len(phone.GetNationalNumber()) > 15 {
		return errors.New("registration phone is invalid")
	}
	if phone.GetE164Number() != "" && phone.GetE164Number() != "+"+phone.GetCountryCallingCode()+phone.GetNationalNumber() {
		return errors.New("registration phone fields do not match")
	}
	return validateProfile(profile)
}

func validateProfile(profile *protocolv1.IOSRegistrationProfile) error {
	if profile.GetAppVersion() != "26.40.77" || len(profile.GetBuildTokenSecret()) != 40 || len(profile.GetBuildHash()) != 32 {
		return errors.New("iOS registration build profile is invalid")
	}
	if _, err := hex.DecodeString(profile.GetBuildHash()); err != nil {
		return errors.New("iOS registration build hash is invalid")
	}
	if len(profile.GetUserAgent()) > 256 || !strings.Contains(profile.GetUserAgent(), profile.GetAppVersion()) ||
		strings.ContainsAny(profile.GetUserAgent(), "\r\n") || !languageCode(profile.GetLanguage()) || !languageCode(profile.GetLocale()) {
		return errors.New("iOS registration metadata is invalid")
	}
	client := profile.GetClient()
	if client.GetPlatform() != protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS || client.GetAppVersion() != "2."+profile.GetAppVersion() ||
		len(client.GetNoiseRootPublicKey()) != 32 || client.GetOsVersion() == "" || client.GetDevice() == "" {
		return errors.New("iOS communication metadata is invalid")
	}
	return nil
}

func languageCode(value string) bool {
	if len(value) != 2 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') {
			return false
		}
	}
	return true
}

func decimal(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
