package wacore

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

// Communication exposes account operation protocols without device preparation.
type Communication interface {
	CheckLoginState(context.Context, EngineLoginCheckInput) EngineLoginCheckResult
	ReceiveMessageBatch(context.Context, EngineMessageInput) EngineMessageBatchResult
	DecryptMessage(context.Context, EngineDecryptInput) EngineDecryptResult
	ApplyAccountSettings(context.Context, EngineAccountSettingsInput) EngineAccountSettingsResult
}

type EngineProfileInput struct {
	WAAccountID       string
	ClientProfileID   string
	ProtocolProfileID string
	AppVersion        string
	Phone             *waappv1.PhoneTarget
}

type EngineRegistrationInput struct {
	WAAccountID       string
	ClientProfileID   string
	ProtocolProfileID string
	AppVersion        string
	Phone             *waappv1.PhoneTarget
	DeliveryMethod    waappv1.VerificationDeliveryMethod
	AuthCodeContext   string
}

type EngineSubmitInput struct {
	EngineRegistrationInput
	VerificationRequestID string
	Code                  string
	CodeSecretRef         string
}

type EngineAccountTransferChallengeInput struct {
	EngineRegistrationInput
	VerificationRequestID string
}

type EngineLoginCheckInput struct {
	WAAccountID          string
	ClientProfileID      string
	RegisteredIdentityID string
	AppVersion           string
	RemoteTimeout        time.Duration
}

type EngineMessageInput struct {
	WAAccountID          string
	ClientProfileID      string
	RegisteredIdentityID string
	ProtocolProfileID    string
	AppVersion           string
	MessageSessionID     string
	WaitTimeout          time.Duration
	MaxMessages          int
}

type EngineDecryptInput struct {
	MessageID            string
	MessageSessionID     string
	ClientProfileID      string
	PayloadRef           string
	SessionCommitPolicy  waappv1.SessionCommitPolicy
	IncludePlaintextText bool
}

type EngineAccountSettingsInput struct {
	WAAccountID          string
	ClientProfileID      string
	RegisteredIdentityID string
	LoginStateID         string
	AppVersion           string
	Kind                 waappv1.AccountSettingsOperationKind
	Password             string
	EmailAddress         string
	GoogleIDToken        string
	LocaleLanguage       string
	LocaleCountry        string
	Code                 string
	DisplayName          string
	ProfilePicture       []byte
}

type EngineContactResolveInput struct {
	WAAccountID          string
	ClientProfileID      string
	RegisteredIdentityID string
	AppVersion           string
	JIDs                 []string
	RemoteTimeout        time.Duration
}

type EngineContactProfilePictureInput struct {
	WAAccountID          string
	ClientProfileID      string
	RegisteredIdentityID string
	AppVersion           string
	ContactJID           string
	ContactPNJID         string
	ContactPictureID     string
	RemoteTimeout        time.Duration
}

type EngineProbeResult struct {
	Status           waappv1.AccountProbeStatus
	AccountFlow      string
	RawStatus        string
	RawReason        string
	RegisteredKnown  bool
	Registered       bool
	Blocked          bool
	SMSWaitSeconds   int64
	CanSendSMS       bool
	SupportedMethods []waappv1.VerificationDeliveryMethod
	MethodStatuses   []VerificationMethodStatus
	Err              error
}

type VerificationMethodStatus struct {
	Method          waappv1.VerificationDeliveryMethod
	Code            string
	Available       bool
	CooldownSeconds int64
}

type EngineCodeResult struct {
	Status                   waappv1.VerificationRequestStatus
	ExpectedCodeLength       int32
	ExpiresAt                time.Time
	RetryAfter               time.Duration
	MethodStatuses           []VerificationMethodStatus
	AccountTransferChallenge *waappv1.AccountTransferChallenge
	RawStatus                string
	RawReason                string
	Err                      error
}

type EngineAccountTransferChallengeResult struct {
	Challenge *waappv1.AccountTransferChallenge
	Err       error
}

type EngineRegisterResult struct {
	PasswordChallenge *waappv1.AccountAddPasswordChallenge
	OutcomeUnknown    bool
	Status            waappv1.RegistrationStatus
	RetryAfter        time.Duration
	RegisteredID      string
	ServiceAccountID  string
	ServiceLoginID    string
	CompletedAt       time.Time
	Err               error
}

type EngineLoginCheckResult struct {
	Status waappv1.LoginStateCheckStatus
	Err    error
}

type EngineMessageReceiptInput struct {
	WAAccountID          string
	ClientProfileID      string
	RegisteredIdentityID string
	AppVersion           string
	Messages             []EngineMessageReceipt
	RemoteTimeout        time.Duration
	Type                 waappv1.MessageReceiptType
}

type EngineMessageReceipt struct {
	ChatJID           string
	ParticipantJID    string
	ProviderMessageID string
}

type EngineMessageReceiptResult struct {
	Sent int
	Err  error
}

type EngineMessageSendInput struct {
	WAAccountID          string
	ClientProfileID      string
	RegisteredIdentityID string
	AppVersion           string
	ContactJID           string
	Message              *waE2E.Message
	ClientMessageID      string
	RemoteTimeout        time.Duration
}

type EngineMessageSendResult struct {
	ProviderMessageID string
	SentAt            time.Time
	AckStatus         waappv1.MessageAckStatus
	Err               error
}

type EngineMessageBatchResult struct {
	Messages      []*waappv1.InboundMessage
	Contacts      []*waappv1.WAContact
	OTPMessages   []*waappv1.OtpMessage
	AccountLogout *EngineAccountLogout
	Receipts      []*waappv1.MessageReceipt
	Presences     []*waappv1.WAContactPresence
	Err           error
}

// EngineAccountLogout 表示该账号已被服务端登出(号码在其他设备注册/被接管),
// 调用方据此把登录态置为 REVOKED 并停止长连接。
type EngineAccountLogout struct {
	Reason              string
	NewDevicePlatform   string
	NewDeviceAppVersion string
}

type EngineDecryptResult struct {
	DecryptedMessage *waappv1.DecryptedMessage
	MessageKind      waappv1.InboundMessageKind
	Candidates       []*waappv1.ExtractedCandidate
	ContactHints     []WAContactHint
	Err              error
}

type EngineAccountSettingsResult struct {
	Status           waappv1.AccountSettingsOperationStatus
	WaitTime         time.Duration
	TwoFactorStatus  *waappv1.TwoFactorAuthStatus
	ProfilePictureID string
	HasStaging       bool
	Err              error
}

type EngineContactResolveResult struct {
	Contacts []*waappv1.WAContact
	Queried  int
	Resolved int
	Err      error
}

type EngineContactProfilePictureResult struct {
	ProfilePictureID string
	ContentType      string
	Data             []byte
	Err              error
}
