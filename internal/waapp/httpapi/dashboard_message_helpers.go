package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func readJSONPayload(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return nil, false
	}
	payload := map[string]any{}
	if len(body) == 0 {
		return payload, true
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be json"})
		return nil, false
	}
	return payload, true
}

func stringListField(payload map[string]any, key string) []string {
	value, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" {
				continue
			}
			out = append(out, strings.TrimSpace(text))
		}
		return out
	case []string:
		return typed
	case string:
		return []string{typed}
	default:
		return nil
	}
}

func boolField(payload map[string]any, key string) bool {
	switch value := payload[key].(type) {
	case bool:
		return value
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func deleteModeField(payload map[string]any) waappv1.AccountMessageDeleteMode {
	switch strings.ToLower(strings.TrimSpace(textField(payload, "mode"))) {
	case "for_everyone", "everyone", "revoke":
		return waappv1.AccountMessageDeleteMode_ACCOUNT_MESSAGE_DELETE_MODE_FOR_EVERYONE
	default:
		return waappv1.AccountMessageDeleteMode_ACCOUNT_MESSAGE_DELETE_MODE_FOR_ME
	}
}
