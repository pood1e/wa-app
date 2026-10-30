package wire

import (
	"context"
	"encoding/base64"
)

// RegistrationAttestor signs the ENC value with a device-owned key.
type RegistrationAttestor interface {
	SignRegistration(context.Context, []byte) (signature, certificateChain []byte, err error)
}

type WaSafeEnvelope struct {
	Body          string
	Enc           string
	Authorization string
}

func BuildWASafeEnvelope(ctx context.Context, plain, serverPublicKey []byte, attestor RegistrationAttestor, sendAttestation bool) (WaSafeEnvelope, error) {
	frame, err := SealRegistration(plain, serverPublicKey)
	if err != nil {
		return WaSafeEnvelope{}, err
	}
	enc := B64u(frame)
	envelope := WaSafeEnvelope{Body: "ENC=" + enc, Enc: enc}
	if !sendAttestation {
		return envelope, nil
	}
	envelope.Body += "&H="
	if attestor == nil {
		return envelope, nil
	}
	signature, certificates, err := attestor.SignRegistration(ctx, []byte(enc))
	if err != nil {
		return WaSafeEnvelope{}, err
	}
	envelope.Body += base64.RawURLEncoding.EncodeToString(signature)
	envelope.Authorization = base64.StdEncoding.EncodeToString(certificates)
	return envelope, nil
}
