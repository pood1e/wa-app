package wire

import (
	"bytes"
	"encoding/binary"
	"errors"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

func verifyNoiseCertificate(payload, serverStatic, rootPublic []byte) error {
	if len(rootPublic) != 32 {
		return errors.New("Noise certificate trust anchor is required")
	}
	chain := new(protocolv1.NoiseCertificateChain)
	if err := proto.Unmarshal(payload, chain); err != nil || chain.Leaf == nil || chain.Intermediate == nil {
		return errors.New("invalid Noise certificate chain")
	}
	leaf, intermediate := new(protocolv1.NoiseCertificateDetails), new(protocolv1.NoiseCertificateDetails)
	if err := proto.Unmarshal(chain.GetLeaf().GetDetails(), leaf); err != nil {
		return errors.New("invalid Noise leaf certificate")
	}
	if err := proto.Unmarshal(chain.GetIntermediate().GetDetails(), intermediate); err != nil {
		return errors.New("invalid Noise intermediate certificate")
	}
	if intermediate.Serial == nil || intermediate.IssuerSerial == nil || leaf.IssuerSerial == nil ||
		intermediate.GetIssuerSerial() != 0 || leaf.GetIssuerSerial() != intermediate.GetSerial() ||
		!bytes.Equal(leaf.GetKey(), serverStatic) || len(intermediate.GetKey()) != 32 {
		return errors.New("Noise certificate identity does not match")
	}
	// wa6 verifies these two signatures; expiry checks belong to the wa5 format.
	for _, item := range []struct {
		certificate *protocolv1.NoiseCertificate
		public      []byte
	}{{chain.GetIntermediate(), rootPublic}, {chain.GetLeaf(), intermediate.GetKey()}} {
		valid, err := XeddsaVerifyCurve25519(item.public, item.certificate.GetDetails(), item.certificate.GetSignature())
		if err != nil || !valid {
			return errors.New("invalid Noise certificate signature")
		}
	}
	return nil
}

func consumeNoiseTLV(data []byte, kind byte, length int) ([]byte, []byte, error) {
	if len(data) < 3 || data[0] != kind || int(binary.BigEndian.Uint16(data[1:3])) != length || len(data)-3 < length {
		return nil, nil, errors.New("invalid Noise extension")
	}
	return data[3 : 3+length], data[3+length:], nil
}

func parseNoisePQCertificate(payload []byte) ([]byte, []byte, error) {
	header, rest, err := consumeNoiseTLV(payload, 1, len("MLKEM512"))
	if err != nil || !bytes.Equal(header, []byte("MLKEM512")) {
		return nil, nil, errors.New("invalid Noise KEM algorithm")
	}
	key, certificate, err := consumeNoiseTLV(rest, 2, 800)
	if err != nil {
		return nil, nil, err
	}
	return key, certificate, nil
}
