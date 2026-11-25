package wire

import (
	"errors"

	"go.mau.fi/libsignal/ecc"
)

// SignalSignCurve25519 preserves the Edwards sign bit used by native Signal signatures.
func SignalSignCurve25519(private, message []byte) ([]byte, error) {
	if len(private) != 32 {
		return nil, errors.New("Signal signature private key has invalid length")
	}
	EnsureSignalLogger()
	signature := ecc.CalculateSignature(ecc.NewDjbECPrivateKey([32]byte(private)), message)
	return signature[:], nil
}

// SignalVerifyCurve25519 verifies the native Signal variant, including its sign bit.
func SignalVerifyCurve25519(public, message, signature []byte) (bool, error) {
	public, err := StripSignalCurvePrefix(public)
	if err != nil {
		return false, err
	}
	if len(public) != 32 || len(signature) != 64 {
		return false, errors.New("Signal signature or public key has invalid length")
	}
	EnsureSignalLogger()
	return ecc.VerifySignature(ecc.NewDjbECPublicKey([32]byte(public)), message, [64]byte(signature)), nil
}
