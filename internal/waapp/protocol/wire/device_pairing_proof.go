package wire

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"slices"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

// DevicePairingProof contains the signed native pair-device payloads.
type DevicePairingProof struct {
	Identity     []byte
	KeyIndexList []byte
	NonceKey     []byte
}

// SignDevicePairing binds the scanned companion identity to this account's key.
func SignDevicePairing(account *protocolv1.CurveKeyPair, details *waAdv.ADVDeviceIdentity, qr *DevicePairingQR, indexes []uint32) (*DevicePairingProof, error) {
	private, err := ecdh.X25519().NewPrivateKey(account.GetPrivate())
	if err != nil || !bytes.Equal(private.PublicKey().Bytes(), account.GetPublic()) {
		return nil, errors.New("linked-device account identity is invalid")
	}
	if qr == nil || len(qr.Secret) != 32 || !validPairingPublicKey(qr.NoisePublic) || !validPairingPublicKey(qr.IdentityPublic) || details.GetRawID() == 0 || details.GetKeyIndex() == 0 ||
		details.GetAccountType() != waAdv.ADVEncryptionType_E2EE || details.GetDeviceType() != waAdv.ADVEncryptionType_E2EE {
		return nil, errors.New("linked-device signing context is invalid")
	}
	rawDetails, err := proto.Marshal(details)
	if err != nil {
		return nil, err
	}
	signed := append([]byte{6, 0}, rawDetails...)
	signed = append(signed, qr.IdentityPublic...)
	signature, err := SignalSignCurve25519(account.GetPrivate(), signed)
	if err != nil {
		return nil, err
	}
	identity, err := proto.Marshal(&waAdv.ADVSignedDeviceIdentity{Details: rawDetails, AccountSignatureKey: account.GetPublic(), AccountSignature: signature})
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, qr.Secret)
	_, _ = mac.Write(identity)
	proof := new(DevicePairingProof)
	proof.Identity, err = proto.Marshal(&waAdv.ADVSignedDeviceIdentityHMAC{Details: identity, HMAC: mac.Sum(nil), AccountType: waAdv.ADVEncryptionType_E2EE.Enum()})
	if err != nil {
		return nil, err
	}
	indexes = append(slices.Clone(indexes), 0, details.GetKeyIndex())
	slices.Sort(indexes)
	proof.KeyIndexList, err = SignDeviceKeyIndexList(account.GetPrivate(), &waAdv.ADVKeyIndexList{RawID: details.RawID, Timestamp: details.Timestamp,
		CurrentIndex: details.KeyIndex, ValidIndexes: slices.Compact(indexes), AccountType: waAdv.ADVEncryptionType_E2EE.Enum()})
	if err != nil {
		return nil, err
	}
	proof.NonceKey, err = hkdf.Key(sha256.New, qr.Secret, qr.NoisePublic, "Canonical Ent Companion Nonce Encrypt", 32)
	return proof, err
}

// SignDeviceKeyIndexList signs the upstream ADV list with the native 06 02 prefix.
func SignDeviceKeyIndexList(private []byte, list *waAdv.ADVKeyIndexList) ([]byte, error) {
	if list.GetRawID() == 0 || list.GetCurrentIndex() == 0 || list.GetAccountType() != waAdv.ADVEncryptionType_E2EE {
		return nil, errors.New("linked-device key index context is invalid")
	}
	details, err := proto.Marshal(list)
	if err != nil {
		return nil, err
	}
	signature, err := SignalSignCurve25519(private, append([]byte{6, 2}, details...))
	if err != nil {
		return nil, err
	}
	return proto.Marshal(&waAdv.ADVSignedKeyIndexList{Details: details, AccountSignature: signature})
}
