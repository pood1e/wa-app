package communication

import (
	"bytes"
	"strconv"
	"strings"
	"time"

	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

const TrustedContactTokenMaxAge = 182 * 24 * time.Hour

type NativePrivacyTokenUpdate struct {
	JID       string
	Token     []byte
	Timestamp int64
}

func PrivacyTokenUpdatesFromChatdNode(node pwire.ChatdNode) []NativePrivacyTokenUpdate {
	updates := []NativePrivacyTokenUpdate{}
	var walk func(pwire.ChatdNode)
	walk = func(current pwire.ChatdNode) {
		updates = append(updates, PrivacyTokenUpdatesFromNotificationNode(current)...)
		for _, child := range pwire.ChatdChildren(current) {
			walk(child)
		}
	}
	walk(node)
	return DedupePrivacyTokenUpdates(updates)
}

func PrivacyTokenUpdatesFromNotificationNode(node pwire.ChatdNode) []NativePrivacyTokenUpdate {
	tokens, ok := ChatdChild(node, "tokens")
	if !ok {
		return nil
	}
	jid := TrustedContactTokenJID(node)
	if jid == "" {
		return nil
	}
	parentTimestamp := PrivacyTokenTimestamp(node.Attrs["t"])
	updates := []NativePrivacyTokenUpdate{}
	for _, tokenNode := range pwire.ChatdChildren(tokens) {
		if tokenNode.Tag != "token" || tokenNode.Attrs["type"] != "trusted_contact" {
			continue
		}
		token := PrivacyTokenContent(tokenNode.Content)
		if len(token) == 0 {
			continue
		}
		timestamp := PrivacyTokenTimestamp(tokenNode.Attrs["t"])
		if timestamp == 0 {
			timestamp = parentTimestamp
		}
		updates = append(updates, NativePrivacyTokenUpdate{JID: jid, Token: token, Timestamp: timestamp})
	}
	return updates
}

func TrustedContactTokenJID(node pwire.ChatdNode) string {
	for _, value := range []string{node.Attrs["sender_lid"], node.Attrs["from"]} {
		jid := wacore.NormalizeWAJID(value)
		if strings.HasSuffix(jid, "@lid") || strings.HasSuffix(jid, "@s.whatsapp.net") {
			return jid
		}
	}
	return ""
}

func PrivacyTokenContent(content any) []byte {
	switch value := content.(type) {
	case []byte:
		return bytes.Clone(value)
	case string:
		return []byte(strings.TrimSpace(value))
	default:
		return nil
	}
}

func PrivacyTokenTimestamp(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	timestamp, err := strconv.ParseInt(value, 10, 64)
	if err != nil || timestamp <= 0 {
		return 0
	}
	if timestamp > 1000000000000 {
		return timestamp / 1000
	}
	return timestamp
}

func DedupePrivacyTokenUpdates(updates []NativePrivacyTokenUpdate) []NativePrivacyTokenUpdate {
	if len(updates) == 0 {
		return nil
	}
	merged := map[string]NativePrivacyTokenUpdate{}
	order := []string{}
	for _, update := range updates {
		if update.JID == "" || len(update.Token) == 0 {
			continue
		}
		key := update.JID
		current, ok := merged[key]
		if !ok {
			merged[key] = update
			order = append(order, key)
			continue
		}
		if update.Timestamp >= current.Timestamp {
			merged[key] = update
		}
	}
	out := make([]NativePrivacyTokenUpdate, 0, len(order))
	for _, key := range order {
		out = append(out, merged[key])
	}
	return out
}

func ApplyPrivacyTokenUpdates(state *pstate.NativeState, updates []NativePrivacyTokenUpdate) bool {
	if state == nil || len(updates) == 0 {
		return false
	}
	state.EnsureMaps()
	changed := false
	for _, update := range updates {
		if update.JID == "" || len(update.Token) == 0 {
			continue
		}
		next := pstate.NativePrivacyToken{Token: pwire.B64u(update.Token), Timestamp: update.Timestamp}
		current, ok := state.PrivacyTokens[update.JID]
		if ok && current.Timestamp > next.Timestamp {
			continue
		}
		if ok && current.Timestamp == next.Timestamp && current.Token == next.Token {
			continue
		}
		state.PrivacyTokens[update.JID] = next
		changed = true
	}
	return changed
}

func TrustedContactTokenForProfilePicture(state pstate.NativeState, jid string, now time.Time) []byte {
	return PrivacyTokenForJID(state, jid, now)
}

func PrivacyTokenForJID(state pstate.NativeState, jid string, now time.Time) []byte {
	jid = wacore.NormalizeWAJID(jid)
	if jid == "" || len(state.PrivacyTokens) == 0 {
		return nil
	}
	record, ok := state.PrivacyTokens[jid]
	if !ok || !PrivacyTokenTimestampActive(record.Timestamp, now) {
		return nil
	}
	token, err := pwire.DecodeB64Any(record.Token)
	if err != nil || len(token) == 0 {
		return nil
	}
	return token
}

func PrivacyTokenTimestampActive(timestamp int64, now time.Time) bool {
	if timestamp == 0 {
		return true
	}
	current := now.Unix()
	if current <= 0 {
		return true
	}
	return timestamp >= current-int64(TrustedContactTokenMaxAge/time.Second)
}
