package state

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/waproto"
)

// existProbeTransientAttempts 限定 /v2/exist 探测在纯传输层失败(代理/网络抖动,响应
// 无任何可解析的应用层结论)时的最大尝试次数。exist 是幂等检查,重试安全。
const ExistProbeTransientAttempts = 3

func ChatRoutingInfoFromValue(value any) string {
	switch typed := value.(type) {
	case []byte:
		if len(typed) == 0 || len(typed) > 256 {
			return ""
		}
		return pwire.B64u(typed)
	case string:
		return pwire.NormalizeChatRoutingInfo(typed)
	default:
		return ""
	}
}

func JsonString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func UpstreamFailureMessage(data map[string]any, err error) string {
	if statusCode := JsonNumber(data["status_code"]); statusCode > 0 {
		if snippet := SafeResponseSnippet(JsonString(data["response_text"])); snippet != "" {
			return fmt.Sprintf("wasafe upstream http %d: %s", statusCode, snippet)
		}
		return fmt.Sprintf("wasafe upstream http %d", statusCode)
	}
	return err.Error()
}

func SafeResponseSnippet(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return ""
	}
	text = waproto.SensitiveDigitsPattern.ReplaceAllString(text, "<digits>")
	for _, marker := range []string{"token", "auth", "key", "code", "sig"} {
		if strings.Contains(strings.ToLower(text), marker) {
			return "<redacted>"
		}
	}
	if utf8.RuneCountInString(text) <= 160 {
		return text
	}
	out := make([]rune, 0, 160)
	for _, ch := range text {
		if len(out) >= 160 {
			break
		}
		out = append(out, ch)
	}
	return string(out) + "..."
}

func JsonNumber(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}
