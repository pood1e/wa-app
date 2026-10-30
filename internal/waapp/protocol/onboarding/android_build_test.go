package onboarding

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
)

func TestAndroidBuildTokenUsesCompleteProfile(t *testing.T) {
	profile := protocoltest.AndroidBuild(t)
	number := "2025550123"
	got, err := AndroidRegistrationToken(profile, number)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha1.New, profile.GetTokenHmacKey())
	message := append(append(append([]byte{}, profile.GetSigningCertificate()...), profile.GetClassesDexMd5()...), []byte(number)...)
	_, _ = mac.Write(message)
	if got != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatal("build-bound token differs from the HMAC transcript")
	}
	for _, mutate := range []func(*protocolv1.AndroidBuildMaterial){
		func(p *protocolv1.AndroidBuildMaterial) { p.TokenHmacKey[0] ^= 1 },
		func(p *protocolv1.AndroidBuildMaterial) { p.ClassesDexMd5[0] ^= 1 },
	} {
		changed := proto.Clone(profile).(*protocolv1.AndroidBuildMaterial)
		mutate(changed)
		token, err := AndroidRegistrationToken(changed, number)
		if err != nil || token == got {
			t.Fatal("changed APK material did not change the token")
		}
	}
	if other, err := AndroidRegistrationToken(profile, "2025550124"); err != nil || other == got {
		t.Fatal("token is not bound to the national number")
	}
}

func TestAndroidProfileRejectsIncompleteOrMismatchedBuild(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*protocolv1.AndroidBuildMaterial)
	}{
		{"version", func(p *protocolv1.AndroidBuildMaterial) { p.AppVersion = "2.26.26.72" }},
		{"version code", func(p *protocolv1.AndroidBuildMaterial) { p.VersionCode-- }},
		{"KDF key", func(p *protocolv1.AndroidBuildMaterial) { p.TokenHmacKey = nil }},
		{"DEX digest", func(p *protocolv1.AndroidBuildMaterial) { p.ClassesDexMd5 = nil }},
		{"certificate", func(p *protocolv1.AndroidBuildMaterial) { p.SigningCertificate = []byte("private-marker") }},
		{"server key", func(p *protocolv1.AndroidBuildMaterial) { p.ServerPublicKey = make([]byte, 32) }},
		{"trust anchor", func(p *protocolv1.AndroidBuildMaterial) { p.NoiseRootPublicKey = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := protocoltest.AndroidBuild(t)
			test.mutate(profile)
			if err := validateAndroidBuild(profile); err == nil || strings.Contains(err.Error(), "private-marker") {
				t.Fatal("invalid profile was accepted or exposed in an error")
			}
		})
	}
	if _, err := AndroidRegistrationToken(nil, "2025550123"); err == nil {
		t.Fatal("missing build profile was accepted")
	}
}

func TestDefaultAndroidBuildOwnsBuildMaterial(t *testing.T) {
	first, err := DefaultAndroidBuild()
	if err != nil {
		t.Fatal(err)
	}
	want := proto.Clone(first)
	second, err := DefaultAndroidBuild()
	if err != nil {
		t.Fatal(err)
	}
	if first.GetApplicationIntegrity().GetApkSize() != 136363853 {
		t.Fatal("bundled build does not match the analyzed APK")
	}
	for _, value := range [][]byte{
		first.TokenHmacKey, first.SigningCertificate, first.ClassesDexMd5,
		first.ServerPublicKey, first.NoiseRootPublicKey,
		first.ApplicationIntegrity.ApkSha256, first.ApplicationIntegrity.ApkPrefixSha256,
		first.ApplicationIntegrity.PrimaryDexSha256, first.ApplicationIntegrity.CertificateSha1,
	} {
		clear(value)
	}
	first.ApplicationIntegrity.ApkSize = 0
	if !proto.Equal(second, want) {
		t.Fatal("changing one profile corrupted another flow's build material")
	}
	third, err := DefaultAndroidBuild()
	if err != nil || !proto.Equal(third, want) {
		t.Fatal("changing one profile corrupted the bundled build material")
	}
}
