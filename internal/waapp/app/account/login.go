package account

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *loginHandler) GetLoginState(ctx context.Context, req *waappv1.GetLoginStateRequest) (*waappv1.GetLoginStateResponse, error) {

	loginState, err := s.store.GetLoginState(ctx, req.GetLoginStateId())
	if err != nil {
		return &waappv1.GetLoginStateResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.GetLoginStateResponse{LoginState: loginState}, nil
}

func (s *loginHandler) GetActiveLoginState(ctx context.Context, req *waappv1.GetActiveLoginStateRequest) (*waappv1.GetActiveLoginStateResponse, error) {

	accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.GetActiveLoginStateResponse{Error: shared.ToProtoError(err)}, nil
	}
	loginState, err := s.store.GetActiveLoginState(ctx, accountID, req.GetClientProfileId())
	if err != nil {
		return &waappv1.GetActiveLoginStateResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.GetActiveLoginStateResponse{LoginState: loginState}, nil
}

func (s *loginHandler) CheckLoginState(ctx context.Context, req *waappv1.CheckLoginStateRequest) (*waappv1.CheckLoginStateResponse, error) {
	return s.CheckLoginStateWithRunner(ctx, req, s.runner)
}

func (s *serverCore) CheckLoginStateWithRunner(ctx context.Context, req *waappv1.CheckLoginStateRequest, runner wacore.Communication) (*waappv1.CheckLoginStateResponse, error) {

	loginState, err := s.loginStateForCheck(ctx, req)
	if err != nil {
		return &waappv1.CheckLoginStateResponse{Error: shared.ToProtoError(err)}, nil
	}
	owned, err := s.longConnections.Acquire(ctx, loginState)
	if err != nil {
		return &waappv1.CheckLoginStateResponse{LoginState: loginState, Error: shared.ToProtoError(err)}, nil
	}
	result := owned.CheckLoginState(ctx, wacore.EngineLoginCheckInput{WAAccountID: loginState.GetWaAccountId(), ClientProfileID: loginState.GetClientProfileId(), RegisteredIdentityID: loginState.GetRegisteredIdentityId(), AppVersion: s.loginStateAppVersion(ctx, loginState), RemoteTimeout: shared.DurationFromProto(req.GetRemoteTimeout())})

	now := s.clock.Now()
	check := &waappv1.LoginStateCheck{
		LoginStateCheckId:    s.ids.NewID("walogchk_"),
		LoginStateId:         loginState.GetLoginStateId(),
		WaAccountId:          loginState.GetWaAccountId(),
		ClientProfileId:      loginState.GetClientProfileId(),
		RegisteredIdentityId: loginState.GetRegisteredIdentityId(),
		Status:               result.Status,
		CheckedAt:            timestamppb.New(now),
		Error:                shared.ToProtoError(result.Err),
	}
	if check.GetStatus() == waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNSPECIFIED && result.Err == nil {
		check.Status = waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE
	}
	if s.applyLoginStateCheck(loginState, check, now) {
		if err := s.store.SaveLoginState(ctx, loginState); err != nil {
			return &waappv1.CheckLoginStateResponse{LoginState: loginState, Check: check, Error: shared.ToProtoError(err)}, nil
		}
	}
	if check.GetStatus() == waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE && loginState.GetStatus() == waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
		s.ensureLongConnection(ctx, loginState)
	}
	return &waappv1.CheckLoginStateResponse{LoginState: loginState, Check: check, Error: check.GetError()}, nil
}

func (s *serverCore) loginStateForCheck(ctx context.Context, req *waappv1.CheckLoginStateRequest) (*waappv1.LoginState, error) {
	if req.GetLoginStateId() != "" {
		return s.store.GetLoginState(ctx, req.GetLoginStateId())
	}
	if req.GetRegisteredIdentityId() != "" {
		return s.store.GetLoginStateByRegisteredIdentity(ctx, req.GetRegisteredIdentityId())
	}
	if req.GetWaAccountId() != "" && req.GetClientProfileId() != "" {
		accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
		if err != nil {
			return nil, err
		}
		return s.store.GetActiveLoginState(ctx, accountID, req.GetClientProfileId())
	}
	return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "login_state_id, registered_identity_id, or wa_account_id/client_profile_id is required", false)
}

func (s *serverCore) applyLoginStateCheck(loginState *waappv1.LoginState, check *waappv1.LoginStateCheck, now time.Time) bool {
	if loginState.GetAudit() == nil {
		loginState.Audit = &waappv1.AuditStamp{CreatedAt: timestamppb.New(now)}
	}
	switch check.GetStatus() {
	case waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE:
		loginState.Status = waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE
		loginState.LastVerifiedAt = check.GetCheckedAt()
		loginState.LastError = nil
	case waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_INVALID:
		loginState.Status = waappv1.LoginStateStatus_LOGIN_STATE_STATUS_INVALID
		loginState.LastError = check.GetError()
	case waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE:
		loginState.LastError = check.GetError()
	default:
		return false
	}
	loginState.Audit.UpdatedAt = timestamppb.New(now)
	return true
}

func (s *serverCore) waAccountAndProfile(ctx context.Context, waAccountIDValue string, clientProfileID string) (*waappv1.WAAccount, *waappv1.ClientProfile, error) {
	accountID, err := wamodel.RequireWAAccountID(waAccountIDValue)
	if err != nil {
		return nil, nil, err
	}
	account, err := s.GetWAAccountRecord(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	profile, err := s.store.GetClientProfile(ctx, clientProfileID)
	if err != nil {
		return nil, nil, err
	}
	if profile.GetWaAccountId() != wamodel.WAAccountID(account) {
		return nil, nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "client profile does not belong to WA account", false)
	}
	return account, profile, nil
}
