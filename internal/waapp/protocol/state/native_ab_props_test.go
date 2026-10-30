package state

import (
	"testing"
	"time"
)

func TestRegistrationConfigDeltaRetainsUnchangedFlags(t *testing.T) {
	current := NativeState{}
	now := time.Unix(1700000000, 0)
	current.PreChatdAB.ApplyResponse(map[string]any{"status": "ok", "ab_hash": "synthetic-hash", "exp_cfg": `[{"config_code":3753,"config_value":1},{"config_code":7999,"config_value":1}]`}, now)
	for _, update := range []string{`[]`, `"[]"`, `wamsys initialization fails`, `invalid-json`} {
		current.PreChatdAB.ApplyResponse(map[string]any{"status": "ok", "exp_cfg": update}, now.Add(time.Minute))
		configs := NativePreChatdABConfigs(current)
		if len(configs) != 2 || !NativeABBoolFromConfigs(configs, NativeABCodeGPIA, false) || !NativeABBoolFromConfigs(configs, NativeABCodeADBState, false) {
			t.Fatal("unchanged or invalid response discarded cached registration config")
		}
	}
	current.PreChatdAB.ApplyResponse(map[string]any{"status": "ok", "ab_hash": "updated-hash", "exp_cfg": `[{"config_code":3753,"config_value":0},{"config_code":4435,"config_value":1}]`}, now.Add(2*time.Minute))
	configs := NativePreChatdABConfigs(current)
	if len(configs) != 3 || NativeABBoolFromConfigs(configs, NativeABCodeGPIA, true) || !NativeABBoolFromConfigs(configs, NativeABCodeADBState, false) || !NativeABBoolFromConfigs(configs, NativeABCodeSIMSignal, false) {
		t.Fatal("changed entries did not merge with the cached registration config")
	}
	if current.PreChatdAB.Hash != "updated-hash" || current.PreChatdAB.Due(now.Add(2*time.Minute)) {
		t.Fatal("config hash or refresh scheduling was not updated")
	}
}
