package ios

import (
	"context"
	"errors"
	"strings"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"google.golang.org/protobuf/proto"
)

var ErrPasswordCooldown = errors.New("two-step verification is cooling down")

func (f *Flow) PasswordChallenge() (bool, time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.available(registrationAwaitingPassword) || f.available(registrationPasswordUncertain), f.passwordRetryAt
}

// SubmitPassword uses the first-factor keys and an already sealed canonical password.
func (f *Flow) SubmitPassword(ctx context.Context, sealed string) (*protocolv1.IOSRegistrationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available(registrationAwaitingPassword) && !f.available(registrationPasswordUncertain) {
		return nil, ErrRegistrationState
	}
	if time.Now().Before(f.passwordRetryAt) {
		return nil, ErrPasswordCooldown
	}
	if !strings.HasPrefix(sealed, "#PWD_WA:11:") {
		return nil, errors.New("registration password envelope is invalid")
	}
	request, err := f.request(ctx, "password", "password", sealed)
	if err != nil {
		return nil, err
	}
	f.stage = registrationPasswordUncertain
	result, err := f.http.send(request)
	if err != nil {
		return result, err
	}
	if result.GetStatus() == "ok" {
		if result.GetLogin() == "" {
			return nil, errors.New("iOS registration response identity is missing")
		}
		f.stage = registrationComplete
		f.registered = proto.CloneOf(result)
	} else if result.GetStatus() == "fail" {
		f.awaitPassword(result)
		if result.GetReason() == "missing" || result.GetReason() == "stale" {
			f.stage = registrationProbed
		}
	}
	return result, nil
}

func (f *Flow) awaitPassword(response *protocolv1.IOSRegistrationResponse) {
	f.stage = registrationAwaitingPassword
	if wait := max(response.GetGuessWait(), response.GetRetryAfter()); wait > 0 {
		f.passwordRetryAt = time.Now().Add(verificationDelay(wait))
	}
}
