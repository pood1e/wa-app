package onboarding

import (
	"context"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/attestation"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding/android"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type engineCore struct {
	activeProxyURL     string
	http               *wire.NativeHTTPClient
	clock              shared.Clock
	ids                shared.IDGenerator
	current            state.NativeState
	build              *protocolv1.AndroidBuildMaterial
	parameters         *protocolv1.AndroidClientParameters
	token              string
	attestor           *android.Attestor
	verification       *protocolv1.AndroidVerificationContext
	verificationMethod waappv1.VerificationDeliveryMethod
	passwordRequired   bool
	passwordRetryAt    time.Time
}

// AndroidFlow owns the material of one registration or transfer operation.
// Its caller serializes operations and closes it when the bounded flow ends.
type AndroidFlow struct {
	*engineCore
	*registrationService
	phone *waappv1.PhoneTarget
}

type AndroidDependencies struct {
	Attestation waappv1.AndroidAttestationServiceClient
	Clock       shared.Clock
	IDs         shared.IDGenerator
}

func NewAndroidFlow(ctx context.Context, phone *waappv1.PhoneTarget, build *protocolv1.AndroidBuildMaterial, parameters *protocolv1.AndroidClientParameters, proxyURL string, dependencies AndroidDependencies) (*AndroidFlow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateAndroidClient(parameters); err != nil {
		return nil, err
	}
	token, err := AndroidRegistrationToken(build, shared.PhoneNational(phone))
	if err != nil {
		return nil, err
	}
	build = proto.Clone(build).(*protocolv1.AndroidBuildMaterial)
	parameters = proto.Clone(parameters).(*protocolv1.AndroidClientParameters)
	parameters.Metadata.AppVersion = build.GetAppVersion()
	clock, ids := dependencies.Clock, dependencies.IDs
	if clock == nil {
		clock = shared.SystemClock{}
	}
	if ids == nil {
		ids = shared.RandomIDGenerator{}
	}
	proxyURL, err = wire.NormalizeProxyURLString(proxyURL)
	if err != nil {
		return nil, err
	}
	current, err := state.NewNativeState(phone, parameters.GetMetadata(), parameters.GetMemoryGib())
	if err != nil {
		return nil, err
	}
	current.Client.LocaleLanguage, current.Client.LocaleCountry = RegistrationLocale(phone)
	current.Client.Mcc = current.Profile.AdditionalMapFields["mcc"]
	current.Client.Mnc = current.Profile.AdditionalMapFields["mnc"]
	current.Client.HandshakePqMode = parameters.GetOptions().GetHandshakePqMode()
	current.Client.NoiseRootPublicKey = build.GetNoiseRootPublicKey()
	if parameters.GetOptions().GetEnablePqRegistration() {
		private, err := current.Signal.Identity.PrivateBytes()
		if err != nil {
			return nil, err
		}
		current.Signal.PQLastResortPreKey, err = wire.NewSignalPQPreKey(private, state.RandomInt24())
		if err != nil {
			return nil, err
		}
	}
	client, err := wire.NewNativeHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	core := &engineCore{activeProxyURL: proxyURL, http: client, clock: clock, ids: ids, current: current, build: build, parameters: parameters, token: token}
	if dependencies.Attestation != nil {
		publicKey, err := current.ChatStatic.PublicBytes()
		if err != nil {
			return nil, err
		}
		core.attestor, err = android.CreateAttestor(ctx, dependencies.Attestation, publicKey, build)
		if err != nil && !attestation.Unavailable(ctx, err) {
			client.CloseIdleConnections()
			return nil, err
		}
		if core.attestor != nil {
			client.Attestor = core.attestor
		}
	}
	return &AndroidFlow{core, &registrationService{core}, proto.Clone(phone).(*waappv1.PhoneTarget)}, nil
}

func (e *AndroidFlow) Probe(ctx context.Context) wacore.EngineProbeResult {
	e.RefreshPreChatdABProps(ctx, e.phone, &e.current, e.build.GetAppVersion())
	result, current := e.ProbeAccountWithState(ctx, e.input(0), e.current)
	e.current = current
	result.SupportedMethods = slices.DeleteFunc(result.SupportedMethods, func(method waappv1.VerificationDeliveryMethod) bool {
		return method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER
	})
	result.MethodStatuses = slices.DeleteFunc(result.MethodStatuses, func(status wacore.VerificationMethodStatus) bool {
		return status.Method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER
	})
	return result
}
func (e *AndroidFlow) RequestCode(ctx context.Context, method waappv1.VerificationDeliveryMethod) wacore.EngineCodeResult {
	if method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
		return wacore.EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: unsupportedQRTransfer()}
	}
	e.RefreshPreChatdABProps(ctx, e.phone, &e.current, e.build.GetAppVersion())
	return e.RequestVerificationCode(ctx, e.input(method))
}
func (e *AndroidFlow) SubmitCode(ctx context.Context, method waappv1.VerificationDeliveryMethod, code string) wacore.EngineRegisterResult {
	return e.SubmitVerificationCode(ctx, wacore.EngineSubmitInput{EngineRegistrationInput: e.input(method), Code: code})
}
func (e *AndroidFlow) RefreshTransfer(ctx context.Context) wacore.EngineAccountTransferChallengeResult {
	return wacore.EngineAccountTransferChallengeResult{Err: unsupportedQRTransfer()}
}
func (e *AndroidFlow) Material(loginID, accountID string) (*waappv1.AcceptedAccountMaterial, error) {
	if loginID != e.phone.GetCountryCallingCode()+e.phone.GetNationalNumber() || accountID == "" {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "account identity does not match this flow", false)
	}
	credentials, recovery, err := e.current.Export()
	if err != nil {
		return nil, err
	}
	if recovery.GetRegistrationJid() == "" {
		recovery.RegistrationJid = state.NormalizeJID(loginID)
	}
	return &waappv1.AcceptedAccountMaterial{Phone: e.phone, ServiceLoginId: loginID, ServiceAccountId: accountID, Credentials: credentials, Recovery: recovery}, nil
}
func (e *AndroidFlow) Close() error {
	e.http.CloseIdleConnections()
	var err error
	if e.attestor != nil {
		err = e.attestor.Close(context.Background())
		e.attestor = nil
		e.http.Attestor = nil
	}
	if key := e.current.Signal.PQLastResortPreKey; key != nil {
		clear(key.Private)
	}
	for _, key := range e.current.Signal.PQOneTimePreKeys {
		clear(key.Private)
	}
	e.current = state.NativeState{}
	e.verification = nil
	e.verificationMethod = 0
	e.passwordRequired, e.passwordRetryAt = false, time.Time{}
	e.token = ""
	if e.build != nil {
		clear(e.build.TokenHmacKey)
	}
	e.build, e.parameters = nil, nil
	return err
}

func (e *AndroidFlow) input(method waappv1.VerificationDeliveryMethod) wacore.EngineRegistrationInput {
	return wacore.EngineRegistrationInput{Phone: e.phone, AppVersion: e.build.GetAppVersion(), DeliveryMethod: method}
}
func (e *engineCore) NewState(phone *waappv1.PhoneTarget) (state.NativeState, error) {
	return state.NewNativeState(phone, e.parameters.GetMetadata(), e.parameters.GetMemoryGib())
}
func (e *engineCore) loadState(context.Context, string) (state.NativeState, error) {
	return e.current, nil
}
func (e *engineCore) SaveState(_ context.Context, _ string, current state.NativeState) error {
	e.current = current
	return nil
}
func (e *engineCore) proxyURL() (string, error) { return e.activeProxyURL, nil }
