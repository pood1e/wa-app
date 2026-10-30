package android

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/binary"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

func syntheticCertificates(t *testing.T, challenge []byte, profile *protocolv1.AndroidBuildMaterial, applicationName string, now time.Time) ([][]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	marshal := func(value any) []byte {
		t.Helper()
		der, err := asn1.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return der
	}
	digest := sha256.Sum256(profile.GetSigningCertificate())
	application := marshal(applicationID{Packages: []packageInfo{{Name: []byte(applicationName), Version: int64(profile.GetVersionCode())}}, Digests: [][]byte{digest[:]}})
	authorization := marshal(asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 709, IsCompound: true, Bytes: marshal(application)})
	description := marshal(keyDescription{AttestationVersion: 400, AttestationLevel: 1, KeymasterVersion: 400, KeymasterLevel: 1,
		Challenge: challenge, SoftwareEnforced: asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true, Bytes: authorization}, HardwareEnforced: asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true}})
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtraExtensions: []pkix.Extension{{Id: attestationOID, Value: description}}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, root, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return [][]byte{leafDER, rootDER}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})
}

func TestCertificateProofRequiresMatchingKeyApplicationAndTrust(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	publicKey := bytes.Repeat([]byte{0x42}, 32)
	profile := &protocolv1.AndroidBuildMaterial{VersionCode: 123, SigningCertificate: []byte("synthetic APK certificate")}
	challenge := make([]byte, 41)
	binary.BigEndian.PutUint64(challenge, uint64(now.Unix()))
	challenge[8] = 0x1f
	copy(challenge[9:], publicKey)
	chain, roots := syntheticCertificates(t, challenge, profile, "com.whatsapp", now)
	if _, err := verifyCertificates(chain, publicKey, profile, now, roots); err != nil {
		t.Fatal(err)
	}
	otherBuild := &protocolv1.AndroidBuildMaterial{VersionCode: 124, SigningCertificate: profile.SigningCertificate}
	if _, err := verifyCertificates(chain, publicKey, otherBuild, now, roots); err != nil {
		t.Fatal("SDK application version was coupled to host protocol parameters")
	}
	otherApplication, otherRoots := syntheticCertificates(t, challenge, profile, "com.example.other", now)
	if _, err := verifyCertificates(otherApplication, publicKey, profile, now, otherRoots); err == nil {
		t.Fatal("unrelated application identity was accepted")
	}
	for _, tc := range []struct {
		name    string
		key     []byte
		profile *protocolv1.AndroidBuildMaterial
		now     time.Time
		roots   []byte
	}{
		{"different key", bytes.Repeat([]byte{0x43}, 32), profile, now, roots},
		{"different signing certificate", publicKey, &protocolv1.AndroidBuildMaterial{VersionCode: 123, SigningCertificate: []byte("other")}, now, roots},
		{"stale challenge", publicKey, profile, now.Add(6 * time.Minute), roots},
		{"untrusted root", publicKey, profile, now, trustedRoots},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := verifyCertificates(chain, tc.key, tc.profile, tc.now, tc.roots); err == nil {
				t.Fatal("mismatched proof was accepted")
			}
		})
	}
}
