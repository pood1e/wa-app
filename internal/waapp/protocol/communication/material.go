package communication

import (
	"crypto/ecdh"
	"crypto/subtle"
	"fmt"
	"slices"
	"strings"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func ValidateMaterial(material *waappv1.AcceptedAccountMaterial) error {
	if _, err := wamodel.NormalizeAccountProxy(material.GetProxy()); err != nil {
		return err
	}
	invalid := func(reason string) error {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, reason, false)
	}
	phone := wamodel.NormalizePhone(material.GetPhone())
	digits := shared.DigitsOnly(phone.GetE164Number())
	if digits == "" || len(digits) > 15 || phone.GetE164Number() != material.GetPhone().GetE164Number() {
		return invalid("normalized account phone is required")
	}
	if material.GetServiceLoginId() != digits || material.GetServiceAccountId() == "" {
		return invalid("account identity does not match phone")
	}
	credentials := material.GetCredentials()
	if credentials.GetRegistrationId() < 1 {
		return invalid("invalid Signal registration ID")
	}
	if credentials.GetSignedPreKey().GetId() < 1 || credentials.GetSignedPreKey().GetId() > 0xffffff {
		return invalid("invalid signed prekey ID")
	}
	keys := []*protocolv1.CurveKeyPair{credentials.GetChatStatic(), credentials.GetIdentity(), credentials.GetSignedPreKey().GetKeyPair()}
	seen := map[int32]bool{}
	if len(credentials.GetOneTimePreKeys()) > 1000 {
		return invalid("too many Signal prekeys")
	}
	for _, key := range credentials.GetOneTimePreKeys() {
		if key.GetId() < 1 || key.GetId() > 0xffffff || seen[key.GetId()] {
			return invalid("invalid Signal prekey ID")
		}
		seen[key.GetId()] = true
		keys = append(keys, key.GetKeyPair())
	}
	for _, key := range keys {
		if !validCurvePair(key) {
			return invalid("invalid account key pair")
		}
	}
	if err := validatePQCredentials(credentials); err != nil {
		return invalid("invalid account PQ prekeys")
	}
	recovery := material.GetRecovery()
	client := recovery.GetClient()
	if err := requireChatdPlatform(client); err != nil {
		return invalid("unsupported account communication platform")
	}
	if client.GetAppVersion() == "" || strings.ContainsAny(client.GetAppVersion(), "\r\n\x00") {
		return invalid("protocol version is required")
	}
	if len(client.GetNoiseRootPublicKey()) != 32 || wire.ValidateHandshakeMode(client.GetHandshakePqMode()) != nil {
		return invalid("invalid Noise handshake metadata")
	}
	if recovery.GetRegistrationJid() != digits+"@s.whatsapp.net" {
		return invalid("protocol identity does not match phone")
	}
	return nil
}

func validatePQCredentials(credentials *protocolv1.AccountCredentials) error {
	if len(credentials.GetPqOneTimePreKeys()) > 1000 {
		return fmt.Errorf("too many PQ prekeys")
	}
	keys := slices.Clone(credentials.GetPqOneTimePreKeys())
	if key := credentials.GetPqLastResortPreKey(); key != nil {
		keys = append(keys, key)
	}
	seen := make(map[int32]bool, len(keys))
	for _, key := range keys {
		if err := wire.ValidateSignalPQPreKey(key, credentials.GetIdentity().GetPublic()); err != nil {
			return err
		}
		if seen[key.GetId()] {
			return fmt.Errorf("duplicate PQ prekey ID")
		}
		seen[key.GetId()] = true
	}
	return nil
}

func validCurvePair(pair *protocolv1.CurveKeyPair) bool {
	secret, public := pair.GetPrivate(), pair.GetPublic()
	if len(public) != 32 {
		return false
	}
	key, err := ecdh.X25519().NewPrivateKey(secret)
	return err == nil && subtle.ConstantTimeCompare(public, key.PublicKey().Bytes()) == 1
}

func requireChatdPlatform(client *protocolv1.ClientMetadata) error {
	switch client.GetPlatform() {
	case protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID, protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS:
		return nil
	default:
		return fmt.Errorf("communication platform is not supported")
	}
}
