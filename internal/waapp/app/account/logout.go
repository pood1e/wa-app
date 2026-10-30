package account

import (
	"context"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (s *profileHandler) LogoutWAAccount(ctx context.Context, req *waappv1.LogoutWAAccountRequest) (*waappv1.LogoutWAAccountResponse, error) {
	if err := s.lifecycle.Acquire(ctx, 1); err != nil {
		return &waappv1.LogoutWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	defer s.lifecycle.Release(1)
	record, err := s.GetWAAccountRecord(ctx, req.GetWaAccountId())
	if err != nil {
		return &waappv1.LogoutWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	err = s.store.WithinAccountTransaction(ctx, "account-logout/"+record.GetWaAccountId(), record.GetPhone().GetE164Number(), func(tx AccountTransaction) error {
		current, err := tx.GetWAAccount(ctx, record.GetWaAccountId())
		if err != nil {
			return err
		}
		now := s.clock.Now()
		if err := retireAccountLogins(ctx, tx, current.GetWaAccountId(), now); err != nil {
			return err
		}
		record = current
		if current.GetStatus() != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_TRANSFERRED_OUT {
			record = withWAAccountStatus(current, waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_LOGGED_OUT, now)
		}
		return tx.SaveWAAccount(ctx, record)
	})
	if err != nil {
		return &waappv1.LogoutWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	// Once logout is durable, finish closing the connection if the caller disconnects.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	err = s.longConnections.StopAccount(cleanup, record.GetWaAccountId())
	return &waappv1.LogoutWAAccountResponse{Account: record, Error: shared.ToProtoError(err)}, nil
}
