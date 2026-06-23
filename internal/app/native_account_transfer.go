package app

import (
	"fmt"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	accountTransferMaxCodes            = 6
	accountTransferRotationIntervalSec = 60
	accountTransferDeeplinkBase        = "whatsapp-consumer://fpm"
	accountTransferDeeplinkPort        = "8988"
	accountTransferDeeplinkVersion     = "3"
	accountTransferDeeplinkPlatform    = "android"
	accountTransferAuthMethod          = "cert"
	accountTransferEncKeyVersion       = "1"
)

func newNativeAccountTransferState(phone *waappv1.PhoneTarget, codes []string, now time.Time) nativeAccountTransferState {
	codes = normalizeAccountTransferCodes(codes)
	ttlSeconds := int64(len(codes) * accountTransferRotationIntervalSec)
	return nativeAccountTransferState{
		Codes:                  codes,
		CurrentIndex:           1,
		RequestedAtUnix:        now.UTC().Unix(),
		ExpiresAtUnix:          now.UTC().Unix() + ttlSeconds,
		RotationIntervalSec:    accountTransferRotationIntervalSec,
		SessionID:              b64u(randomBytes(32)),
		Certificate:            b64u(randomBytes(64)),
		AuthToken:              b64u(randomBytes(32)),
		PeerID:                 b64u(randomBytes(16)),
		EncryptionKeyVersion:   accountTransferEncKeyVersion,
		EncryptionAccountHash:  b64u(randomBytes(32)),
		EncryptionKeySalt:      b64u(randomBytes(32)),
		DeeplinkBase:           accountTransferDeeplinkBase,
		AccountPhoneNumber:     fullPhoneKey(phoneCC(phone), phoneNational(phone)),
		LastChallengeIssuedSec: now.UTC().Unix(),
	}
}

func normalizeAccountTransferCodes(codes []string) []string {
	out := make([]string, 0, min(len(codes), accountTransferMaxCodes))
	seen := map[string]struct{}{}
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
		if len(out) >= accountTransferMaxCodes {
			break
		}
	}
	return out
}

func accountTransferCodesFromResponse(data map[string]any) []string {
	return normalizeAccountTransferCodes(stringList(data["code_list"]))
}

func (s nativeAccountTransferState) empty() bool {
	return len(s.Codes) == 0
}

func (s nativeAccountTransferState) interval() time.Duration {
	if s.RotationIntervalSec <= 0 {
		return accountTransferRotationIntervalSec * time.Second
	}
	return time.Duration(s.RotationIntervalSec) * time.Second
}

func (s nativeAccountTransferState) expiresAt() time.Time {
	if s.ExpiresAtUnix > 0 {
		return time.Unix(s.ExpiresAtUnix, 0).UTC()
	}
	if s.RequestedAtUnix <= 0 || len(s.Codes) == 0 {
		return time.Time{}
	}
	return time.Unix(s.RequestedAtUnix+int64(len(s.Codes))*int64(s.interval()/time.Second), 0).UTC()
}

func (s *nativeAccountTransferState) currentCode(now time.Time) (string, int, error) {
	if s == nil || len(s.Codes) == 0 {
		return "", 0, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_EXPIRED, "account transfer challenge is not available", false)
	}
	requestedAt := s.RequestedAtUnix
	if requestedAt <= 0 {
		requestedAt = now.UTC().Unix()
		s.RequestedAtUnix = requestedAt
	}
	intervalSeconds := int64(s.interval() / time.Second)
	if intervalSeconds <= 0 {
		intervalSeconds = accountTransferRotationIntervalSec
	}
	index := int((now.UTC().Unix()-requestedAt)/intervalSeconds) + 1
	if index < 1 {
		index = 1
	}
	if index > len(s.Codes) {
		return "", index, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_EXPIRED, "account transfer challenge expired", false)
	}
	s.CurrentIndex = index
	s.LastChallengeIssuedSec = now.UTC().Unix()
	return s.Codes[index-1], index, nil
}

func (s *nativeAccountTransferState) challenge(verificationRequestID string, now time.Time) (*waappv1.AccountTransferChallenge, error) {
	code, index, err := s.currentCode(now)
	if err != nil {
		return nil, err
	}
	payload := s.deeplink(code)
	return &waappv1.AccountTransferChallenge{
		VerificationRequestId: verificationRequestID,
		CodeCount:             int32(len(s.Codes)),
		CurrentCodeIndex:      int32(index),
		CurrentCodeLength:     int32(len(code)),
		RotationInterval:      durationpb.New(s.interval()),
		RequestedAt:           timestamppb.New(time.Unix(s.RequestedAtUnix, 0).UTC()),
		ExpiresAt:             timestamppb.New(s.expiresAt()),
		QrDeeplink: &waappv1.SensitiveText{
			Value:         payload,
			RedactedValue: accountTransferDeeplinkBase + "?<redacted>",
		},
	}, nil
}

func (s nativeAccountTransferState) deeplink(code string) string {
	base := firstNonEmpty(s.DeeplinkBase, accountTransferDeeplinkBase)
	values := []struct {
		key   string
		value string
	}{
		{"version", accountTransferDeeplinkVersion},
		{"platform", accountTransferDeeplinkPlatform},
		{"sessionID", s.SessionID},
		{"authMethod", accountTransferAuthMethod},
		{"cert", s.Certificate},
		{"authToken", s.AuthToken},
		{"peerID", s.PeerID},
		{"ip", ""},
		{"ssid", ""},
		{"ssidPw", ""},
		{"otpCode", code},
		{"port", accountTransferDeeplinkPort},
		{"encKeyVer", firstNonEmpty(s.EncryptionKeyVersion, accountTransferEncKeyVersion)},
		{"encKeyAccHash", s.EncryptionAccountHash},
		{"encKeySalt", s.EncryptionKeySalt},
		{"phoneNumber", s.AccountPhoneNumber},
	}
	parts := make([]string, 0, len(values))
	for _, item := range values {
		parts = append(parts, item.key+"="+item.value)
	}
	return fmt.Sprintf("%s?%s", strings.TrimRight(base, "?"), strings.Join(parts, "&"))
}
