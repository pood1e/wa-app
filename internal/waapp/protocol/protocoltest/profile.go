// Package protocoltest provides synthetic protocol material for isolated tests.
package protocoltest

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

// AndroidProfile contains no material from an APK or account.
func AndroidBuild(t testing.TB) *protocolv1.AndroidBuildMaterial {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic build"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Unix(1<<32, 0)}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, private)
	if err != nil {
		t.Fatal(err)
	}
	server, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := sha512.Sum512([]byte("synthetic APK token key"))
	digest := sha256.Sum256([]byte("synthetic DEX"))
	certificateDigest := sha1.Sum(der)
	return &protocolv1.AndroidBuildMaterial{
		AppVersion: wire.DefaultWAAppVersion, VersionCode: wire.DefaultWAAppVersionCode,
		TokenHmacKey: key[:], SigningCertificate: der, ClassesDexMd5: digest[:16],
		ServerPublicKey: server.PublicKey().Bytes(), NoiseRootPublicKey: root.PublicKey().Bytes(),
		ApplicationIntegrity: &protocolv1.AndroidApplicationIntegrity{ApkSize: 4096, ApkSha256: digest[:], ApkPrefixSha256: digest[:],
			PrimaryDexSha256: digest[:], CertificateSha1: certificateDigest[:]},
	}
}

func AndroidClient() *protocolv1.AndroidClientParameters {
	digest := sha256.Sum256([]byte("synthetic client runtime"))
	return &protocolv1.AndroidClientParameters{
		Metadata: &protocolv1.ClientMetadata{Platform: protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID,
			OsVersion: "17", Manufacturer: "Synthetic", Device: "synthetic-device",
			OsBuildNumber: "TEST.001", DeviceBoard: "test-board", ModelType: "TestPhone"},
		MemoryGib: "8.00", Options: &protocolv1.AndroidRegistrationOptions{},
		RequestedPermissionsSha256: digest[:], NativeLibraryNamesSha256: append([]byte(nil), digest[:]...),
		DataSoNamesSha256: append([]byte(nil), digest[:]...),
	}
}
