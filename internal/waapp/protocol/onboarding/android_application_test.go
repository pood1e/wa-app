package onboarding

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	modelv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/model/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestApplicationIntegrityBindsEveryEnvelopeToTheSelectedApp(t *testing.T) {
	for _, business := range []bool{false, true} {
		t.Run(map[bool]string{false: "personal", true: "business"}[business], func(t *testing.T) {
			build, err := DefaultAndroidBuild()
			parameters, expectedPackage := DefaultAndroidClient(), "com.whatsapp"
			if business {
				build, err = DefaultBusinessAndroidBuild()
				parameters, expectedPackage = DefaultBusinessAndroidClient(), "com.whatsapp.w4b"
				parameters.Business = &modelv1.BusinessRegistration{Name: "WA Lab"}
			}
			if err != nil {
				t.Fatal(err)
			}
			flow, err := NewAndroidFlow(t.Context(), &waappv1.PhoneTarget{CountryCallingCode: "1", NationalNumber: "2025550123"}, build, parameters, "", AndroidDependencies{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = flow.Close() })
			material, err := state.BuildNativeGPIAMaterial(state.WamsysMaterialInput{State: flow.current, Build: build, Client: parameters, Integrity: &waappv1.AndroidIntegrityResult{Outcome: &waappv1.AndroidIntegrityResult_ErrorCode{ErrorCode: 1000}}})
			if err != nil {
				t.Fatal(err)
			}
			key := sha256.Sum256([]byte(state.NativeGPIAKeySource(flow.current)))
			decrypt := func(value string) []byte {
				t.Helper()
				raw, err := base64.StdEncoding.DecodeString(value)
				if err != nil || len(raw) < 32 {
					t.Fatal("invalid integrity envelope")
				}
				plaintext, err := wire.AesCBCPKCS7Decrypt(raw[16:], key[:], raw[:16])
				if err != nil {
					t.Fatal(err)
				}
				return plaintext
			}
			var primary struct {
				Package string `json:"packageName"`
				Path    string `json:"p"`
			}
			var compact struct {
				Package string `json:"_ip"`
				Path    string `json:"_p"`
			}
			if json.Unmarshal(decrypt(material.Primary), &primary) != nil || json.Unmarshal(decrypt(material.DeviceCompact), &compact) != nil || primary.Package != expectedPackage || compact.Package != expectedPackage || primary.Path != compact.Path || !strings.Contains(primary.Path, "/"+expectedPackage+"-") {
				t.Fatal("integrity envelopes mixed personal and business application identity")
			}
		})
	}
}
