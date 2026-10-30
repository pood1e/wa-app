package onboarding

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"errors"
	"strings"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

//go:embed android-2.26.40.76.pb
var androidBuildData string

// DefaultAndroidBuild returns an independent copy of the bundled APK material.
func DefaultAndroidBuild() (*protocolv1.AndroidBuildMaterial, error) {
	build := new(protocolv1.AndroidBuildMaterial)
	if err := proto.Unmarshal([]byte(androidBuildData), build); err != nil {
		return nil, errors.New("decode bundled Android registration build")
	}
	if err := validateAndroidBuild(build); err != nil {
		return nil, err
	}
	return build, nil
}

func validateAndroidBuild(build *protocolv1.AndroidBuildMaterial) error {
	if build.GetAppVersion() != wire.DefaultWAAppVersion || build.GetVersionCode() != wire.DefaultWAAppVersionCode {
		return errors.New("unsupported Android registration build")
	}
	if len(build.GetTokenHmacKey()) != 64 || len(build.GetClassesDexMd5()) != md5.Size {
		return errors.New("invalid Android registration token material")
	}
	if len(build.GetNoiseRootPublicKey()) != 32 {
		return errors.New("invalid Noise certificate trust anchor")
	}
	if len(build.GetSigningCertificate()) > 16<<10 {
		return errors.New("invalid Android signing certificate")
	}
	if _, err := x509.ParseCertificate(build.GetSigningCertificate()); err != nil {
		return errors.New("invalid Android signing certificate")
	}
	material := build.GetApplicationIntegrity()
	if material.GetApkSize() == 0 || len(material.GetApkSha256()) != 32 || len(material.GetApkPrefixSha256()) != 32 || len(material.GetPrimaryDexSha256()) != 32 {
		return errors.New("invalid Android application integrity material")
	}
	digest := sha1.Sum(build.GetSigningCertificate())
	if !bytes.Equal(digest[:], material.GetCertificateSha1()) {
		return errors.New("Android certificate digest does not match")
	}
	server, err := ecdh.X25519().NewPublicKey(build.GetServerPublicKey())
	if err != nil {
		return errors.New("invalid Android registration server key")
	}
	// Reject low-order server keys before generating any registration request.
	probe, err := ecdh.X25519().NewPrivateKey(make([]byte, 32))
	if err != nil {
		return err
	}
	if _, err := probe.ECDH(server); err != nil {
		return errors.New("invalid Android registration server key")
	}
	return nil
}

// AndroidRegistrationToken binds the phone to one explicit APK build.
func AndroidRegistrationToken(build *protocolv1.AndroidBuildMaterial, nationalNumber string) (string, error) {
	if err := validateAndroidBuild(build); err != nil {
		return "", err
	}
	if nationalNumber == "" || len(nationalNumber) > 14 || strings.IndexFunc(nationalNumber, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return "", errors.New("invalid registration national number")
	}
	mac := hmac.New(sha1.New, build.GetTokenHmacKey())
	_, _ = mac.Write(build.GetSigningCertificate())
	_, _ = mac.Write(build.GetClassesDexMd5())
	_, _ = mac.Write([]byte(nationalNumber))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}
