package onboarding

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestWamsysUsesSDKOutcomeAndBuildMaterial(t *testing.T) {
	profile := protocoltest.AndroidBuild(t)
	flow, err := NewAndroidFlow(t.Context(), &waappv1.PhoneTarget{CountryCallingCode: "1", NationalNumber: "2025550123"}, profile, protocoltest.AndroidClient(), "", AndroidDependencies{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Close() })
	flow.current.PreChatdAB.ExpConfig = `[{"config_code":3753,"config_value":1}]`
	cases := []struct {
		name    string
		result  *waappv1.AndroidIntegrityResult
		code    int
		token   string
		invalid bool
	}{
		{name: "missing"},
		{name: "success", result: &waappv1.AndroidIntegrityResult{Outcome: &waappv1.AndroidIntegrityResult_Token{Token: "synthetic-sdk-token"}}, token: "synthetic-sdk-token"},
		{name: "empty token", result: &waappv1.AndroidIntegrityResult{Outcome: &waappv1.AndroidIntegrityResult_Token{}}, invalid: true},
		{name: "zero error", result: &waappv1.AndroidIntegrityResult{Outcome: &waappv1.AndroidIntegrityResult_ErrorCode{}}, invalid: true},
		{name: "missing outcome", result: &waappv1.AndroidIntegrityResult{}, invalid: true},
	}
	for _, code := range []int32{-2, 1000, 1004, 1005} {
		cases = append(cases, struct {
			name    string
			result  *waappv1.AndroidIntegrityResult
			code    int
			token   string
			invalid bool
		}{name: "SDK error", result: &waappv1.AndroidIntegrityResult{Outcome: &waappv1.AndroidIntegrityResult_ErrorCode{ErrorCode: code}}, code: int(code)})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := state.WamsysMaterialInput{Kind: protocolv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_REGISTER, Phone: flow.phone, State: flow.current, Build: profile, Client: flow.parameters, Integrity: tc.result}
			capture, err := (state.LocalWamsysMaterialProvider{}).RegistrationMaterial(t.Context(), input)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid SDK outcome was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			fields := make(map[string][]byte)
			for _, field := range capture.GetMapParams() {
				fields[field.GetKey()] = field.GetValue()
			}
			if tc.result == nil {
				for _, key := range []string{"gpia", "_gi", "_gg"} {
					if len(fields[key]) != 0 {
						t.Fatal("missing SDK result produced a synthetic outcome")
					}
				}
				return
			}
			key := sha256.Sum256([]byte(state.NativeGPIAKeySource(flow.current)))
			decrypt := func(name string) []byte {
				t.Helper()
				encrypted, err := base64.StdEncoding.DecodeString(string(fields[name]))
				if err != nil || len(encrypted) < 32 {
					t.Fatalf("invalid %s envelope", name)
				}
				plain, err := wire.AesCBCPKCS7Decrypt(encrypted[16:], key[:], encrypted[:16])
				if err != nil {
					t.Fatal(err)
				}
				return plain
			}
			var primary struct {
				Code   int    `json:"code"`
				Token  string `json:"token"`
				Size   string `json:"sizeInBytes"`
				SHA256 string `json:"sha256"`
			}
			if err := json.Unmarshal(decrypt("gpia"), &primary); err != nil {
				t.Fatal(err)
			}
			if primary.Code != tc.code || primary.Token != tc.token || primary.Size != "4096" || primary.SHA256 != base64.StdEncoding.EncodeToString(profile.GetApplicationIntegrity().GetApkSha256()) {
				t.Fatal("primary envelope changed SDK outcome or APK material")
			}
			var compact struct {
				Code  int    `json:"_ic"`
				Token string `json:"_it"`
			}
			if err := json.Unmarshal(decrypt("_gg"), &compact); err != nil || compact.Code != tc.code || compact.Token != tc.token {
				t.Fatal("compact envelope changed SDK outcome")
			}
		})
	}
}
