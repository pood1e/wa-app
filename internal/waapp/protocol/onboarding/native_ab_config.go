package onboarding

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"strconv"
	"strings"

	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type NativePreChatdABSummary struct {
	ConfigCount            int
	GPIAEnabled            bool
	SIMSignalEnabled       bool
	RecaptchaStage         string
	RequestIDRandom        bool
	KeyAttestationObserved bool
	KeyAttestationEnabled  bool
}

func NativeABIntFromConfigs(configs map[int]pstate.NativePreChatdABConfig, code int) (int, bool) {
	config, ok := configs[code]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(config.Value))
	if err != nil {
		return 0, false
	}
	return n, true
}

func NativePreChatdABLogSummary(state pstate.NativeState) NativePreChatdABSummary {
	configs := pstate.NativePreChatdABConfigs(state)
	_, attestationObserved := configs[0x78e]
	return NativePreChatdABSummary{
		ConfigCount:            len(configs),
		GPIAEnabled:            pstate.NativeABBoolFromConfigs(configs, pstate.NativeABCodeGPIA, false),
		SIMSignalEnabled:       pstate.NativeABBoolFromConfigs(configs, pstate.NativeABCodeSIMSignal, false),
		RecaptchaStage:         NativeRecaptchaStage(state, configs),
		RequestIDRandom:        pstate.NativeABBoolFromConfigs(configs, pstate.NativeABCodeRandomRequestID, false),
		KeyAttestationObserved: attestationObserved,
		KeyAttestationEnabled:  pstate.NativeABBoolFromConfigs(configs, 0x78e, true),
	}
}

func NativeShouldSendRegistrationGPIA(state pstate.NativeState) bool {
	return pstate.NativeABBoolFromConfigs(pstate.NativePreChatdABConfigs(state), pstate.NativeABCodeGPIA, false)
}

func ApplyNativePreChatdABDeviceFields(fields map[string]string, state pstate.NativeState) {
	if fields == nil {
		return
	}
	configs := pstate.NativePreChatdABConfigs(state)
	if pstate.NativeABBoolFromConfigs(configs, pstate.NativeABCodeSIMSignal, false) {
		fields["sim_type"] = NativeABSIMType(fields)
		fields["airplane_mode_type"] = "0"
		fields["cellular_strength"] = "5"
		fields["roaming_type"] = "0"
	}
	fields["recaptcha"] = NativeRecaptchaPayload(NativeRecaptchaStage(state, configs))
}

func NativeABSIMType(fields map[string]string) string {
	if fields["simnum"] == "1" || NativeOperatorPresent(fields["sim_mcc"], fields["sim_mnc"]) {
		return "1"
	}
	return "0"
}

func NativeOperatorPresent(mcc string, mnc string) bool {
	mcc = strings.TrimSpace(mcc)
	mnc = strings.TrimSpace(mnc)
	return mcc != "" && mcc != "000" && mnc != "" && mnc != "000"
}

func NativeRecaptchaStage(state pstate.NativeState, configs map[int]pstate.NativePreChatdABConfig) string {
	threshold, ok := NativeABIntFromConfigs(configs, pstate.NativeABCodeRecaptchaThreshold)
	if !ok || threshold <= 0 {
		return "ABPROP_DISABLED"
	}
	if NativeRecaptchaInstallRoll(state) < threshold {
		return "ABPROP_ENABLED"
	}
	return "ABPROP_DISABLED"
}

func NativeRecaptchaInstallRoll(state pstate.NativeState) int {
	seed := shared.FirstNonEmpty(state.Profile.ExpID, state.Profile.FDID, state.Profile.PhoneSHA256, state.AuthKey)
	sum := sha256.Sum256([]byte(seed))
	return 1 + int(binary.BigEndian.Uint16(sum[:2])%1000)
}

func NativeRecaptchaPayload(stage string) string {
	if strings.TrimSpace(stage) == "" {
		stage = "ABPROP_DISABLED"
	}
	data, err := json.Marshal(map[string]string{"stage": stage})
	if err != nil {
		return `{"stage":"ABPROP_DISABLED"}`
	}
	return string(data)
}
