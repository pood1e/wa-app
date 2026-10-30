package account

import (
	"context"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	protocolwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

const (
	accountProfileNameMaxRunes = 25
)

func (s *accountSettingsHandler) GetTwoFactorAuthStatus(ctx context.Context, req *waappv1.GetTwoFactorAuthStatusRequest) (*waappv1.GetTwoFactorAuthStatusResponse, error) {

	if !req.GetRemoteRefresh() {
		status, err := s.cachedTwoFactorAuthStatus(ctx, req.GetSelector())
		if err != nil {
			return &waappv1.GetTwoFactorAuthStatusResponse{Error: shared.ToProtoError(err)}, nil
		}
		return &waappv1.GetTwoFactorAuthStatusResponse{Status: status}, nil
	}
	status, appErr, err := s.refreshTwoFactorAuthStatus(ctx, req.GetContext(), req.GetSelector())
	if err != nil {
		return &waappv1.GetTwoFactorAuthStatusResponse{Error: shared.ToProtoError(err)}, nil
	}
	if appErr != nil {
		return &waappv1.GetTwoFactorAuthStatusResponse{Error: appErr}, nil
	}
	return &waappv1.GetTwoFactorAuthStatusResponse{Status: status}, nil
}

func (s *accountSettingsHandler) SetTwoFactorAuthSettings(ctx context.Context, req *waappv1.SetTwoFactorAuthSettingsRequest) (*waappv1.SetTwoFactorAuthSettingsResponse, error) {

	password := req.GetPassword().GetValue()
	err := requireAccountPassword(password)
	if err != nil {
		return &waappv1.SetTwoFactorAuthSettingsResponse{Error: shared.ToProtoError(err)}, nil
	}
	op, err := s.applyAccountSettings(ctx, req.GetContext(), req.GetSelector(), waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_TWO_FACTOR_AUTH_SETTINGS, func(input wacore.EngineAccountSettingsInput) wacore.EngineAccountSettingsInput {
		input.Password = password
		return input
	})
	if err != nil {
		return &waappv1.SetTwoFactorAuthSettingsResponse{Error: shared.ToProtoError(err)}, nil
	}
	if op.GetError() == nil {
		if err := s.patchTwoFactorAuthStatus(ctx, op.GetWaAccountId(), accountSettingsCompletedAt(op, s.clock.Now()), func(status *waappv1.TwoFactorAuthStatus) {
			status.Configured = true
		}); err != nil {
			return &waappv1.SetTwoFactorAuthSettingsResponse{Operation: op, Error: shared.ToProtoError(err)}, nil
		}
	}
	return &waappv1.SetTwoFactorAuthSettingsResponse{Operation: op, Error: op.GetError()}, nil
}

func (s *accountSettingsHandler) SetAccountEmail(ctx context.Context, req *waappv1.SetAccountEmailRequest) (*waappv1.SetAccountEmailResponse, error) {

	emailAddress, err := requiredEmailAddress(req.GetEmailAddress(), "email_address")
	if err != nil {
		return &waappv1.SetAccountEmailResponse{Error: shared.ToProtoError(err)}, nil
	}
	googleIDToken, err := accountSettingsSensitiveValue(req.GetGoogleIdToken(), "google_id_token", false)
	if err != nil {
		return &waappv1.SetAccountEmailResponse{Error: shared.ToProtoError(err)}, nil
	}
	op, result, err := s.applyAccountSettingsResult(ctx, req.GetContext(), req.GetSelector(), waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_SET, func(input wacore.EngineAccountSettingsInput) wacore.EngineAccountSettingsInput {
		input.EmailAddress = emailAddress
		input.GoogleIDToken = googleIDToken
		return input
	})
	if err != nil {
		return &waappv1.SetAccountEmailResponse{Error: shared.ToProtoError(err)}, nil
	}
	if op.GetError() == nil {
		if err := s.patchTwoFactorAuthStatus(ctx, op.GetWaAccountId(), accountSettingsCompletedAt(op, s.clock.Now()), func(status *waappv1.TwoFactorAuthStatus) {
			mergeTwoFactorAuthStatus(status, result.TwoFactorStatus)
			if status.GetEmailAddress() == "" {
				status.EmailAddress = emailAddress
			}
			status.EmailConfigured = status.GetEmailConfigured() || status.GetEmailAddress() != ""
		}); err != nil {
			return &waappv1.SetAccountEmailResponse{Operation: op, Error: shared.ToProtoError(err)}, nil
		}
	}
	return &waappv1.SetAccountEmailResponse{Operation: op, Error: op.GetError()}, nil
}

func (s *accountSettingsHandler) RequestAccountEmailOtp(ctx context.Context, req *waappv1.RequestAccountEmailOtpRequest) (*waappv1.RequestAccountEmailOtpResponse, error) {

	op, err := s.applyAccountSettings(ctx, req.GetContext(), req.GetSelector(), waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_REQUEST, func(input wacore.EngineAccountSettingsInput) wacore.EngineAccountSettingsInput {
		input.LocaleLanguage = accountSettingsLocale(req.GetLocaleLanguage(), "en")
		input.LocaleCountry = accountSettingsLocale(req.GetLocaleCountry(), "US")
		return input
	})
	if err != nil {
		return &waappv1.RequestAccountEmailOtpResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.RequestAccountEmailOtpResponse{Operation: op, Error: op.GetError()}, nil
}

func (s *accountSettingsHandler) VerifyAccountEmailOtp(ctx context.Context, req *waappv1.VerifyAccountEmailOtpRequest) (*waappv1.VerifyAccountEmailOtpResponse, error) {

	code, err := accountSettingsSensitiveValue(req.GetCode(), "code", true)
	if err != nil {
		return &waappv1.VerifyAccountEmailOtpResponse{Error: shared.ToProtoError(err)}, nil
	}
	code, err = requireSixDigits(code, "code")
	if err != nil {
		return &waappv1.VerifyAccountEmailOtpResponse{Error: shared.ToProtoError(err)}, nil
	}
	op, err := s.applyAccountSettings(ctx, req.GetContext(), req.GetSelector(), waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_VERIFY, func(input wacore.EngineAccountSettingsInput) wacore.EngineAccountSettingsInput {
		input.Code = code
		return input
	})
	if err != nil {
		return &waappv1.VerifyAccountEmailOtpResponse{Error: shared.ToProtoError(err)}, nil
	}
	if op.GetError() == nil && op.GetStatus() == waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED {
		if err := s.patchTwoFactorAuthStatus(ctx, op.GetWaAccountId(), accountSettingsCompletedAt(op, s.clock.Now()), func(status *waappv1.TwoFactorAuthStatus) {
			mergeTwoFactorAuthStatus(status, &waappv1.TwoFactorAuthStatus{EmailConfigured: true, EmailVerified: true})
		}); err != nil {
			return &waappv1.VerifyAccountEmailOtpResponse{Operation: op, Error: shared.ToProtoError(err)}, nil
		}
	}
	return &waappv1.VerifyAccountEmailOtpResponse{Operation: op, Error: op.GetError()}, nil
}

func (s *accountSettingsHandler) SetAccountProfileName(ctx context.Context, req *waappv1.SetAccountProfileNameRequest) (*waappv1.SetAccountProfileNameResponse, error) {

	displayName, err := requiredAccountProfileName(req.GetDisplayName())
	if err != nil {
		return &waappv1.SetAccountProfileNameResponse{Error: shared.ToProtoError(err)}, nil
	}
	op, err := s.applyAccountSettings(ctx, req.GetContext(), req.GetSelector(), waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_PROFILE_NAME_SET, func(input wacore.EngineAccountSettingsInput) wacore.EngineAccountSettingsInput {
		input.DisplayName = displayName
		return input
	})
	if err != nil {
		return &waappv1.SetAccountProfileNameResponse{Error: shared.ToProtoError(err)}, nil
	}
	if op.GetError() == nil {
		if err := s.saveAccountDisplayName(ctx, op.GetWaAccountId(), displayName, accountSettingsCompletedAt(op, s.clock.Now())); err != nil {
			return &waappv1.SetAccountProfileNameResponse{Operation: op, Error: shared.ToProtoError(err)}, nil
		}
	}
	return &waappv1.SetAccountProfileNameResponse{Operation: op, Error: op.GetError()}, nil
}

func (s *accountSettingsHandler) SetAccountProfilePicture(ctx context.Context, req *waappv1.SetAccountProfilePictureRequest) (*waappv1.SetAccountProfilePictureResponse, error) {

	image, err := requiredAccountProfilePicture(req.GetImage(), req.GetContentType())
	if err != nil {
		return &waappv1.SetAccountProfilePictureResponse{Error: shared.ToProtoError(err)}, nil
	}
	contentType, _ := protocolwire.ProfilePictureContentType(image, req.GetContentType())
	op, result, err := s.applyAccountSettingsResult(ctx, req.GetContext(), req.GetSelector(), waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_PROFILE_PICTURE_SET, func(input wacore.EngineAccountSettingsInput) wacore.EngineAccountSettingsInput {
		input.ProfilePicture = image
		return input
	})
	if err != nil {
		return &waappv1.SetAccountProfilePictureResponse{Error: shared.ToProtoError(err)}, nil
	}
	if op.GetError() == nil {
		s.cacheWAAccountProfilePicture(ctx, op.GetWaAccountId(), WAContactProfilePicture{ProfilePictureID: result.ProfilePictureID, ContentType: contentType, Data: image})
	}
	return &waappv1.SetAccountProfilePictureResponse{Operation: op, ProfilePictureId: result.ProfilePictureID, HasStaging: result.HasStaging, Error: op.GetError()}, nil
}

func (s *accountSettingsHandler) RemoveAccountProfilePicture(ctx context.Context, req *waappv1.RemoveAccountProfilePictureRequest) (*waappv1.RemoveAccountProfilePictureResponse, error) {

	op, err := s.applyAccountSettings(ctx, req.GetContext(), req.GetSelector(), waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_PROFILE_PICTURE_REMOVE, nil)
	if err != nil {
		return &waappv1.RemoveAccountProfilePictureResponse{Error: shared.ToProtoError(err)}, nil
	}
	if op.GetError() == nil {
		s.deleteWAAccountProfilePictureCache(ctx, op.GetWaAccountId())
	}
	return &waappv1.RemoveAccountProfilePictureResponse{Operation: op, Error: op.GetError()}, nil
}

func (s *serverCore) applyAccountSettings(ctx context.Context, requestContext *waappv1.RequestContext, selector *waappv1.AccountLoginSelector, kind waappv1.AccountSettingsOperationKind, enrich func(wacore.EngineAccountSettingsInput) wacore.EngineAccountSettingsInput) (*waappv1.AccountSettingsOperation, error) {
	op, _, err := s.applyAccountSettingsResult(ctx, requestContext, selector, kind, enrich)
	return op, err
}

func (s *serverCore) applyAccountSettingsResult(ctx context.Context, requestContext *waappv1.RequestContext, selector *waappv1.AccountLoginSelector, kind waappv1.AccountSettingsOperationKind, enrich func(wacore.EngineAccountSettingsInput) wacore.EngineAccountSettingsInput) (*waappv1.AccountSettingsOperation, wacore.EngineAccountSettingsResult, error) {
	operationCtx, cancel := accountSettingsOperationContext(ctx)
	defer cancel()
	loginState, err := s.accountSettingsLoginState(operationCtx, selector)
	if err != nil {
		return nil, wacore.EngineAccountSettingsResult{}, err
	}
	input := wacore.EngineAccountSettingsInput{
		WAAccountID:          loginState.GetWaAccountId(),
		ClientProfileID:      loginState.GetClientProfileId(),
		RegisteredIdentityID: loginState.GetRegisteredIdentityId(),
		LoginStateID:         loginState.GetLoginStateId(),
		AppVersion:           s.loginStateAppVersion(operationCtx, loginState),
		Kind:                 kind,
	}
	if enrich != nil {
		input = enrich(input)
	}
	runner, release, err := s.accountSettingsRunner(operationCtx, requestContext, loginState, kind)
	if err != nil {
		return nil, wacore.EngineAccountSettingsResult{}, err
	}
	defer release()
	result := runner.ApplyAccountSettings(operationCtx, input)
	completedAt := s.clock.Now()
	op := &waappv1.AccountSettingsOperation{
		AccountSettingsOperationId: s.ids.NewID("waacctset_"),
		WaAccountId:                loginState.GetWaAccountId(),
		ClientProfileId:            loginState.GetClientProfileId(),
		LoginStateId:               loginState.GetLoginStateId(),
		RegisteredIdentityId:       loginState.GetRegisteredIdentityId(),
		Kind:                       kind,
		Status:                     accountSettingsStatus(kind, result),
		CompletedAt:                timestamppb.New(completedAt),
		Error:                      shared.ToProtoError(result.Err),
	}
	if result.WaitTime > 0 {
		op.WaitTime = durationpb.New(result.WaitTime)
	}
	return op, result, nil
}
