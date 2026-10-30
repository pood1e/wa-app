package state

import (
	"strings"
	"time"
)

type NativePreChatdABState struct {
	Hash                 string `json:"hash,omitempty"`
	Key                  string `json:"key,omitempty"`
	ExpConfig            string `json:"exp_config,omitempty"`
	NextFetchAtUnixMilli int64  `json:"next_fetch_at_unix_ms,omitempty"`
}

const NativeABPropSuccessRefreshDelay = time.Minute

func (s NativePreChatdABState) Due(now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	return s.NextFetchAtUnixMilli <= 0 || now.UTC().UnixMilli() >= s.NextFetchAtUnixMilli
}

func (s *NativePreChatdABState) ScheduleRetry(now time.Time, delay time.Duration) {
	if s == nil || delay <= 0 {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.NextFetchAtUnixMilli = now.UTC().Add(delay).UnixMilli()
}

func (s *NativePreChatdABState) ApplyResponse(data map[string]any, now time.Time) {
	if s == nil || data == nil {
		return
	}
	if hash := strings.TrimSpace(JsonString(data["ab_hash"])); hash != "" {
		s.Hash = hash
	}
	if key := strings.TrimSpace(JsonString(data["ab_key"])); key != "" {
		s.Key = key
	}
	if expConfig := strings.TrimSpace(JsonString(data["exp_cfg"])); expConfig != "" && expConfig != "wamsys initialization fails" {
		if merged, ok := mergeNativeABConfigs(s.ExpConfig, expConfig); ok {
			s.ExpConfig = merged
		}
	}
	if seconds := NativeABPropRetryAfterSeconds(data); seconds > 0 {
		s.ScheduleRetry(now, time.Duration(seconds)*time.Second)
		return
	}
	if ResponseStatus(data) == "ok" {
		s.ScheduleRetry(now, NativeABPropSuccessRefreshDelay)
	}
}

func NativeABPropRetryAfterSeconds(data map[string]any) int64 {
	if data == nil {
		return 0
	}
	return JsonInt64(data["retry_after"])
}
