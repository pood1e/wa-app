package account

import (
	"context"
	"log"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type SessionFactory func(context.Context, wacore.EngineMessageInput) (wacore.Communication, error)

func (s *serverCore) NewSessionProtocol(ctx context.Context, login *waappv1.LoginState, current *waappv1.MessageSession) (wacore.Communication, error) {
	input := longConnectionEngineInput(current)
	input.AppVersion = s.protocolIDAppVersion(ctx, input.ProtocolProfileID)
	return s.factory(ctx, input)
}
func (s *serverCore) ensureLongConnection(ctx context.Context, loginState *waappv1.LoginState) {
	if s != nil && s.longConnections != nil {
		s.longConnections.Ensure(ctx, loginState)
	}
}

func (s *serverCore) revokeLongConnection(registeredIdentityID string, cause error) {
	if s != nil && s.longConnections != nil {
		s.longConnections.Revoke(registeredIdentityID, cause)
	}
}

// AccountTransferredOut keeps a retired identity from changing a newer account generation.
func (s *serverCore) AccountTransferredOut(ctx context.Context, login *waappv1.LoginState, cause error) {
	if login == nil {
		return
	}
	defer s.revokeLongConnection(login.GetRegisteredIdentityId(), cause)
	record, err := s.GetWAAccountRecord(ctx, login.GetWaAccountId())
	if err != nil {
		return
	}
	err = s.store.WithinAccountTransaction(ctx, "logout/"+login.GetRegisteredIdentityId(), record.GetPhone().GetE164Number(), func(tx AccountTransaction) error {
		current, err := tx.GetLoginStateByRegisteredIdentity(ctx, login.GetRegisteredIdentityId())
		if err != nil {
			return err
		}
		now := s.clock.Now()
		if current.Audit == nil {
			current.Audit = shared.NewAuditStamp(now, now)
		}
		current.Status = waappv1.LoginStateStatus_LOGIN_STATE_STATUS_REVOKED
		current.LastError = shared.ToProtoError(cause)
		current.Audit.UpdatedAt = timestamppb.New(now)
		if err := tx.SaveLoginState(ctx, current); err != nil {
			return err
		}
		active, err := tx.GetActiveLoginState(ctx, record.GetWaAccountId(), "")
		if err == nil && active.GetRegisteredIdentityId() != current.GetRegisteredIdentityId() {
			return nil
		}
		if err != nil && shared.ToProtoError(err).GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND {
			return err
		}
		account, err := tx.GetWAAccount(ctx, record.GetWaAccountId())
		if err != nil {
			return err
		}
		if account.GetStatus() == waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_LOGGED_OUT {
			return nil
		}
		return tx.SaveWAAccount(ctx, withWAAccountStatus(account, waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_TRANSFERRED_OUT, now))
	})
	if err != nil {
		log.Printf("WA persist transferred-out failed: %v", shared.SanitizeLogError(err))
	}
}

func longConnectionEngineInput(session *waappv1.MessageSession) wacore.EngineMessageInput {
	if session == nil {
		return wacore.EngineMessageInput{}
	}
	return wacore.EngineMessageInput{
		WAAccountID:          session.GetWaAccountId(),
		ClientProfileID:      session.GetClientProfileId(),
		RegisteredIdentityID: session.GetRegisteredIdentityId(),
		ProtocolProfileID:    session.GetProtocolProfileId(),
		MessageSessionID:     session.GetMessageSessionId(),
	}
}
