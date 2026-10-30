package ios

import (
	"context"
	"crypto/ecdh"
	"errors"
	"net/http"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/attestation"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

type registrationStage uint8

const (
	registrationPrepared registrationStage = iota
	registrationProbed
	registrationCodeUncertain
	registrationAwaitingCode
	registrationVerifyUncertain
	registrationAwaitingPassword
	registrationPasswordUncertain
	registrationComplete
	registrationClosed
)

// ErrRegistrationState rejects operations outside their registration stage.
var ErrRegistrationState = errors.New("iOS registration operation is not available in this state")

// ErrCodeCooldown means the server has not made the selected channel available.
var ErrCodeCooldown = errors.New("registration channel is unavailable or cooling down")

// Flow owns one in-memory iOS registration and its independent device proof context.
// Calls are serialized; transport failures preserve uncertain outcomes.
type Flow struct {
	mu               sync.Mutex
	stage            registrationStage
	expires          time.Time
	keys             *protocolv1.RegistrationKeyMaterial
	phone            *waappv1.PhoneTarget
	client           *protocolv1.ClientMetadata
	registered       *protocolv1.IOSRegistrationResponse
	params           registrationParameters
	http             *registrationHTTP
	attestor         *Attestor
	authorization    string
	channels         map[waappv1.VerificationDeliveryMethod]verificationChannel
	method           waappv1.VerificationDeliveryMethod
	codeLength       int
	passwordRetryAt  time.Time
	recaptcha        waappv1.RecaptchaServiceClient
	recaptchaSiteKey string
}

// Dependencies contains independent device SDK providers.
type Dependencies struct {
	Attestation waappv1.AppAttestServiceClient
	Recaptcha   waappv1.RecaptchaServiceClient
}

// Begin generates host-owned keys and creates an independent device proof context.
// The caller owns both network clients; Close releases this flow's proof context.
func Begin(ctx context.Context, phone *waappv1.PhoneTarget, profile *protocolv1.IOSRegistrationProfile, client *http.Client, dependencies Dependencies) (*Flow, error) {
	if err := validateRegistration(phone, profile); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keys, err := wire.NewRegistrationKeys()
	if err != nil {
		return nil, err
	}
	params, err := newRegistrationParameters(phone, profile, keys, time.Now())
	if err != nil {
		return nil, err
	}
	var attestor *Attestor
	if dependencies.Attestation != nil {
		attestor, err = CreateAttestor(ctx, dependencies.Attestation)
		if err != nil && !attestation.Unavailable(ctx, err) {
			return nil, err
		}
	}
	metadata := proto.Clone(profile.GetClient()).(*protocolv1.ClientMetadata)
	metadata.PhoneId, metadata.DeviceExperimentId = params.phoneID, params.experimentID
	return &Flow{expires: time.Now().Add(30 * time.Minute), keys: keys, params: params,
		phone: proto.Clone(phone).(*waappv1.PhoneTarget), client: metadata,
		http: newRegistrationHTTP(client, profile.GetUserAgent()), attestor: attestor,
		recaptcha: dependencies.Recaptcha, recaptchaSiteKey: profile.GetRecaptchaSiteKey()}, nil
}

// Probe checks the number using this flow's keys and public-key proof.
func (f *Flow) Probe(ctx context.Context) (*protocolv1.IOSRegistrationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stage == registrationClosed || f.stage == registrationComplete || time.Now().After(f.expires) {
		return nil, ErrRegistrationState
	}
	if f.authorization == "" && f.attestor != nil {
		noise, err := ecdh.X25519().NewPrivateKey(f.keys.GetNoisePrivateKey())
		if err != nil {
			return nil, err
		}
		f.authorization, err = f.attestor.AttestKey(ctx, noise.PublicKey().Bytes())
		if err != nil {
			return nil, err
		}
	}
	request, err := f.request(ctx, "exist", "", "")
	if err != nil {
		return nil, err
	}
	result, err := f.http.send(request)
	if err == nil && result.GetStatus() == "fail" && result.GetReason() == "incorrect" &&
		(f.stage == registrationPrepared || f.stage == registrationProbed) {
		f.stage = registrationProbed
		f.updateChannels(result, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED)
	}
	return result, err
}

// RequestCode uses an available channel and continues native SDK challenges.
func (f *Flow) RequestCode(ctx context.Context, method waappv1.VerificationDeliveryMethod) (*protocolv1.IOSRegistrationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available(registrationProbed) {
		return nil, ErrRegistrationState
	}
	name, err := verificationMethodName(method)
	if err != nil {
		return nil, err
	}
	channel := f.channels[method]
	if !channel.available || time.Now().Before(channel.nextCodeAt) {
		return nil, ErrCodeCooldown
	}
	request, err := f.request(ctx, "code", name, "")
	if err != nil {
		return nil, err
	}
	result, err := f.sendCodeRequest(ctx, request, name)
	if err == nil && result.GetStatus() == "sent" && result.GetMethod() == name {
		f.stage = registrationAwaitingCode
		f.method, f.codeLength = method, VerificationCodeLength(result, method)
		f.updateChannels(result, method)
	} else if result.GetStatus() == "fail" && f.stage != registrationCodeUncertain {
		f.stage = registrationProbed
		f.updateChannels(result, method)
	}
	return result, err
}

// SubmitCode verifies one fresh code through the channel used to request it.
// An explicit submission can resolve an uncertain verification using the same keys.
func (f *Flow) SubmitCode(ctx context.Context, method waappv1.VerificationDeliveryMethod, code string) (*protocolv1.IOSRegistrationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available(registrationAwaitingCode) && !f.available(registrationVerifyUncertain) {
		return nil, ErrRegistrationState
	}
	if method != f.method {
		return nil, ErrRegistrationState
	}
	if !decimal(code, f.codeLength, f.codeLength) {
		return nil, errors.New("verification code has invalid length or characters")
	}
	name := ""
	if method != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS {
		name, _ = verificationMethodName(method)
	}
	request, err := f.request(ctx, "register", name, code)
	if err != nil {
		return nil, err
	}
	f.stage = registrationVerifyUncertain
	result, err := f.http.send(request)
	if err == nil && result.GetStatus() == "ok" {
		if result.GetLogin() == "" {
			return nil, errors.New("iOS registration response identity is missing")
		}
		f.stage = registrationComplete
		f.registered = proto.Clone(result).(*protocolv1.IOSRegistrationResponse)
	} else if err == nil && result.GetStatus() == "fail" {
		switch result.GetReason() {
		case "security_code":
			f.awaitPassword(result)
		case "missing", "stale":
			f.stage = registrationProbed
		default:
			f.stage = registrationAwaitingCode
		}
	}
	return result, err
}

// KeyMaterial returns a copy for account handoff after registration succeeds.
func (f *Flow) KeyMaterial() (*protocolv1.RegistrationKeyMaterial, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available(registrationComplete) {
		return nil, ErrRegistrationState
	}
	return proto.Clone(f.keys).(*protocolv1.RegistrationKeyMaterial), nil
}

// Close clears flow material and releases the device proof context.
func (f *Flow) Close(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stage = registrationClosed
	if f.keys != nil {
		clear(f.keys.NoisePrivateKey)
		clear(f.keys.IdentityPrivateKey)
		clear(f.keys.SignedPreKeyPrivateKey)
		clear(f.keys.SignedPreKeySignature)
		f.keys = nil
	}
	f.params = registrationParameters{}
	f.phone, f.client, f.registered = nil, nil, nil
	f.channels = nil
	f.method, f.codeLength = 0, 0
	f.passwordRetryAt = time.Time{}
	f.authorization = ""
	f.recaptcha, f.recaptchaSiteKey = nil, ""
	if f.attestor == nil {
		return nil
	}
	if err := f.attestor.Close(ctx); err != nil {
		return err
	}
	f.attestor = nil
	return nil
}

func (f *Flow) available(stage registrationStage) bool {
	return f.stage == stage && time.Now().Before(f.expires)
}

// OutcomeUnknown reports whether a mutating HTTP request lacks a definite result.
func (f *Flow) OutcomeUnknown() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stage == registrationCodeUncertain || f.stage == registrationVerifyUncertain || f.stage == registrationPasswordUncertain
}

func (f *Flow) request(ctx context.Context, path, method, code string) (*http.Request, error) {
	now := time.Now().Add(f.http.clockOffset)
	endpoint := path
	if path == "password" {
		endpoint = "register"
	}
	return f.http.prepare(ctx, endpoint, f.params.request(path, method, code, now), f.attestor, f.authorization)
}
