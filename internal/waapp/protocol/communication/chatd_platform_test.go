package communication

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestChatdUserAgentUsesPersistedPlatform(t *testing.T) {
	for _, test := range []struct {
		name     string
		platform protocolv1.ClientPlatform
		want     uint64
	}{
		{"Android", protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID, 0},
		{"iOS", protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &protocolv1.ClientMetadata{Platform: test.platform, AppVersion: "2.26.40.77", Manufacturer: "Apple", Device: "iPhone12,3"}
			agent := ChatdUserAgentForState(state.NativeState{Client: client}, "unused-version")
			data := wire.BuildUserAgentPayload(agent)
			number, kind, size := protowire.ConsumeTag(data)
			if size < 0 || number != 1 || kind != protowire.VarintType {
				t.Fatal("user agent has no platform field")
			}
			got, size := protowire.ConsumeVarint(data[size:])
			if size < 0 || got != test.want {
				t.Fatalf("native platform = %d, want %d", got, test.want)
			}
			if agent.Version != client.GetAppVersion() || agent.Device != client.GetDevice() || agent.Manufacturer != client.GetManufacturer() {
				t.Fatal("user agent replaced persisted client metadata")
			}
		})
	}
}
