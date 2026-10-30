// Package ios implements iOS account onboarding wire formats.
package ios

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
)

// RegistrationAuthorization encodes the original key attestation and SDK key ID.
func RegistrationAuthorization(keyIdentifier string, keyAttestation []byte) (string, error) {
	key, err := base64.StdEncoding.Strict().DecodeString(keyIdentifier)
	if err != nil || len(key) != 32 || base64.StdEncoding.EncodeToString(key) != keyIdentifier {
		return "", errors.New("app attest key identifier is invalid")
	}
	if len(keyAttestation) == 0 || len(keyAttestation) > 16*1024 {
		return "", errors.New("app attest key attestation size is invalid")
	}
	return base64.RawURLEncoding.EncodeToString(keyAttestation) + "|" +
		base64.RawURLEncoding.EncodeToString([]byte(keyIdentifier)), nil
}

// RegistrationBody encodes a sealed ENC frame and its optional App Attest assertion.
// The assertion must bind the SHA-256 of the same binary frame.
func RegistrationBody(frame, assertion []byte) ([]byte, error) {
	if len(frame) < 32+16 {
		return nil, errors.New("registration frame is shorter than public key and tag")
	}
	form := url.Values{}
	form.Set("ENC", base64.URLEncoding.EncodeToString(frame))
	if len(assertion) != 0 {
		payload := struct {
			Assertion []byte `json:"assertion"`
		}{Assertion: assertion}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		encoded = bytes.ReplaceAll(encoded, []byte("/"), []byte(`\/`))
		form.Set("H", base64.RawURLEncoding.EncodeToString(encoded))
	}
	return []byte(form.Encode()), nil
}
