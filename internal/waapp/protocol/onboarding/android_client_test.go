package onboarding

import (
	"bytes"
	"crypto/sha256"
	"net/url"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

func TestAndroidFlowUsesIndependentLogicalClientParameters(t *testing.T) {
	build, err := DefaultAndroidBuild()
	if err != nil {
		t.Fatal(err)
	}
	buildBefore := proto.Clone(build)
	phone := &waappv1.PhoneTarget{E164Number: "+447700900123", CountryCallingCode: "44", NationalNumber: "7700900123", CountryIso2: "GB"}
	var previousID, previousToken string
	for _, tc := range []struct {
		name       string
		parameters *protocolv1.AndroidClientParameters
		userAgent  string
	}{
		{"default", DefaultAndroidClient(), "WhatsApp/2.26.40.76 Android/17 Device/Google-Pixel_6_Pro"},
		{"other client", protocoltest.AndroidClient(), "WhatsApp/2.26.40.76 Android/17 Device/Synthetic-TestPhone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := proto.Clone(tc.parameters)
			flow, err := NewAndroidFlow(t.Context(), phone, build, tc.parameters, "", AndroidDependencies{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = flow.Close() })
			flow.current.PreChatdAB.ExpConfig = `[{"config_code":7999,"config_value":1}]`
			current := flow.current
			want := proto.Clone(tc.parameters.GetMetadata()).(*protocolv1.ClientMetadata)
			want.AppVersion = build.GetAppVersion()
			want.PhoneId, want.DeviceExperimentId = current.Profile.FDID, current.Profile.ExpIDUUID
			want.LocaleLanguage, want.LocaleCountry = "en", "GB"
			want.HandshakePqMode = tc.parameters.GetOptions().GetHandshakePqMode()
			want.NoiseRootPublicKey = build.GetNoiseRootPublicKey()
			if !proto.Equal(current.Client, want) {
				t.Fatal("logical client metadata changed between registration and chatd")
			}
			if current.Profile.FDID == "" || current.Profile.FDID == previousID {
				t.Fatal("client instances share an identifier")
			}
			previousID = current.Profile.FDID
			if previousToken != "" && flow.token != previousToken {
				t.Fatal("logical client metadata changed the APK/phone token")
			}
			previousToken = flow.token
			if state.NativeUserAgentForState(current, build.GetAppVersion()) != tc.userAgent {
				t.Fatal("HTTP user agent does not describe the logical client")
			}
			params, err := flow.ExistOrderedParamsWithWamsys(t.Context(), phone, current, nil, true, build.GetAppVersion())
			if err != nil {
				t.Fatal(err)
			}
			name, err := url.PathUnescape(params.Get("device_name"))
			if err != nil || name != want.GetDevice() || params.Get("device_ram") != tc.parameters.GetMemoryGib() {
				t.Fatal("registration metadata did not come from logical client parameters")
			}
			if params.Get("_gp") == "" {
				t.Fatal("logical client permissions digest is missing")
			}
			material, err := flow.Material("447700900123", "synthetic-account")
			if err != nil || !proto.Equal(material.GetRecovery().GetClient(), want) {
				t.Fatal("handoff lost logical client metadata")
			}
			if !proto.Equal(tc.parameters, before) || !proto.Equal(build, buildBefore) {
				t.Fatal("flow changed caller-owned client or APK material")
			}
			tc.parameters.Metadata.Device = "changed"
			tc.parameters.RequestedPermissionsSha256[0] ^= 1
			if flow.parameters.GetMetadata().GetDevice() == "changed" || bytes.Equal(flow.parameters.GetRequestedPermissionsSha256(), tc.parameters.GetRequestedPermissionsSha256()) {
				t.Fatal("flow retained caller-owned client parameters")
			}
		})
	}
}

func TestAndroidClientRejectsInvalidParameters(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*protocolv1.AndroidClientParameters)
	}{
		{"metadata", func(p *protocolv1.AndroidClientParameters) { p.Metadata = nil }},
		{"platform", func(p *protocolv1.AndroidClientParameters) {
			p.Metadata.Platform = protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS
		}},
		{"version", func(p *protocolv1.AndroidClientParameters) { p.Metadata.AppVersion = "2.0" }},
		{"build", func(p *protocolv1.AndroidClientParameters) { p.Metadata.OsBuildNumber = "" }},
		{"instance ID", func(p *protocolv1.AndroidClientParameters) { p.Metadata.PhoneId = "private-marker" }},
		{"experiment ID", func(p *protocolv1.AndroidClientParameters) { p.Metadata.DeviceExperimentId = "private-marker" }},
		{"missing memory", func(p *protocolv1.AndroidClientParameters) { p.MemoryGib = "" }},
		{"negative memory", func(p *protocolv1.AndroidClientParameters) { p.MemoryGib = "-1" }},
		{"NaN memory", func(p *protocolv1.AndroidClientParameters) { p.MemoryGib = "NaN" }},
		{"infinite memory", func(p *protocolv1.AndroidClientParameters) { p.MemoryGib = "+Inf" }},
		{"PQ mode", func(p *protocolv1.AndroidClientParameters) { p.Options.HandshakePqMode = 99 }},
		{"permissions", func(p *protocolv1.AndroidClientParameters) { p.RequestedPermissionsSha256 = nil }},
		{"native libraries", func(p *protocolv1.AndroidClientParameters) { p.NativeLibraryNamesSha256 = nil }},
		{"data libraries", func(p *protocolv1.AndroidClientParameters) { p.DataSoNamesSha256 = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parameters := protocoltest.AndroidClient()
			tc.mutate(parameters)
			if err := validateAndroidClient(parameters); err == nil || strings.Contains(err.Error(), "private-marker") {
				t.Fatal("invalid client was accepted or exposed")
			}
		})
	}
	if err := validateAndroidClient(nil); err == nil {
		t.Fatal("missing client parameters were accepted")
	}
}

func TestDefaultAndroidClientOwnsItsRuntimeMaterial(t *testing.T) {
	first, second := DefaultAndroidClient(), DefaultAndroidClient()
	want := proto.Clone(second)
	wantEmpty := sha256.Sum256([]byte("signal_error"))
	for _, got := range [][]byte{first.GetNativeLibraryNamesSha256(), first.GetDataSoNamesSha256()} {
		if !bytes.Equal(got, wantEmpty[:]) {
			t.Fatal("empty library list differs from the native branch")
		}
	}
	first.Metadata.Device = "changed"
	clear(first.RequestedPermissionsSha256)
	clear(first.NativeLibraryNamesSha256)
	first.Options.AdbEnabled = proto.Bool(false)
	if !proto.Equal(second, want) || !proto.Equal(DefaultAndroidClient(), want) {
		t.Fatal("logical clients share mutable defaults")
	}
}
