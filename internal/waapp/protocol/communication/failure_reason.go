package communication

import (
	"context"
	"regexp"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func ChatdFailureReason(text string) string {
	switch {
	case strings.Contains(text, "connection reset by peer"):
		return "connection reset by peer"
	case strings.Contains(text, "eof"):
		return "EOF"
	case strings.Contains(text, "i/o timeout") || strings.Contains(text, "deadline") || strings.Contains(text, "timeout"):
		return "timeout"
	case strings.Contains(text, "server returned goa"):
		return "server returned GOA"
	case strings.Contains(text, "message authentication failed") || strings.Contains(text, "authentication failed") || strings.Contains(text, "cipher"):
		return "decrypt authentication failed"
	case strings.Contains(text, "invalid list-size token"):
		return ChatdNodeTokenFailureReason(text, "invalid list-size token")
	case strings.Contains(text, "readstring could not match token"):
		return ChatdNodeTokenFailureReason(text, "unsupported node string token")
	case strings.Contains(text, "unexpected end of binary node"):
		return "unexpected end of binary node"
	case strings.Contains(text, "truncated"):
		return "truncated binary node"
	case strings.Contains(text, "fragmented stanza"):
		return "fragmented stanza"
	case strings.Contains(text, "zlib") || strings.Contains(text, "flate"):
		return "compressed stanza decode failed"
	case strings.Contains(text, "server static public length"):
		return "invalid server static"
	case strings.Contains(text, "server ephemeral length"):
		return "invalid server ephemeral"
	case strings.Contains(text, "no such host"):
		return "DNS lookup failed"
	case strings.Contains(text, "socks5 connect failed"):
		return "SOCKS5 connect failed"
	case strings.Contains(text, "proxy rejected"):
		return "proxy rejected"
	case strings.Contains(text, "tls"):
		return "TLS handshake failed"
	default:
		return ""
	}
}

func ChatdNodeTokenFailureReason(text string, fallback string) string {
	match := ChatdNodeTokenErrorPattern.FindStringSubmatch(text)
	if len(match) < 3 {
		return fallback
	}
	return fallback + " " + match[2]
}

func (e *messagingService) DecryptMessage(ctx context.Context, input wacore.EngineDecryptInput) wacore.EngineDecryptResult {
	if strings.HasPrefix(input.PayloadRef, "plaintext:") {
		plain := strings.TrimPrefix(input.PayloadRef, "plaintext:")
		decryptedID := e.ids.NewID("wadec_")
		text := &waappv1.SensitiveText{RedactedValue: shared.Redacted(plain), SecretRef: "plaintext:" + decryptedID}
		if input.IncludePlaintextText {
			text.Value = plain
		}
		msg := &waappv1.DecryptedMessage{DecryptedMessageId: decryptedID, MessageId: input.MessageID, Status: waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED, PlaintextRef: "inline:" + decryptedID, PlaintextText: text, DecryptedAt: timestamppb.New(e.clock.Now())}
		return wacore.EngineDecryptResult{DecryptedMessage: msg, Candidates: ExtractCandidates(input.MessageID, decryptedID, plain, input.IncludePlaintextText, e.clock.Now(), e.ids)}
	}
	if strings.HasPrefix(input.PayloadRef, "native-enc:") {
		state, err := e.loadState(ctx, input.ClientProfileID)
		if err != nil {
			return wacore.EngineDecryptResult{Err: err}
		}
		payload, ok := state.MessagePayloads[input.PayloadRef]
		if !ok {
			return wacore.EngineDecryptResult{Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "encrypted message payload ref not found", false)}
		}
		plaintextRef := MessagePlaintextRef(input, payload)
		raw, err := e.decryptMessagePayload(ctx, input, &state, payload)
		if err != nil {
			return wacore.EngineDecryptResult{Err: err}
		}
		decryptedID := e.ids.NewID("wadec_")
		body, err := DecodeSignalMessage(raw)
		if err != nil {
			return wacore.EngineDecryptResult{Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_DECRYPTION_FAILED, "decrypted E2E message is malformed", false)}
		}
		plain, content, kind := ProjectMessage(body, input.IncludePlaintextText)
		text := &waappv1.SensitiveText{RedactedValue: shared.Redacted(plain), SecretRef: plaintextRef}
		if input.IncludePlaintextText {
			text.Value = plain
		}
		msg := &waappv1.DecryptedMessage{DecryptedMessageId: decryptedID, MessageId: input.MessageID, Status: waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED, PlaintextRef: plaintextRef, PlaintextText: text, DecryptedAt: timestamppb.New(e.clock.Now()), Content: content}
		contactHints := pwire.NativeContactHints(raw)
		contactHints = append(contactHints, NativeAppStateContactHints(&state, raw)...)
		contactHints = append(contactHints, ContactHintsFromNativePayloadMetadata(payload)...)
		return wacore.EngineDecryptResult{DecryptedMessage: msg, MessageKind: kind, Candidates: ExtractCandidates(input.MessageID, decryptedID, plain, input.IncludePlaintextText, e.clock.Now(), e.ids), ContactHints: pwire.DedupeWAContactHints(contactHints)}
	}
	return wacore.EngineDecryptResult{Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "payload ref scheme is not supported by native decryptor", false)}
}

func NativePlaintextText(raw []byte) string {
	message, err := DecodeSignalMessage(raw)
	if err != nil {
		return ""
	}
	text, _, _ := ProjectMessage(message, true)
	return text
}

func ExtractCandidates(messageID string, decryptedID string, text string, includeValue bool, now time.Time, ids shared.IDGenerator) []*waappv1.ExtractedCandidate {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	patterns := []struct {
		kind      waappv1.CandidateKind
		re        *regexp.Regexp
		normalize func(string) string
	}{
		{kind: waappv1.CandidateKind_CANDIDATE_KIND_FLAG, re: regexp.MustCompile(`(?i)(flag|ctf)\{[^\s}]{1,120}\}`)},
		{kind: waappv1.CandidateKind_CANDIDATE_KIND_OTP, re: regexp.MustCompile(`\b\d{4,8}\b`), normalize: shared.DigitsOnly},
		{kind: waappv1.CandidateKind_CANDIDATE_KIND_OTP, re: regexp.MustCompile(`\b\d{3}[-\s]\d{3}\b`), normalize: shared.DigitsOnly},
	}
	out := []*waappv1.ExtractedCandidate{}
	seen := map[string]struct{}{}
	for _, pattern := range patterns {
		for _, match := range pattern.re.FindAllString(text, -1) {
			value := match
			if pattern.normalize != nil {
				value = pattern.normalize(match)
			}
			if value == "" {
				continue
			}
			key := pattern.kind.String() + ":" + value
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidateID := ids.NewID("wacand_")
			sensitive := &waappv1.SensitiveText{RedactedValue: shared.Redacted(value), SecretRef: "candidate:" + candidateID}
			if includeValue {
				sensitive.Value = value
			}
			out = append(out, &waappv1.ExtractedCandidate{CandidateId: candidateID, MessageId: messageID, DecryptedMessageId: decryptedID, Kind: pattern.kind, Text: sensitive, Confidence: 0.9, ExtractedAt: timestamppb.New(now)})
		}
	}
	return out
}
