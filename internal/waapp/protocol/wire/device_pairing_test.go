package wire

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"go.mau.fi/libsignal/ecc"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

func syntheticPairing(t *testing.T) (*protocolv1.CurveKeyPair, *DevicePairingQR, string) {
	t.Helper()
	generate := func() *ecdh.PrivateKey {
		t.Helper()
		key, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	account, noise, identity := generate(), generate(), generate()
	secret := bytes.Repeat([]byte{7}, 32)
	encode := base64.StdEncoding.EncodeToString
	value := strings.Join([]string{"synthetic-qr-reference", encode(noise.PublicKey().Bytes()), encode(identity.PublicKey().Bytes()), encode(secret), "1"}, ",")
	qr, err := ParseDevicePairingQR(value)
	if err != nil {
		t.Fatal(err)
	}
	return &protocolv1.CurveKeyPair{Public: account.PublicKey().Bytes(), Private: account.Bytes()}, qr, value
}

func TestPairingQRSupportsNativeAndOfficialDeepLinkFormats(t *testing.T) {
	_, expected, value := syntheticPairing(t)
	for _, input := range []string{value, value[:strings.LastIndex(value, ",")], "https://wa.me/settings/linked_devices#" + value} {
		qr, err := ParseDevicePairingQR(input)
		if err != nil || qr.Reference != expected.Reference || !bytes.Equal(qr.Secret, expected.Secret) || !bytes.Equal(qr.NoisePublic, expected.NoisePublic) || !bytes.Equal(qr.IdentityPublic, expected.IdentityPublic) {
			t.Fatal("native pairing QR changed its key binding", err)
		}
	}
	for _, input := range []string{"", strings.Repeat("x", 8193), "https://evil.invalid/#" + value,
		"https://wa.me/settings/linked_devices?redirect=evil#" + value, strings.Replace(value, "synthetic-qr-reference", "short", 1),
		strings.Replace(value, base64.StdEncoding.EncodeToString(expected.IdentityPublic), base64.StdEncoding.EncodeToString(make([]byte, 32)), 1), value + ",unexpected"} {
		if _, err := ParseDevicePairingQR(input); err == nil {
			t.Fatal("invalid pairing QR was accepted")
		}
	}
}

func TestPairingProofMatchesTheCompanionVerificationProtocol(t *testing.T) {
	account, qr, _ := syntheticPairing(t)
	details := &waAdv.ADVDeviceIdentity{RawID: proto.Uint32(1234), Timestamp: proto.Uint64(1800000000), KeyIndex: proto.Uint32(3),
		AccountType: waAdv.ADVEncryptionType_E2EE.Enum(), DeviceType: waAdv.ADVEncryptionType_E2EE.Enum()}
	proof, err := SignDevicePairing(account, details, qr, []uint32{2, 0, 2})
	if err != nil {
		t.Fatal(err)
	}
	var envelope waAdv.ADVSignedDeviceIdentityHMAC
	var signed waAdv.ADVSignedDeviceIdentity
	var signedList waAdv.ADVSignedKeyIndexList
	var list waAdv.ADVKeyIndexList
	if proto.Unmarshal(proof.Identity, &envelope) != nil || proto.Unmarshal(envelope.GetDetails(), &signed) != nil ||
		proto.Unmarshal(proof.KeyIndexList, &signedList) != nil || proto.Unmarshal(signedList.GetDetails(), &list) != nil {
		t.Fatal("pairing proof did not reuse the upstream ADV schemas")
	}
	mac := hmac.New(sha256.New, qr.Secret)
	_, _ = mac.Write(envelope.GetDetails())
	if !hmac.Equal(mac.Sum(nil), envelope.GetHMAC()) || len(signed.GetDeviceSignature()) != 0 || !bytes.Equal(signed.GetAccountSignatureKey(), account.GetPublic()) {
		t.Fatal("pairing HMAC or account-key binding differs from native")
	}
	accountInput := append(append([]byte{6, 0}, signed.GetDetails()...), qr.IdentityPublic...)
	public := ecc.NewDjbECPublicKey([32]byte(account.GetPublic()))
	if !ecc.VerifySignature(public, accountInput, [64]byte(signed.GetAccountSignature())) ||
		!ecc.VerifySignature(public, append([]byte{6, 2}, signedList.GetDetails()...), [64]byte(signedList.GetAccountSignature())) {
		t.Fatal("independent libsignal verification rejected the primary signatures")
	}
	if !proto.Equal(details, &waAdv.ADVDeviceIdentity{RawID: list.RawID, Timestamp: list.Timestamp, KeyIndex: list.CurrentIndex,
		AccountType: list.AccountType, DeviceType: waAdv.ADVEncryptionType_E2EE.Enum()}) || len(list.GetValidIndexes()) != 3 || len(proof.NonceKey) != 32 {
		t.Fatal("pairing proof lost its monotonic key index, timestamp or nonce key")
	}
}

func TestPairingNonceKeyUsesTheCompanionNoisePublicKeyAsSalt(t *testing.T) {
	account, qr, _ := syntheticPairing(t)
	qr.NoisePublic = bytes.Repeat([]byte{9}, 32)
	details := &waAdv.ADVDeviceIdentity{RawID: proto.Uint32(12), KeyIndex: proto.Uint32(1)}
	proof, err := SignDevicePairing(account, details, qr, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(proof.NonceKey) != "3ea6cb73eac6990cc55498be75360e4c1188aec23b81e48a2ee8d620340443fc" {
		t.Fatal("canonical companion nonce derivation differs from the native HKDF vector")
	}
}

func FuzzParseDevicePairingQR(f *testing.F) {
	f.Add("")
	f.Add("https://wa.me/settings/linked_devices#not-a-pairing-code")
	f.Add("synthetic,invalid,invalid,invalid,1")
	f.Fuzz(func(t *testing.T, value string) {
		qr, err := ParseDevicePairingQR(value)
		if err == nil && (len(qr.Secret) != 32 || len(qr.NoisePublic) != 32 || len(qr.IdentityPublic) != 32) {
			t.Fatal("parser accepted an incomplete pairing context")
		}
	})
}
