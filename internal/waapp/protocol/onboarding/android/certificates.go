package android

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

// Google publishes these attestation roots at https://android.googleapis.com/attestation/root.
//
//go:embed roots.pem
var trustedRoots []byte

var attestationOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 11129, 2, 1, 17}

type keyDescription struct {
	AttestationVersion int
	AttestationLevel   asn1.Enumerated
	KeymasterVersion   int
	KeymasterLevel     asn1.Enumerated
	Challenge          []byte
	UniqueID           []byte
	SoftwareEnforced   asn1.RawValue
	HardwareEnforced   asn1.RawValue
}

type applicationID struct {
	Packages []packageInfo `asn1:"set"`
	Digests  [][]byte      `asn1:"set"`
}

type packageInfo struct {
	Name    []byte
	Version int64
}

func verifyCertificates(chain [][]byte, publicKey []byte, build *protocolv1.AndroidBuildMaterial, now time.Time, rootsPEM []byte) ([]*x509.Certificate, error) {
	if len(chain) < 2 || len(chain) > 8 {
		return nil, errors.New("invalid Android certificate chain length")
	}
	certificates := make([]*x509.Certificate, 0, len(chain))
	total := 0
	for _, der := range chain {
		total += len(der)
		if total > 32768 {
			return nil, errors.New("Android certificate chain is too large")
		}
		certificate, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, errors.New("invalid Android attestation certificate")
		}
		certificates = append(certificates, certificate)
	}
	for i := 0; i < len(certificates)-1; i++ {
		if err := certificates[i].CheckSignatureFrom(certificates[i+1]); err != nil {
			return nil, errors.New("Android certificate chain order is invalid")
		}
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootsPEM) {
		return nil, errors.New("Android attestation roots are unavailable")
	}
	intermediates := x509.NewCertPool()
	for _, certificate := range certificates[1:] {
		intermediates.AddCert(certificate)
	}
	_, err := certificates[0].Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: intermediates, CurrentTime: now,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return nil, errors.New("Android attestation certificate chain is untrusted")
	}
	for _, extension := range certificates[0].Extensions {
		if !extension.Id.Equal(attestationOID) {
			continue
		}
		var description keyDescription
		if rest, err := asn1.Unmarshal(extension.Value, &description); err != nil || len(rest) != 0 {
			return nil, errors.New("invalid Android key description")
		}
		challenge := description.Challenge
		if len(challenge) != 41 || challenge[8] != 0x1f || !bytes.Equal(challenge[9:], publicKey) {
			return nil, errors.New("Android attestation challenge does not match the flow")
		}
		issued := time.Unix(int64(binary.BigEndian.Uint64(challenge[:8])), 0)
		if issued.Before(now.Add(-5*time.Minute)) || issued.After(now.Add(time.Minute)) {
			return nil, errors.New("Android attestation challenge is stale")
		}
		if description.AttestationLevel == 0 || description.KeymasterLevel == 0 {
			return nil, errors.New("Android key is not hardware attested")
		}
		if err := verifyApplication(description.SoftwareEnforced, build); err != nil {
			return nil, err
		}
		return certificates, nil
	}
	return nil, errors.New("Android key attestation extension is missing")
}

func verifyApplication(authorization asn1.RawValue, build *protocolv1.AndroidBuildMaterial) error {
	data := authorization.Bytes
	for len(data) > 0 {
		var value asn1.RawValue
		var err error
		data, err = asn1.Unmarshal(data, &value)
		if err != nil {
			return errors.New("invalid Android authorization list")
		}
		if value.Class != asn1.ClassContextSpecific || value.Tag != 709 {
			continue
		}
		var encoded []byte
		if rest, err := asn1.Unmarshal(value.Bytes, &encoded); err != nil || len(rest) != 0 {
			return errors.New("invalid Android attestation application ID")
		}
		var application applicationID
		if rest, err := asn1.Unmarshal(encoded, &application); err != nil || len(rest) != 0 {
			return errors.New("invalid Android attestation application ID")
		}
		if len(application.Packages) != 1 || string(application.Packages[0].Name) != "com.whatsapp" {
			return errors.New("Android attestation application identity is invalid")
		}
		digest := sha256.Sum256(build.GetSigningCertificate())
		if len(application.Digests) != 1 || !bytes.Equal(application.Digests[0], digest[:]) {
			return errors.New("Android attestation signing certificate does not match the APK")
		}
		return nil
	}
	return errors.New("Android attestation application ID is missing")
}
