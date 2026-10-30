package account

import (
	"context"
	"log"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

// AccountLoginRejected preserves credentials while stopping a rejected login generation.
func (s *serverCore) AccountLoginRejected(ctx context.Context, login *waappv1.LoginState, cause error) {
	if login == nil {
		return
	}
	defer s.revokeLongConnection(login.GetRegisteredIdentityId(), cause)
	record, err := s.GetWAAccountRecord(ctx, login.GetWaAccountId())
	if err != nil {
		return
	}
	err = s.store.WithinAccountTransaction(ctx, "login-rejection/"+login.GetRegisteredIdentityId(), record.GetPhone().GetE164Number(), func(tx AccountTransaction) error {
		current, err := tx.GetLoginStateByRegisteredIdentity(ctx, login.GetRegisteredIdentityId())
		if err != nil {
			return err
		}
		if current.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
			return nil
		}
		now := s.clock.Now()
		if current.Audit == nil {
			current.Audit = shared.NewAuditStamp(now, now)
		}
		current.Status = waappv1.LoginStateStatus_LOGIN_STATE_STATUS_INVALID
		current.LastError = shared.ToProtoError(cause)
		current.Audit.UpdatedAt = timestamppb.New(now)
		return tx.SaveLoginState(ctx, current)
	})
	if err != nil {
		log.Printf("WA persist rejected login failed: %v", shared.SanitizeLogError(err))
	}
}
