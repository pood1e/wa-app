package ios

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

// ErrRecaptchaUnavailable means the server requires an independent SDK proof provider.
var ErrRecaptchaUnavailable = errors.New("iOS registration requires a reCAPTCHA SDK provider")

func (f *Flow) sendCodeRequest(ctx context.Context, request *http.Request, method string) (*protocolv1.IOSRegistrationResponse, error) {
	for attempt := 0; ; attempt++ {
		f.stage = registrationCodeUncertain
		result, err := f.http.send(request)
		if err != nil {
			return nil, err
		}
		if result.GetStatus() == "fail" {
			f.stage = registrationProbed
		}
		if result.GetStatus() != "fail" || result.GetReason() != "challenge" || result.GetNextMethod() != "recaptcha" || result.GetRecaptchaWait() > 0 || attempt >= 2 {
			return result, nil
		}
		request, err = f.recaptchaCodeRequest(ctx, method)
		if err != nil {
			return result, err
		}
	}
}

func (f *Flow) recaptchaCodeRequest(ctx context.Context, method string) (*http.Request, error) {
	if f.recaptcha == nil || f.recaptchaSiteKey == "" {
		return nil, ErrRecaptchaUnavailable
	}
	operation, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	proof, err := f.recaptcha.GenerateLoginToken(operation, &waappv1.RecaptchaLoginTokenRequest{SiteKey: f.recaptchaSiteKey})
	if err != nil {
		return nil, fmt.Errorf("generate registration reCAPTCHA token: %w", err)
	}
	token := proof.GetToken()
	if len(token) == 0 || len(token) > 16*1024 {
		return nil, errors.New("iOS registration reCAPTCHA token has invalid length")
	}
	payload, err := (protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}).Marshal(&protocolv1.IOSRecaptchaPayload{Token: token, TokenLength: int32(len(token))})
	if err != nil {
		return nil, errors.New("encode iOS registration reCAPTCHA payload")
	}
	query := f.params.request("code", method, "", time.Now().Add(f.http.clockOffset))
	query = append(query, parameter{"recaptcha", strings.ReplaceAll(url.QueryEscape(string(payload)), "+", "%20")})
	return f.http.prepare(ctx, "code", query, f.attestor, f.authorization)
}
