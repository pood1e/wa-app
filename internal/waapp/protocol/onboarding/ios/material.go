package ios

import (
	"crypto/ecdh"
	"errors"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

// Material exports retained keys without device or proof context.
// Uncertain registration material must authenticate before account acceptance.
func (f *Flow) Material(loginID, accountID string) (*waappv1.AcceptedAccountMaterial, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available(registrationComplete) && !f.available(registrationVerifyUncertain) && !f.available(registrationPasswordUncertain) {
		return nil, ErrRegistrationState
	}
	expectedLoginID := f.phone.GetCountryCallingCode() + f.phone.GetNationalNumber()
	expectedAccountID := expectedLoginID
	if f.stage == registrationComplete {
		expectedLoginID = f.registered.GetLogin()
		expectedAccountID = f.registered.GetLid()
	}
	if expectedAccountID == "" {
		expectedAccountID = expectedLoginID
	}
	if accountID == "" {
		accountID = expectedAccountID
	}
	if loginID != expectedLoginID || accountID != expectedAccountID ||
		loginID != f.phone.GetCountryCallingCode()+f.phone.GetNationalNumber() {
		return nil, errors.New("registered account identity does not match this flow")
	}
	chat, err := registrationPair(f.keys.GetNoisePrivateKey())
	if err != nil {
		return nil, err
	}
	identity, err := registrationPair(f.keys.GetIdentityPrivateKey())
	if err != nil {
		return nil, err
	}
	signed, err := registrationPair(f.keys.GetSignedPreKeyPrivateKey())
	if err != nil {
		return nil, err
	}
	phone := proto.Clone(f.phone).(*waappv1.PhoneTarget)
	phone.E164Number = "+" + loginID
	return &waappv1.AcceptedAccountMaterial{
		Phone: phone, ServiceLoginId: loginID, ServiceAccountId: accountID,
		Credentials: &protocolv1.AccountCredentials{ChatStatic: chat, Identity: identity, RegistrationId: int32(f.keys.GetRegistrationId()),
			SignedPreKey: &protocolv1.SignalPreKey{Id: int32(f.keys.GetSignedPreKeyId()), KeyPair: signed}},
		Recovery: &protocolv1.AccountRecoveryState{Client: proto.Clone(f.client).(*protocolv1.ClientMetadata), RegistrationJid: loginID + "@s.whatsapp.net"},
	}, nil
}

func registrationPair(private []byte) (*protocolv1.CurveKeyPair, error) {
	key, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		return nil, errors.New("invalid registration key material")
	}
	return &protocolv1.CurveKeyPair{Public: key.PublicKey().Bytes(), Private: key.Bytes()}, nil
}
