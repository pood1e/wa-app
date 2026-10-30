package httpapi

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func textField(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func objectField(data map[string]any, key string) map[string]any {
	if data == nil {
		return map[string]any{}
	}
	if value, ok := data[key].(map[string]any); ok {
		return value
	}
	return map[string]any{}
}

func newRequestID(prefix string) string {
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%d-%s", prefix, time.Now().UnixNano(), hex.EncodeToString(random[:]))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeProtoJSON(w http.ResponseWriter, status int, value proto.Message) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(value)
	if err != nil {
		_, _ = w.Write([]byte("{}"))
		return
	}
	_, _ = w.Write(data)
}

func readProtoJSONPayload(w http.ResponseWriter, r *http.Request, maxBytes int64, value proto.Message, message string) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, value); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": message})
		return false
	}
	return true
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func queryBool(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return fallback
	}
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
