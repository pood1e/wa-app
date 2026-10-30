package account

import (
	"context"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	protocolcommunication "github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *serverCore) refreshTwoFactorAuthStatus(ctx context.Context, requestContext *waappv1.RequestContext, selector *waappv1.AccountLoginSelector) (*waappv1.TwoFactorAuthStatus, *waappv1.WaError, error) {
	operationCtx, cancel := accountSettingsOperationContext(ctx)
	defer cancel()
	loginState, err := s.accountSettingsLoginState(operationCtx, selector)
	if err != nil {
		return nil, nil, err
	}
	runner, release, err := s.accountSettingsRunner(operationCtx, requestContext, loginState, waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_TWO_FACTOR_AUTH_STATUS_GET)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	result := runner.ApplyAccountSettings(operationCtx, wacore.EngineAccountSettingsInput{
		WAAccountID:          loginState.GetWaAccountId(),
		ClientProfileID:      loginState.GetClientProfileId(),
		RegisteredIdentityID: loginState.GetRegisteredIdentityId(),
		LoginStateID:         loginState.GetLoginStateId(),
		AppVersion:           s.loginStateAppVersion(operationCtx, loginState),
		Kind:                 waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_TWO_FACTOR_AUTH_STATUS_GET,
	})
	if result.Err != nil {
		return nil, shared.ToProtoError(result.Err), nil
	}
	status := cloneTwoFactorAuthStatus(result.TwoFactorStatus)
	if status == nil {
		status = &waappv1.TwoFactorAuthStatus{}
	}
	if err := s.saveTwoFactorAuthStatus(operationCtx, loginState.GetWaAccountId(), status, s.clock.Now()); err != nil {
		return nil, nil, err
	}
	return status, nil, nil
}

func (s *serverCore) cachedTwoFactorAuthStatus(ctx context.Context, selector *waappv1.AccountLoginSelector) (*waappv1.TwoFactorAuthStatus, error) {
	accountID, err := s.accountSettingsAccountID(ctx, selector)
	if err != nil {
		return nil, err
	}
	account, err := s.GetWAAccountRecord(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return cloneTwoFactorAuthStatus(account.GetTwoFactorAuth()), nil
}

func (s *serverCore) patchTwoFactorAuthStatus(ctx context.Context, accountID string, updatedAt time.Time, patch func(*waappv1.TwoFactorAuthStatus)) error {
	account, err := s.GetWAAccountRecord(ctx, accountID)
	if err != nil {
		return err
	}
	status := cloneTwoFactorAuthStatus(account.GetTwoFactorAuth())
	if status == nil {
		status = &waappv1.TwoFactorAuthStatus{}
	}
	patch(status)
	return s.saveTwoFactorAuthStatusForAccount(ctx, account, status, updatedAt)
}

func (s *serverCore) saveTwoFactorAuthStatus(ctx context.Context, accountID string, status *waappv1.TwoFactorAuthStatus, updatedAt time.Time) error {
	account, err := s.GetWAAccountRecord(ctx, accountID)
	if err != nil {
		return err
	}
	return s.saveTwoFactorAuthStatusForAccount(ctx, account, status, updatedAt)
}

func (s *serverCore) saveTwoFactorAuthStatusForAccount(ctx context.Context, account *waappv1.WAAccount, status *waappv1.TwoFactorAuthStatus, updatedAt time.Time) error {
	if updatedAt.IsZero() {
		updatedAt = s.clock.Now()
	}
	return s.mutateAccount(ctx, account.GetWaAccountId(), func(current *waappv1.WAAccount) *waappv1.WAAccount {
		return withWAAccountTwoFactorAuthStatus(current, preserveTwoFactorEmailProjection(current.GetTwoFactorAuth(), status), updatedAt)
	})
}

func preserveTwoFactorEmailProjection(current *waappv1.TwoFactorAuthStatus, next *waappv1.TwoFactorAuthStatus) *waappv1.TwoFactorAuthStatus {
	out := cloneTwoFactorAuthStatus(next)
	if out == nil || current == nil {
		return out
	}
	currentEmailAddress := strings.TrimSpace(current.GetEmailAddress())
	nextEmailAddress := strings.TrimSpace(out.GetEmailAddress())
	if nextEmailAddress == "" {
		out.EmailAddress = currentEmailAddress
		nextEmailAddress = currentEmailAddress
	}
	if nextEmailAddress == "" {
		return out
	}
	out.EmailConfigured = out.GetEmailConfigured() || current.GetEmailConfigured() || nextEmailAddress != ""
	if nextEmailAddress == currentEmailAddress {
		out.EmailVerified = out.GetEmailVerified() || current.GetEmailVerified()
		out.EmailConfirmed = out.GetEmailConfirmed() || current.GetEmailConfirmed()
	}
	return out
}

func mergeTwoFactorAuthStatus(status *waappv1.TwoFactorAuthStatus, patch *waappv1.TwoFactorAuthStatus) {
	if status == nil || patch == nil {
		return
	}
	status.Configured = status.GetConfigured() || patch.GetConfigured()
	patchEmailAddress := strings.TrimSpace(patch.GetEmailAddress())
	if patchEmailAddress != "" {
		currentEmailAddress := strings.TrimSpace(status.GetEmailAddress())
		status.EmailAddress = patchEmailAddress
		status.EmailConfigured = status.GetEmailConfigured() || patch.GetEmailConfigured() || patchEmailAddress != ""
		if patchEmailAddress == currentEmailAddress {
			status.EmailVerified = status.GetEmailVerified() || patch.GetEmailVerified()
			status.EmailConfirmed = status.GetEmailConfirmed() || patch.GetEmailConfirmed()
			return
		}
		status.EmailVerified = patch.GetEmailVerified()
		status.EmailConfirmed = patch.GetEmailConfirmed()
		return
	}
	status.EmailConfigured = status.GetEmailConfigured() || patch.GetEmailConfigured()
	status.EmailVerified = status.GetEmailVerified() || patch.GetEmailVerified()
	status.EmailConfirmed = status.GetEmailConfirmed() || patch.GetEmailConfirmed()
}

func (s *serverCore) accountSettingsAccountID(ctx context.Context, selector *waappv1.AccountLoginSelector) (string, error) {
	if selector.GetWaAccountId() != "" {
		return wamodel.RequireWAAccountID(selector.GetWaAccountId())
	}
	loginState, err := s.accountSettingsLoginState(ctx, selector)
	if err != nil {
		return "", err
	}
	return wamodel.RequireWAAccountID(loginState.GetWaAccountId())
}

func (s *serverCore) saveAccountDisplayName(ctx context.Context, id, displayName string, updatedAt time.Time) error {
	return s.mutateAccount(ctx, id, func(current *waappv1.WAAccount) *waappv1.WAAccount {
		return withWAAccountDisplayName(current, displayName, updatedAt)
	})
}

func accountSettingsCompletedAt(op *waappv1.AccountSettingsOperation, fallback time.Time) time.Time {
	if op == nil {
		return fallback
	}
	completedAt := shared.TimeFromProto(op.GetCompletedAt())
	if completedAt.IsZero() {
		return fallback
	}
	return completedAt
}

func accountSettingsOperationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, protocolcommunication.DefaultAccountSettingsOperationTimeout)
}

func (s *serverCore) accountSettingsRunner(ctx context.Context, requestContext *waappv1.RequestContext, loginState *waappv1.LoginState, kind waappv1.AccountSettingsOperationKind) (wacore.Communication, func(), error) {
	runner, err := s.longConnections.Acquire(ctx, loginState)
	return runner, func() {}, err
}

func (s *serverCore) accountSettingsLoginState(ctx context.Context, selector *waappv1.AccountLoginSelector) (*waappv1.LoginState, error) {
	if selector.GetLoginStateId() != "" {
		return requireActiveLoginState(func() (*waappv1.LoginState, error) {
			return s.store.GetLoginState(ctx, selector.GetLoginStateId())
		})
	}
	if selector.GetRegisteredIdentityId() != "" {
		return requireActiveLoginState(func() (*waappv1.LoginState, error) {
			return s.store.GetLoginStateByRegisteredIdentity(ctx, selector.GetRegisteredIdentityId())
		})
	}
	accountID, err := wamodel.RequireWAAccountID(selector.GetWaAccountId())
	if err != nil {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "login_state_id, registered_identity_id, or wa_account_id is required", false)
	}
	if selector.GetClientProfileId() != "" {
		return requireActiveLoginState(func() (*waappv1.LoginState, error) {
			return s.store.GetActiveLoginState(ctx, accountID, selector.GetClientProfileId())
		})
	}
	records, err := s.store.ListActiveLoginStates(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		loginState := record
		if loginState.GetWaAccountId() == accountID {
			return loginState, nil
		}
	}
	return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND, "active login state not found", false)
}

func requireActiveLoginState(load func() (*waappv1.LoginState, error)) (*waappv1.LoginState, error) {
	loginState, err := load()
	if err != nil {
		return nil, err
	}
	if loginState.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "login state is not active", false)
	}
	return loginState, nil
}

func accountSettingsStatus(kind waappv1.AccountSettingsOperationKind, result wacore.EngineAccountSettingsResult) waappv1.AccountSettingsOperationStatus {
	if result.Status != waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_UNSPECIFIED {
		return result.Status
	}
	if result.Err != nil {
		return waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED
	}
	if kind == waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_REQUEST {
		return waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_WAITING
	}
	return waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED
}

func accountSettingsSensitiveValue(value *waappv1.SensitiveText, label string, required bool) (string, error) {
	plain := strings.TrimSpace(value.GetValue())
	if plain != "" {
		return plain, nil
	}
	if strings.TrimSpace(value.GetSecretRef()) != "" {
		return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, label+" secret_ref is not supported by native account settings", false)
	}
	if required {
		return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, label+" is required", false)
	}
	return "", nil
}
