package store

import (
	"context"
	"slices"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (s *PostgresStore) GetLatestAccountLoginState(ctx context.Context, accountID string) (*waappv1.LoginState, error) {
	return s.getLoginState(ctx, `wa_account_id=$1 ORDER BY registered_at DESC,created_at DESC,login_state_id DESC LIMIT 1`, accountID)
}

func (s *SQLiteStore) GetLatestAccountLoginState(ctx context.Context, accountID string) (*waappv1.LoginState, error) {
	states, err := sqliteListPayloads(ctx, s.queries, func() *waappv1.LoginState { return new(waappv1.LoginState) }, `SELECT payload FROM wa_sqlite_login_states WHERE wa_account_id=?`, accountID)
	if err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND, "login state not found", false)
	}
	return slices.MaxFunc(states, compareLoginGeneration), nil
}

func compareLoginGeneration(left, right *waappv1.LoginState) int {
	if order := left.GetRegisteredAt().AsTime().Compare(right.GetRegisteredAt().AsTime()); order != 0 {
		return order
	}
	if order := left.GetAudit().GetCreatedAt().AsTime().Compare(right.GetAudit().GetCreatedAt().AsTime()); order != 0 {
		return order
	}
	return strings.Compare(left.GetLoginStateId(), right.GetLoginStateId())
}
