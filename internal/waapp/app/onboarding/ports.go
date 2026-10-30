package onboarding

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type Protocol interface {
	Probe(context.Context) wacore.EngineProbeResult
	RequestCode(context.Context, waappv1.VerificationDeliveryMethod) wacore.EngineCodeResult
	SubmitCode(context.Context, waappv1.VerificationDeliveryMethod, string) wacore.EngineRegisterResult
	SubmitPassword(context.Context, string) wacore.EngineRegisterResult
	RefreshTransfer(context.Context) wacore.EngineAccountTransferChallengeResult
	Material(string, string) (*waappv1.AcceptedAccountMaterial, error)
	Close() error
}

type Factory func(context.Context, *waappv1.BeginAccountAddRequest) (Protocol, error)
type Validator func(context.Context, *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error)

type Acceptor interface {
	AcceptAccount(context.Context, *waappv1.AcceptAccountRequest) (*waappv1.AcceptAccountResponse, error)
	GetAccountAcceptance(context.Context, *waappv1.GetAccountAcceptanceRequest) (*waappv1.AcceptAccountResponse, error)
}
