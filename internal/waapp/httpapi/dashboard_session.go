package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const (
	dashboardSessionCookieName = "wa_app_session"
	dashboardSessionDuration   = 7 * 24 * time.Hour
)

type dashboardSessionClaims struct {
	Version   string `json:"v"`
	ExpiresAt int64  `json:"exp"`
	Nonce     string `json:"n"`
}

func authenticatedDashboardSessionRequest(r *http.Request, auth dashboardAuthConfig) bool {
	cookie, err := r.Cookie(dashboardSessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	claims, ok := parseDashboardSessionToken(cookie.Value, auth)
	return ok && claims.Version == "1" && claims.ExpiresAt > time.Now().Unix()
}

func newDashboardSessionCookie(r *http.Request, auth dashboardAuthConfig) (*http.Cookie, error) {
	token, err := newDashboardSessionToken(auth)
	if err != nil {
		return nil, err
	}
	return &http.Cookie{
		Name:     dashboardSessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(dashboardSessionDuration),
		MaxAge:   int(dashboardSessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   dashboardCookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	}, nil
}

func expiredDashboardSessionCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     dashboardSessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   dashboardCookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	}
}

func newDashboardSessionToken(auth dashboardAuthConfig) (string, error) {
	nonce, err := newDashboardSessionNonce()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(dashboardSessionClaims{Version: "1", ExpiresAt: time.Now().Add(dashboardSessionDuration).Unix(), Nonce: nonce})
	if err != nil {
		return "", err
	}
	signature := signDashboardSessionPayload(payload, auth)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parseDashboardSessionToken(token string, auth dashboardAuthConfig) (dashboardSessionClaims, bool) {
	payloadPart, signaturePart, ok := strings.Cut(token, ".")
	if !ok || payloadPart == "" || signaturePart == "" {
		return dashboardSessionClaims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return dashboardSessionClaims{}, false
	}
	signature, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil || !hmac.Equal(signature, signDashboardSessionPayload(payload, auth)) {
		return dashboardSessionClaims{}, false
	}
	var claims dashboardSessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return dashboardSessionClaims{}, false
	}
	return claims, true
}

func signDashboardSessionPayload(payload []byte, auth dashboardAuthConfig) []byte {
	mac := hmac.New(sha256.New, dashboardSessionKey(auth))
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func dashboardSessionKey(auth dashboardAuthConfig) []byte {
	sum := sha256.Sum256([]byte("wa-app-dashboard-session\x00" + auth.password))
	return sum[:]
}

func newDashboardSessionNonce() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func dashboardCookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}
