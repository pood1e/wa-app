package ios

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	modelv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/model/v1"
	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

func TestBundledProfilesKeepBuildAndClientVersionsAligned(t *testing.T) {
	for _, test := range []struct {
		name  string
		load  func() (*protocolv1.IOSRegistrationProfile, error)
		kind  modelv1.WAAccountType
		agent string
	}{
		{"personal", DefaultProfile, modelv1.WAAccountType_WA_ACCOUNT_TYPE_PERSONAL, "WhatsApp/2.26.46.72 iOS/"},
		{"business", DefaultBusinessProfile, modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS, "WhatsApp/2.26.46.72 SMB iOS/"},
	} {
		t.Run(test.name, func(t *testing.T) {
			first, err := test.load()
			if err != nil {
				t.Fatal(err)
			}
			if first.GetAppVersion() != "26.46.72" || first.GetClient().GetAppVersion() != "2.26.46.72" || first.GetClient().GetAccountType() != test.kind || !strings.HasPrefix(first.GetUserAgent(), test.agent) {
				t.Fatal("bundled iOS profile has mismatched native metadata")
			}
			want := proto.Clone(first)
			first.BuildHash = "changed"
			first.BuildTokenSecret = "changed"
			clear(first.Client.NoiseRootPublicKey)
			next, err := test.load()
			if err != nil || !proto.Equal(next, want) {
				t.Fatal("iOS profiles share mutable build material", err)
			}
		})
	}
}

func TestProfileRejectsBuildHashFromAnotherVersion(t *testing.T) {
	for _, hash := range []string{"f230dcaad3b72a6b2bb16182b5a84688", strings.Repeat("z", 32), ""} {
		profile := syntheticProfile()
		profile.BuildHash = hash
		if err := validateProfile(profile); err == nil {
			t.Fatal("profile accepted a build hash from another version")
		}
	}
}
