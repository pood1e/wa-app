package state

import (
	"encoding/json"
	"strconv"
	"strings"
)

func IsRuntimeNativeDeviceMapKey(key string) bool {
	switch key {
	case "pid", "feo2_query_status":
		return true
	default:
		return false
	}
}

const NativeDefaultFeo2QueryStatus = "did_not_query"

func (s *NativeState) NextGenerateCodeAttempt() int {
	previous := s.GenerateCodeAttempts
	if previous < 1 {
		previous = NativeCodeRequestAttemptsFromLastParams(s.LastCodeParams)
	}
	if previous < 0 {
		previous = 0
	}
	s.GenerateCodeAttempts = previous + 1
	return s.GenerateCodeAttempts
}

func NativeCodeRequestAttemptsFromLastParams(params map[string]string) int {
	metrics := strings.TrimSpace(params["client_metrics"])
	if metrics == "" {
		return 0
	}
	var payload struct {
		Attempts int `json:"attempts"`
	}
	if err := json.Unmarshal([]byte(metrics), &payload); err != nil {
		return 0
	}
	return payload.Attempts
}

func StringList(value any) []string {
	switch v := value.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return v
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			out = append(out, strings.TrimSpace(part))
		}
		return out
	default:
		return nil
	}
}

func JsonInt64(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}
