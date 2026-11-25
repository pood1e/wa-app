package wire

import (
	"crypto/ecdh"
	"errors"
	"net/url"
	"strings"
)

// DevicePairingQR contains a companion's short-lived pairing authorization.
type DevicePairingQR struct {
	Reference      string
	NoisePublic    []byte
	IdentityPublic []byte
	Secret         []byte
	ClientType     string
}

// ParseDevicePairingQR validates a native payload or official pairing deep link.
func ParseDevicePairingQR(value string) (*DevicePairingQR, error) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 8192 {
		return nil, errors.New("linked-device QR has invalid length")
	}
	if strings.Contains(value, "://") {
		link, err := url.Parse(value)
		if err != nil || link.Scheme != "https" || link.Host != "wa.me" || link.Path != "/settings/linked_devices" || link.User != nil || link.RawQuery != "" {
			return nil, errors.New("linked-device QR must use the official pairing link")
		}
		value = link.Fragment
	}
	fields := strings.Split(value, ",")
	if len(fields) != 4 && len(fields) != 5 {
		return nil, errors.New("linked-device QR has invalid fields")
	}
	if len(fields[0]) < 8 || len(fields[0]) > 512 || strings.ContainsAny(fields[0], "\r\n\t ") {
		return nil, errors.New("linked-device QR has invalid reference")
	}
	result := &DevicePairingQR{Reference: fields[0], ClientType: "0"}
	if len(fields) == 5 {
		result.ClientType = fields[4]
	}
	if !strings.Contains("0123456789ce", result.ClientType) || len(result.ClientType) != 1 {
		return nil, errors.New("linked-device QR has unsupported client type")
	}
	var err error
	result.NoisePublic, err = DecodeB64Any(fields[1])
	if err != nil || !validPairingPublicKey(result.NoisePublic) {
		return nil, errors.New("linked-device QR has invalid Noise public key")
	}
	result.IdentityPublic, err = DecodeB64Any(fields[2])
	if err != nil || !validPairingPublicKey(result.IdentityPublic) {
		return nil, errors.New("linked-device QR has invalid Signal public key")
	}
	result.Secret, err = DecodeB64Any(fields[3])
	if err != nil || len(result.Secret) != 32 {
		return nil, errors.New("linked-device QR has invalid pairing secret")
	}
	return result, nil
}

func validPairingPublicKey(value []byte) bool {
	public, err := ecdh.X25519().NewPublicKey(value)
	if err != nil {
		return false
	}
	private, err := ecdh.X25519().NewPrivateKey(make([]byte, 32))
	if err != nil {
		return false
	}
	_, err = private.ECDH(public)
	return err == nil
}
