package account

import (
	"context"

	modelv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/model/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (s *accountSettingsHandler) SetAccountBusinessName(ctx context.Context, request *waappv1.SetAccountBusinessNameRequest) (*waappv1.SetAccountBusinessNameResponse, error) {
	name, err := shared.RequireBusinessName(request.GetBusinessName())
	if err != nil {
		return &waappv1.SetAccountBusinessNameResponse{Error: shared.ToProtoError(err)}, nil
	}
	op, err := s.applyAccountSettings(ctx, request.GetContext(), request.GetSelector(), waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_BUSINESS_NAME_SET, func(input wacore.EngineAccountSettingsInput) wacore.EngineAccountSettingsInput {
		input.DisplayName = name
		return input
	})
	if err != nil {
		return &waappv1.SetAccountBusinessNameResponse{Error: shared.ToProtoError(err)}, nil
	}
	if op.GetError() != nil {
		return &waappv1.SetAccountBusinessNameResponse{Operation: op, Error: op.GetError()}, nil
	}
	err = s.mutateAccount(ctx, op.GetWaAccountId(), func(current *waappv1.WAAccount) *waappv1.WAAccount {
		next := withWAAccountDisplayName(current, name, accountSettingsCompletedAt(op, s.clock.Now()))
		next.AccountType = modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS
		return next
	})
	return &waappv1.SetAccountBusinessNameResponse{Operation: op, Error: shared.ToProtoError(err)}, nil
}
