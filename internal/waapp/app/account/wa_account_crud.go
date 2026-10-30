package account

import (
	"context"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *serverCore) mutateAccount(ctx context.Context, id string, patch func(*waappv1.WAAccount) *waappv1.WAAccount) error {
	record, err := s.GetWAAccountRecord(ctx, id)
	if err != nil {
		return err
	}
	return s.store.WithinAccountTransaction(ctx, s.ids.NewID("account-update_"), record.GetPhone().GetE164Number(), func(tx AccountTransaction) error {
		current, err := tx.GetWAAccount(ctx, id)
		if err != nil {
			return err
		}
		return tx.SaveWAAccount(ctx, patch(current))
	})
}

func (s *serverCore) GetWAAccountRecord(ctx context.Context, accountID string) (*waappv1.WAAccount, error) {
	accountID, err := wamodel.RequireWAAccountID(accountID)
	if err != nil {
		return nil, err
	}
	account, err := s.store.GetWAAccount(ctx, accountID)
	if wamodel.IsWAAccountNotFound(err) {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, "WA account not found", false)
	}
	return account, err
}

func (s *serverCore) listWAAccounts(ctx context.Context, cursor string, limit int) ([]*waappv1.WAAccount, string, error) {
	return s.store.ListWAAccounts(ctx, strings.TrimSpace(cursor), limit)
}

func (s *serverCore) deleteWAAccount(ctx context.Context, accountID string) (bool, error) {
	if err := s.lifecycle.Acquire(ctx, 1); err != nil {
		return false, err
	}
	defer s.lifecycle.Release(1)
	accountID, err := wamodel.RequireWAAccountID(accountID)
	if err != nil {
		return false, err
	}
	if err := s.longConnections.StopAccount(ctx, accountID); err != nil {
		return false, err
	}
	record, err := s.GetWAAccountRecord(ctx, accountID)
	if wamodel.IsWAAccountNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	err = s.store.WithinAccountTransaction(ctx, "delete/"+accountID, record.GetPhone().GetE164Number(), func(tx AccountTransaction) error { return tx.DeleteWAAccount(ctx, accountID) })
	if wamodel.IsWAAccountNotFound(err) {
		return false, nil
	}
	return err == nil, err
}
