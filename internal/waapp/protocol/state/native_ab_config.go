package state

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

const (
	NativeABCodeGPIA               = 3753
	NativeABCodeSIMSignal          = 4435
	NativeABCodeADBState           = 7999
	NativeABCodeRecaptchaThreshold = 7343
	NativeABCodeRandomRequestID    = 9463
)

type NativePreChatdABConfig struct {
	Value string
}

// mergeNativeABConfigs applies the server's changed entries to the cached values.
func mergeNativeABConfigs(current, update string) (string, bool) {
	changes, ok := ParseNativePreChatdABConfigArray(update)
	if !ok {
		var nested string
		if json.Unmarshal([]byte(update), &nested) != nil {
			return current, false
		}
		changes, ok = ParseNativePreChatdABConfigArray(nested)
		if !ok {
			return current, false
		}
	}
	if len(changes) == 0 {
		return current, true
	}
	configs := ParseNativePreChatdABConfigs(current)
	if configs == nil {
		configs = make(map[int]NativePreChatdABConfig, len(changes))
	}
	for code, value := range changes {
		configs[code] = value
	}
	keys := make([]int, 0, len(configs))
	for code := range configs {
		keys = append(keys, code)
	}
	slices.Sort(keys)
	type item struct {
		Code  int    `json:"config_code"`
		Value string `json:"config_value"`
	}
	items := make([]item, 0, len(keys))
	for _, code := range keys {
		items = append(items, item{Code: code, Value: configs[code].Value})
	}
	data, err := json.Marshal(items)
	return string(data), err == nil
}

func NativePreChatdABConfigs(state NativeState) map[int]NativePreChatdABConfig {
	return ParseNativePreChatdABConfigs(state.PreChatdAB.ExpConfig)
}

func ParseNativePreChatdABConfigs(expConfig string) map[int]NativePreChatdABConfig {
	expConfig = strings.TrimSpace(expConfig)
	if expConfig == "" || expConfig == "wamsys initialization fails" {
		return nil
	}
	if configs, ok := ParseNativePreChatdABConfigArray(expConfig); ok {
		return configs
	}
	var nested string
	if err := json.Unmarshal([]byte(expConfig), &nested); err == nil {
		if configs, ok := ParseNativePreChatdABConfigArray(nested); ok {
			return configs
		}
	}
	return nil
}

func ParseNativePreChatdABConfigArray(expConfig string) (map[int]NativePreChatdABConfig, bool) {
	var items []map[string]any
	if err := json.Unmarshal([]byte(expConfig), &items); err != nil {
		return nil, false
	}
	configs := make(map[int]NativePreChatdABConfig, len(items))
	for _, item := range items {
		code, err := strconv.Atoi(strings.TrimSpace(NativeJSONScalarString(item["config_code"])))
		if err != nil {
			continue
		}
		configs[code] = NativePreChatdABConfig{Value: strings.TrimSpace(NativeJSONScalarString(item["config_value"]))}
	}
	return configs, true
}

func NativeJSONScalarString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "1"
		}
		return "0"
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func NativeABBoolFromConfigs(configs map[int]NativePreChatdABConfig, code int, fallback bool) bool {
	config, ok := configs[code]
	if !ok {
		return fallback
	}
	value := strings.ToLower(strings.TrimSpace(config.Value))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "", "0", "false", "no", "off":
		return false
	default:
		n, err := strconv.ParseFloat(value, 64)
		return err == nil && n != 0
	}
}

func NativeShouldRandomizeRegistrationRequestID(state NativeState) bool {
	return NativeABBoolFromConfigs(NativePreChatdABConfigs(state), NativeABCodeRandomRequestID, false)
}
