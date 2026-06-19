package app

import (
	"context"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) ProbeAccount(ctx context.Context, req *waappv1.ProbeAccountRequest) (*waappv1.ProbeAccountResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.ProbeAccountResponse{Error: ToProtoError(err)}, nil
	}
	account, profile, err := s.waAccountAndProfile(ctx, req.GetWaAccountId(), req.GetClientProfileId())
	if err != nil {
		return &waappv1.ProbeAccountResponse{Error: ToProtoError(err)}, nil
	}
	result := s.runner.ProbeAccount(ctx, EngineRegistrationInput{WAAccountID: waAccountID(account), ClientProfileID: profile.GetClientProfileId(), ProtocolProfileID: profile.GetProtocolProfileId(), AppVersion: s.clientProfileAppVersion(ctx, profile), Phone: account.GetPhone(), DeliveryMethod: waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS})
	now := s.clock.Now()
	probe := &waappv1.AccountProbe{AccountProbeId: s.ids.NewID("waprobe_"), WaAccountId: waAccountID(account), ClientProfileId: profile.GetClientProfileId(), Status: result.Status, SupportedMethods: result.SupportedMethods, ProbedAt: timestamppb.New(now), LastError: ToProtoError(result.Err), MethodStatuses: protoVerificationMethodStatuses(result.MethodStatuses)}
	if err := s.store.SaveAccountProbe(ctx, probe); err != nil {
		return &waappv1.ProbeAccountResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.ProbeAccountResponse{Probe: probe, Error: probe.GetLastError()}, nil
}

func (s *Server) RequestVerificationCode(ctx context.Context, req *waappv1.RequestVerificationCodeRequest) (*waappv1.RequestVerificationCodeResponse, error) {
	return s.requestVerificationCode(ctx, req, s.runner)
}

func (s *Server) requestVerificationCode(ctx context.Context, req *waappv1.RequestVerificationCodeRequest, runner ProtocolEngine) (*waappv1.RequestVerificationCodeResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.RequestVerificationCodeResponse{Error: ToProtoError(err)}, nil
	}
	account, profile, err := s.waAccountAndProfile(ctx, req.GetWaAccountId(), req.GetClientProfileId())
	if err != nil {
		return &waappv1.RequestVerificationCodeResponse{Error: ToProtoError(err)}, nil
	}
	method := req.GetDeliveryMethod()
	if method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
		method = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS
	}
	result := runner.RequestVerificationCode(ctx, EngineRegistrationInput{WAAccountID: waAccountID(account), ClientProfileID: profile.GetClientProfileId(), ProtocolProfileID: profile.GetProtocolProfileId(), AppVersion: s.clientProfileAppVersion(ctx, profile), Phone: account.GetPhone(), DeliveryMethod: method})
	record := s.newVerificationCodeRequestRecord(account, profile, method, result)
	challenge := result.AccountTransferChallenge
	if challenge != nil {
		challenge.VerificationRequestId = record.GetVerificationRequestId()
	}
	if err := s.store.SaveVerificationRequest(ctx, record); err != nil {
		return &waappv1.RequestVerificationCodeResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.RequestVerificationCodeResponse{VerificationRequest: record, AccountTransferChallenge: challenge, Error: record.GetLastError()}, nil
}

func (s *Server) RefreshAccountTransferChallenge(ctx context.Context, req *waappv1.RefreshAccountTransferChallengeRequest) (*waappv1.RefreshAccountTransferChallengeResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.RefreshAccountTransferChallengeResponse{Error: ToProtoError(err)}, nil
	}
	verification, err := s.store.GetVerificationRequest(ctx, req.GetVerificationRequestId())
	if err != nil {
		return &waappv1.RefreshAccountTransferChallengeResponse{Error: ToProtoError(err)}, nil
	}
	if verification.GetDeliveryMethod() != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
		return &waappv1.RefreshAccountTransferChallengeResponse{Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "verification request is not account transfer", false))}, nil
	}
	account, profile, err := s.waAccountAndProfile(ctx, verification.GetWaAccountId(), verification.GetClientProfileId())
	if err != nil {
		return &waappv1.RefreshAccountTransferChallengeResponse{Error: ToProtoError(err)}, nil
	}
	result := s.runner.RefreshAccountTransferChallenge(ctx, EngineAccountTransferChallengeInput{
		EngineRegistrationInput: EngineRegistrationInput{
			WAAccountID:       waAccountID(account),
			ClientProfileID:   profile.GetClientProfileId(),
			ProtocolProfileID: profile.GetProtocolProfileId(),
			AppVersion:        s.clientProfileAppVersion(ctx, profile),
			Phone:             account.GetPhone(),
			DeliveryMethod:    verification.GetDeliveryMethod(),
		},
		VerificationRequestID: verification.GetVerificationRequestId(),
	})
	return &waappv1.RefreshAccountTransferChallengeResponse{AccountTransferChallenge: result.Challenge, Error: ToProtoError(result.Err)}, nil
}

func (s *Server) SubmitVerificationCode(ctx context.Context, req *waappv1.SubmitVerificationCodeRequest) (*waappv1.SubmitVerificationCodeResponse, error) {
	return s.submitVerificationCode(ctx, req, s.runner)
}

func (s *Server) submitVerificationCode(ctx context.Context, req *waappv1.SubmitVerificationCodeRequest, runner ProtocolEngine) (*waappv1.SubmitVerificationCodeResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.SubmitVerificationCodeResponse{Error: ToProtoError(err)}, nil
	}
	verification, err := s.store.GetVerificationRequest(ctx, req.GetVerificationRequestId())
	if err != nil {
		return &waappv1.SubmitVerificationCodeResponse{Error: ToProtoError(err)}, nil
	}
	account, profile, err := s.waAccountAndProfile(ctx, verification.GetWaAccountId(), verification.GetClientProfileId())
	if err != nil {
		return &waappv1.SubmitVerificationCodeResponse{Error: ToProtoError(err)}, nil
	}
	now := s.clock.Now()
	registration := &waappv1.RegistrationRecord{RegistrationId: s.ids.NewID("wareg_"), VerificationRequestId: verification.GetVerificationRequestId(), WaAccountId: waAccountID(account), ClientProfileId: profile.GetClientProfileId(), Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_SUBMITTED, SubmittedAt: timestamppb.New(now)}
	result := runner.SubmitVerificationCode(ctx, EngineSubmitInput{EngineRegistrationInput: EngineRegistrationInput{WAAccountID: waAccountID(account), ClientProfileID: profile.GetClientProfileId(), ProtocolProfileID: profile.GetProtocolProfileId(), AppVersion: s.clientProfileAppVersion(ctx, profile), Phone: account.GetPhone(), DeliveryMethod: verification.GetDeliveryMethod()}, VerificationRequestID: verification.GetVerificationRequestId(), Code: req.GetCode(), CodeSecretRef: req.GetCodeSecretRef()})
	registration.Status = result.Status
	registration.LastError = ToProtoError(result.Err)
	if result.Status == waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED {
		completedAt := result.CompletedAt
		if completedAt.IsZero() {
			completedAt = s.clock.Now()
		}
		registration.CompletedAt = timestamppb.New(completedAt)
		registration.Identity = &waappv1.RegisteredIdentity{RegisteredIdentityId: firstNonEmpty(result.RegisteredID, s.ids.NewID("waid_")), WaAccountId: waAccountID(account), ClientProfileId: profile.GetClientProfileId(), ServiceAccountId: result.ServiceAccountID, ServiceLoginId: result.ServiceLoginID, RegisteredAt: timestamppb.New(completedAt)}
	}
	if err := s.store.SaveRegistration(ctx, registration); err != nil {
		return &waappv1.SubmitVerificationCodeResponse{Error: ToProtoError(err)}, nil
	}
	if registration.GetStatus() == waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED {
		if _, err := s.saveWAAccount(ctx, withWAAccountStatus(account, waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_ACTIVE, s.clock.Now())); err != nil {
			return &waappv1.SubmitVerificationCodeResponse{Registration: registration, Error: ToProtoError(err)}, nil
		}
	}
	loginState, err := s.loginStateFromRegistration(registration)
	if err != nil {
		return &waappv1.SubmitVerificationCodeResponse{Registration: registration, Error: ToProtoError(err)}, nil
	}
	if loginState != nil {
		if err := s.store.SaveLoginState(ctx, loginState, "native-db:"+profile.GetClientProfileId()); err != nil {
			return &waappv1.SubmitVerificationCodeResponse{Registration: registration, Error: ToProtoError(err)}, nil
		}
		s.ensureLongConnection(ctx, loginState)
	}
	return &waappv1.SubmitVerificationCodeResponse{Registration: registration, LoginState: loginState, Error: registration.GetLastError()}, nil
}

func (s *Server) GetRegistration(ctx context.Context, req *waappv1.GetRegistrationRequest) (*waappv1.GetRegistrationResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.GetRegistrationResponse{Error: ToProtoError(err)}, nil
	}
	registration, err := s.store.GetRegistration(ctx, req.GetRegistrationId())
	if err != nil {
		return &waappv1.GetRegistrationResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.GetRegistrationResponse{Registration: registration}, nil
}

func (s *Server) GetLoginState(ctx context.Context, req *waappv1.GetLoginStateRequest) (*waappv1.GetLoginStateResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.GetLoginStateResponse{Error: ToProtoError(err)}, nil
	}
	loginState, err := s.store.GetLoginState(ctx, req.GetLoginStateId())
	if err != nil {
		return &waappv1.GetLoginStateResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.GetLoginStateResponse{LoginState: loginState}, nil
}

func (s *Server) GetActiveLoginState(ctx context.Context, req *waappv1.GetActiveLoginStateRequest) (*waappv1.GetActiveLoginStateResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.GetActiveLoginStateResponse{Error: ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.GetActiveLoginStateResponse{Error: ToProtoError(err)}, nil
	}
	loginState, err := s.store.GetActiveLoginState(ctx, accountID, req.GetClientProfileId())
	if err != nil {
		return &waappv1.GetActiveLoginStateResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.GetActiveLoginStateResponse{LoginState: loginState}, nil
}

func (s *Server) CheckLoginState(ctx context.Context, req *waappv1.CheckLoginStateRequest) (*waappv1.CheckLoginStateResponse, error) {
	return s.checkLoginState(ctx, req, s.runner)
}

func (s *Server) checkLoginState(ctx context.Context, req *waappv1.CheckLoginStateRequest, runner ProtocolEngine) (*waappv1.CheckLoginStateResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.CheckLoginStateResponse{Error: ToProtoError(err)}, nil
	}
	loginState, err := s.loginStateForCheck(ctx, req)
	if err != nil {
		return &waappv1.CheckLoginStateResponse{Error: ToProtoError(err)}, nil
	}
	result := EngineLoginCheckResult{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE}
	if s.longConnections == nil || s.longConnections.ActiveRunner(loginState) == nil {
		result = runner.CheckLoginState(ctx, EngineLoginCheckInput{WAAccountID: loginState.GetWaAccountId(), ClientProfileID: loginState.GetClientProfileId(), RegisteredIdentityID: loginState.GetRegisteredIdentityId(), AppVersion: s.loginStateAppVersion(ctx, loginState), RemoteTimeout: durationFromProto(req.GetRemoteTimeout())})
	}
	now := s.clock.Now()
	check := &waappv1.LoginStateCheck{
		LoginStateCheckId:    s.ids.NewID("walogchk_"),
		LoginStateId:         loginState.GetLoginStateId(),
		WaAccountId:          loginState.GetWaAccountId(),
		ClientProfileId:      loginState.GetClientProfileId(),
		RegisteredIdentityId: loginState.GetRegisteredIdentityId(),
		Status:               result.Status,
		CheckedAt:            timestamppb.New(now),
		Error:                ToProtoError(result.Err),
	}
	if check.GetStatus() == waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNSPECIFIED && result.Err == nil {
		check.Status = waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE
	}
	if s.applyLoginStateCheck(loginState, check, now) {
		if err := s.store.SaveLoginState(ctx, loginState, "native-db:"+loginState.GetClientProfileId()); err != nil {
			return &waappv1.CheckLoginStateResponse{LoginState: loginState, Check: check, Error: ToProtoError(err)}, nil
		}
	}
	if check.GetStatus() == waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE && loginState.GetStatus() == waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
		s.ensureLongConnection(ctx, loginState)
	}
	return &waappv1.CheckLoginStateResponse{LoginState: loginState, Check: check, Error: check.GetError()}, nil
}

func (s *Server) loginStateForCheck(ctx context.Context, req *waappv1.CheckLoginStateRequest) (*waappv1.LoginState, error) {
	if req.GetLoginStateId() != "" {
		return s.store.GetLoginState(ctx, req.GetLoginStateId())
	}
	if req.GetRegisteredIdentityId() != "" {
		return s.store.GetLoginStateByRegisteredIdentity(ctx, req.GetRegisteredIdentityId())
	}
	if req.GetWaAccountId() != "" && req.GetClientProfileId() != "" {
		accountID, err := requireWAAccountID(req.GetWaAccountId())
		if err != nil {
			return nil, err
		}
		return s.store.GetActiveLoginState(ctx, accountID, req.GetClientProfileId())
	}
	return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "login_state_id, registered_identity_id, or wa_account_id/client_profile_id is required", false)
}

func (s *Server) applyLoginStateCheck(loginState *waappv1.LoginState, check *waappv1.LoginStateCheck, now time.Time) bool {
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

func (s *Server) loginStateFromRegistration(registration *waappv1.RegistrationRecord) (*waappv1.LoginState, error) {
	if registration.GetStatus() != waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED {
		return nil, nil
	}
	identity := registration.GetIdentity()
	if identity.GetRegisteredIdentityId() == "" {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "registered identity is required for active login state", false)
	}
	registeredAt := identity.GetRegisteredAt()
	if registeredAt == nil {
		registeredAt = registration.GetCompletedAt()
	}
	if registeredAt == nil {
		registeredAt = timestamppb.New(s.clock.Now())
	}
	now := s.clock.Now()
	return &waappv1.LoginState{
		LoginStateId:         "walogin_" + stableID(identity.GetRegisteredIdentityId()),
		RegistrationId:       registration.GetRegistrationId(),
		WaAccountId:          registration.GetWaAccountId(),
		ClientProfileId:      registration.GetClientProfileId(),
		RegisteredIdentityId: identity.GetRegisteredIdentityId(),
		ServiceAccountId:     identity.GetServiceAccountId(),
		ServiceLoginId:       identity.GetServiceLoginId(),
		Status:               waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE,
		RegisteredAt:         registeredAt,
		LastVerifiedAt:       registeredAt,
		Audit:                &waappv1.AuditStamp{CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)},
	}, nil
}

func (s *Server) waAccountAndProfile(ctx context.Context, waAccountIDValue string, clientProfileID string) (*waappv1.WAAccount, *waappv1.ClientProfile, error) {
	accountID, err := requireWAAccountID(waAccountIDValue)
	if err != nil {
		return nil, nil, err
	}
	account, err := s.getWAAccount(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	profile, err := s.store.GetClientProfile(ctx, clientProfileID)
	if err != nil {
		return nil, nil, err
	}
	if profile.GetWaAccountId() != waAccountID(account) {
		return nil, nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "client profile does not belong to WA account", false)
	}
	return account, profile, nil
}

func defaultExpiry(now time.Time, expiresAt *timestamppb.Timestamp) *timestamppb.Timestamp {
	if expiresAt != nil {
		return expiresAt
	}
	return timestamppb.New(now.Add(10 * time.Minute))
}

func (s *Server) newVerificationCodeRequestRecord(account *waappv1.WAAccount, profile *waappv1.ClientProfile, method waappv1.VerificationDeliveryMethod, result EngineCodeResult) *waappv1.VerificationCodeRequestRecord {
	now := s.clock.Now()
	return &waappv1.VerificationCodeRequestRecord{
		VerificationRequestId: s.ids.NewID("wavrf_"),
		WaAccountId:           waAccountID(account),
		ClientProfileId:       profile.GetClientProfileId(),
		DeliveryMethod:        method,
		Status:                result.Status,
		ExpectedCodeLength:    result.ExpectedCodeLength,
		RequestedAt:           timestamppb.New(now),
		ExpiresAt:             defaultExpiry(now, timestamp(result.ExpiresAt)),
		LastError:             ToProtoError(result.Err),
		RetryAfter:            durationToProto(result.RetryAfter),
		MethodStatuses:        protoVerificationMethodStatuses(result.MethodStatuses),
	}
}

func protoVerificationMethodStatuses(statuses []VerificationMethodStatus) []*waappv1.VerificationMethodStatus {
	out := make([]*waappv1.VerificationMethodStatus, 0, len(statuses))
	for _, status := range statuses {
		if status.Method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
			continue
		}
		out = append(out, &waappv1.VerificationMethodStatus{
			DeliveryMethod: status.Method,
			Available:      status.Available,
			Cooldown:       durationFromSeconds(status.CooldownSeconds),
		})
	}
	return out
}
