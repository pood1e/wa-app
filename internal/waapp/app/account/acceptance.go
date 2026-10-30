package account

import (
	"context"
	"log"
	"strings"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (a *Application) AcceptAccount(ctx context.Context, req *waappv1.AcceptAccountRequest) (*waappv1.AcceptAccountResponse, error) {
	if err := validateAcceptanceID(req.GetContext().GetRequestId()); err != nil {
		return &waappv1.AcceptAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	if proto.Size(req) > 4<<20 {
		return &waappv1.AcceptAccountResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "account handoff exceeds 4 MiB", false))}, nil
	}
	if err := communication.ValidateMaterial(req.GetMaterial()); err != nil {
		return &waappv1.AcceptAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	if err := a.lifecycle.Acquire(ctx, 1); err != nil {
		return &waappv1.AcceptAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	defer a.lifecycle.Release(1)
	account, err := a.accept(ctx, req)
	if err == nil {
		a.activateAccount(ctx, account)
	}
	return &waappv1.AcceptAccountResponse{Account: account, Error: shared.ToProtoError(err)}, nil
}

func (a *Application) GetAccountAcceptance(ctx context.Context, req *waappv1.GetAccountAcceptanceRequest) (*waappv1.AcceptAccountResponse, error) {
	if err := a.lifecycle.Acquire(ctx, 1); err != nil {
		return &waappv1.AcceptAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	defer a.lifecycle.Release(1)
	if err := validateAcceptanceID(req.GetAcceptanceRequestId()); err != nil {
		return &waappv1.AcceptAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	record, err := a.store.GetAccountAcceptance(ctx, req.GetAcceptanceRequestId())
	account := record.GetAccount()
	if err == nil {
		a.activateAccount(ctx, account)
	}
	return &waappv1.AcceptAccountResponse{Account: account, Error: shared.ToProtoError(err)}, nil
}

func (a *Application) activateAccount(ctx context.Context, account *waappv1.WAAccount) {
	login, err := a.store.GetActiveLoginState(ctx, account.GetWaAccountId(), "")
	if err == nil && a.longConnections != nil {
		if err := a.longConnections.Handoff(ctx, login); err != nil {
			log.Printf("WA account session handoff failed: %v", shared.SanitizeLogError(err))
		}
	}
}

func validateAcceptanceID(id string) error {
	if strings.TrimSpace(id) == "" || len(id) > 128 {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "stable acceptance request ID is required", false)
	}
	return nil
}
