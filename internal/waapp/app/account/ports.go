package account

import (
	"context"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

// Repository is the persistence required by account operations.
type Repository interface {
	DiscoveryRepository
	WAAccountRepository
	AccountProxyRepository
	ClientProfileRepository
	LoginRepository
	AccountLifecycleRepository
	MessagingRepository
	WAContactRepository
}

type AccountProxyRepository interface {
	GetAccountProxy(context.Context, string) (*waappv1.AccountProxy, error)
	SaveAccountProxy(context.Context, string, *waappv1.AccountProxy) error
}

// DiscoveryRepository persists app artifacts and protocol profiles.
type DiscoveryRepository interface {
	SaveAppArtifact(context.Context, *waappv1.AppArtifact) error
	GetAppArtifact(context.Context, string) (*waappv1.AppArtifact, error)
	SaveProtocolProfile(context.Context, *waappv1.ProtocolProfile) error
	GetProtocolProfile(context.Context, string) (*waappv1.ProtocolProfile, error)
}

// WAAccountRepository persists WA accounts.
type WAAccountRepository interface {
	SaveWAAccount(context.Context, *waappv1.WAAccount) error
	GetWAAccount(context.Context, string) (*waappv1.WAAccount, error)
	FindWAAccountByPhone(context.Context, string) (*waappv1.WAAccount, error)
	ListWAAccounts(context.Context, string, int) ([]*waappv1.WAAccount, string, error)
	DeleteWAAccount(context.Context, string) error
}

// ClientProfileRepository persists client (device) profiles.
type ClientProfileRepository interface {
	SaveClientProfile(context.Context, *waappv1.ClientProfile) error
	GetClientProfile(context.Context, string) (*waappv1.ClientProfile, error)
	ListClientProfiles(context.Context, string, string, int) ([]*waappv1.ClientProfile, string, error)
}

type AccountLifecycleRepository interface {
	WithinAccountTransaction(context.Context, string, string, func(AccountTransaction) error) error
	GetAccountAcceptance(context.Context, string) (*waappv1.AccountAcceptanceRecord, error)
}

type AccountTransaction interface {
	SaveAccountProxy(context.Context, string, *waappv1.AccountProxy) error
	SaveAppArtifact(context.Context, *waappv1.AppArtifact) error
	SaveProtocolProfile(context.Context, *waappv1.ProtocolProfile) error
	SaveWAAccount(context.Context, *waappv1.WAAccount) error
	GetWAAccount(context.Context, string) (*waappv1.WAAccount, error)
	FindWAAccountByPhone(context.Context, string) (*waappv1.WAAccount, error)
	DeleteWAAccount(context.Context, string) error
	SaveClientProfile(context.Context, *waappv1.ClientProfile) error
	SaveLoginState(context.Context, *waappv1.LoginState) error
	GetActiveLoginState(context.Context, string, string) (*waappv1.LoginState, error)
	GetLoginStateByRegisteredIdentity(context.Context, string) (*waappv1.LoginState, error)
	GetAccountAcceptance(context.Context, string) (*waappv1.AccountAcceptanceRecord, error)
	SaveAccountAcceptance(context.Context, string, *waappv1.AccountAcceptanceRecord) error
	SaveProtocolState(context.Context, string, *protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState) error
}

type LoginRepository interface {
	SaveLoginState(context.Context, *waappv1.LoginState) error
	GetLoginState(context.Context, string) (*waappv1.LoginState, error)
	GetLatestAccountLoginState(context.Context, string) (*waappv1.LoginState, error)
	GetActiveLoginState(context.Context, string, string) (*waappv1.LoginState, error)
	ListActiveLoginStates(context.Context) ([]*waappv1.LoginState, error)
	ListStoppedLoginStates(context.Context) ([]*waappv1.LoginState, error)
	GetLoginStateByRegisteredIdentity(context.Context, string) (*waappv1.LoginState, error)
}

// MessagingRepository persists message sessions, inbound/decrypted messages,
// extracted candidates and OTP messages.
type MessagingRepository interface {
	SaveMessageSession(context.Context, *waappv1.MessageSession) error
	GetMessageSession(context.Context, string) (*waappv1.MessageSession, error)
	CloseStaleOpenMessageSessions(context.Context, time.Time) (int64, error)
	SaveInboundMessages(context.Context, []*waappv1.InboundMessage) error
	GetInboundMessage(context.Context, string) (*waappv1.InboundMessage, error)
	ListPendingEncryptedInboundMessages(context.Context, string, string, int) ([]*waappv1.InboundMessage, error)
	ListPendingReadMessagesByContactRefs(context.Context, string, []string, bool, int) ([]*waappv1.InboundMessage, error)
	ListPendingDeliveryReceipts(context.Context, string, int) ([]*waappv1.InboundMessage, error)
	ListAccountMessages(context.Context, string, []string, string, int, bool) ([]*waappv1.AccountMessage, string, error)
	ListLatestAccountMessages(context.Context, string, []string) ([]*waappv1.AccountMessage, error)
	ListMessageChanges(context.Context, string, []string, []string, bool) ([]*waappv1.AccountMessage, error)
	SaveMessageReceipts(context.Context, string, []*waappv1.MessageReceipt) error
	ListMessageReceipts(context.Context, string, []string) ([]*waappv1.MessageReceipt, error)
	SaveDecryptedMessage(context.Context, *waappv1.DecryptedMessage) error
	GetDecryptedMessage(context.Context, string) (*waappv1.DecryptedMessage, error)
	GetLatestDecryptedMessage(context.Context, string) (*waappv1.DecryptedMessage, error)
	SaveCandidates(context.Context, []*waappv1.ExtractedCandidate) error
	SaveOTPMessage(context.Context, *waappv1.OtpMessage) error
	ListAccountOTPMessages(context.Context, string, string, int, bool) ([]*waappv1.OtpMessage, string, error)
}

// WAContactRepository persists WA contacts.
type WAContactRepository interface {
	SaveWAContacts(context.Context, []*waappv1.WAContact) error
	GetWAContact(context.Context, string) (*waappv1.WAContact, error)
	GetWAContactByRef(context.Context, string, string) (*waappv1.WAContact, error)
	ListWAContacts(context.Context, string, string, int) ([]*waappv1.WAContact, string, error)
	DeleteWAContact(context.Context, string, []string, time.Time) (*waappv1.DeleteWAContactResponse, error)
}
