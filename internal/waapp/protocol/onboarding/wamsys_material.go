package onboarding

import (
	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

func ApplyOrderedWamsysKey(params *OrderedParams, capture *protocolv1.WamsysCapture, key string) {
	if params == nil || capture == nil || !IsOpaqueWamsysMapKey(key) {
		return
	}
	for _, item := range capture.GetMapParams() {
		if item.GetKey() == key {
			params.Set(key, pstate.PctBytes(item.GetValue()), true)
			return
		}
	}
}

func ApplyOrderedWamsysExcept(params *OrderedParams, capture *protocolv1.WamsysCapture, excluded map[string]struct{}) {
	if params == nil || capture == nil {
		return
	}
	for _, item := range capture.GetMapParams() {
		key := item.GetKey()
		if !IsOpaqueWamsysMapKey(key) {
			continue
		}
		if _, skip := excluded[key]; skip {
			continue
		}
		params.Set(key, pstate.PctBytes(item.GetValue()), true)
	}
}

// Opaque WAMSYS values stay behind a dedicated material provider so registration
// maps do not leak opaque blobs into generic phone profile fields.
var OpaqueWamsysMapKeys = map[string]struct{}{
	"gpia": {},
	"_ge":  {},
	"_gi":  {},
	"_gg":  {},
	"_gp":  {},
	"_ga":  {},
	"aid":  {},
	"_gs":  {},
}

func IsOpaqueWamsysMapKey(key string) bool {
	_, ok := OpaqueWamsysMapKeys[key]
	return ok
}
