package onboarding

import (
	"encoding/base64"
	"strconv"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

func ApplyAndroidCodeOptions(params *OrderedParams, options *protocolv1.AndroidRegistrationOptions) {
	if options == nil {
		return
	}
	setOptionalBool(params, "clicked_education_link", options.ClickedEducationLink)
	setOptionalBool(params, "manage_call_permission", options.ManageCallPermission)
	setOptionalBool(params, "call_log_permission", options.CallLogPermission)
	if options.ClientStartMessage != nil {
		params.Set("client_start_message", base64.RawURLEncoding.EncodeToString(options.GetClientStartMessage()), false)
	}
}

func ApplyAndroidADBState(params *OrderedParams, options *protocolv1.AndroidRegistrationOptions, current state.NativeState) {
	if options == nil || options.AdbEnabled == nil ||
		!state.NativeABBoolFromConfigs(state.NativePreChatdABConfigs(current), state.NativeABCodeADBState, false) {
		return
	}
	value := "0"
	if options.GetAdbEnabled() {
		value = "1"
	}
	AddRawParam(params, "db", value)
}

func setOptionalBool(params *OrderedParams, name string, value *bool) {
	if value != nil {
		params.Set(name, strconv.FormatBool(*value), false)
	}
}
