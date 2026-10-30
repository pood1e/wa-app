package onboarding

import (
	"testing"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"google.golang.org/protobuf/proto"
)

func TestAndroidFeaturesFollowAPKDefaultsAndServerOverrides(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  string
		enabled bool
	}{
		{"APK defaults", "", false},
		{"server disabled", `[{"config_code":3753,"config_value":0},{"config_code":4435,"config_value":0},{"config_code":7999,"config_value":0}]`, false},
		{"server enabled", `[{"config_code":3753,"config_value":1},{"config_code":4435,"config_value":1},{"config_code":7999,"config_value":1}]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := state.NativeState{}
			current.PreChatdAB.ExpConfig = tc.config
			summary := NativePreChatdABLogSummary(current)
			if summary.GPIAEnabled != tc.enabled || summary.SIMSignalEnabled != tc.enabled || !summary.KeyAttestationEnabled {
				t.Fatal("registration feature defaults or server overrides changed")
			}
			fields := NativeDeviceMapFields(current)
			for _, key := range []string{"sim_type", "airplane_mode_type", "cellular_strength", "roaming_type"} {
				if (fields[key] != "") != tc.enabled {
					t.Fatalf("%s presence does not follow the SIM signal gate", key)
				}
			}
			params := OrderedParams{}
			ApplyAndroidADBState(&params, &protocolv1.AndroidRegistrationOptions{AdbEnabled: proto.Bool(true)}, current)
			if (params.Get("db") == "1") != tc.enabled {
				t.Fatal("ADB observation does not follow its gate")
			}
		})
	}
}
