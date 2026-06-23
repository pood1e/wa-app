package app

import (
	"context"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	waappv1.UnimplementedWaDiscoveryServiceServer
	waappv1.UnimplementedWaProfileServiceServer
	waappv1.UnimplementedWaRegistrationServiceServer
	waappv1.UnimplementedWaMessagingServiceServer
	waappv1.UnimplementedWaExtractionServiceServer
	waappv1.UnimplementedWaContactServiceServer
	waappv1.UnimplementedWaToolingServiceServer
	waappv1.UnimplementedWaAccountSettingsServiceServer

	store   Store
	runtime RuntimeState
	runner  ProtocolEngine
	clock   Clock
	ids     IDGenerator

	commonProxyURL  string
	longConnections *LongConnectionManager
}

func NewServer(store Store, runtime RuntimeState, runner ProtocolEngine, clock Clock, ids IDGenerator) *Server {
	if clock == nil {
		clock = SystemClock{}
	}
	if ids == nil {
		ids = RandomIDGenerator{}
	}
	server := &Server{store: store, runtime: runtime, runner: runner, clock: clock, ids: ids}
	server.longConnections = NewLongConnectionManager(server)
	return server
}

func (s *Server) SetCommonProxyURL(common string) {
	s.commonProxyURL = strings.TrimSpace(common)
}

func (s *Server) RunLongConnections(ctx context.Context) error {
	if s == nil || s.longConnections == nil {
		return nil
	}
	return s.longConnections.Run(ctx)
}

func (s *Server) RegisterAppArtifact(ctx context.Context, req *waappv1.RegisterAppArtifactRequest) (*waappv1.RegisterAppArtifactResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.RegisterAppArtifactResponse{Error: ToProtoError(err)}, nil
	}
	if strings.TrimSpace(req.GetLabel()) == "" {
		return &waappv1.RegisterAppArtifactResponse{Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "artifact label is required", false))}, nil
	}
	now := s.clock.Now()
	artifact := &waappv1.AppArtifact{ArtifactId: s.ids.NewID("waart_"), Label: req.GetLabel(), VersionLabel: req.GetVersionLabel(), Sha256: req.GetSha256(), ObservedAt: timestamppb.New(now)}
	if err := s.store.SaveAppArtifact(ctx, artifact); err != nil {
		return &waappv1.RegisterAppArtifactResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.RegisterAppArtifactResponse{Artifact: artifact}, nil
}

func (s *Server) RecordProtocolProfile(ctx context.Context, req *waappv1.RecordProtocolProfileRequest) (*waappv1.RecordProtocolProfileResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.RecordProtocolProfileResponse{Error: ToProtoError(err)}, nil
	}
	if _, err := s.store.GetAppArtifact(ctx, req.GetAppArtifactId()); err != nil {
		return &waappv1.RecordProtocolProfileResponse{Error: ToProtoError(err)}, nil
	}
	now := s.clock.Now()
	profile := &waappv1.ProtocolProfile{
		ProtocolProfileId: s.ids.NewID("waproto_"),
		AppArtifactId:     req.GetAppArtifactId(),
		DisplayName:       firstNonEmpty(req.GetDisplayName(), "WA protocol profile"),
		AppVersion:        nativeAppVersion(req.GetAppVersion()),
		Status:            waappv1.ProtocolProfileStatus_PROTOCOL_PROFILE_STATUS_ACTIVE,
		Capabilities:      req.GetCapabilities(),
		RegistrationFlows: req.GetRegistrationFlows(),
		MessageTransports: req.GetMessageTransports(),
		DiscoveredAt:      timestamppb.New(now),
		Audit:             &waappv1.AuditStamp{CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)},
	}
	if err := s.store.SaveProtocolProfile(ctx, profile); err != nil {
		return &waappv1.RecordProtocolProfileResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.RecordProtocolProfileResponse{ProtocolProfile: profile}, nil
}

func (s *Server) GetProtocolProfile(ctx context.Context, req *waappv1.GetProtocolProfileRequest) (*waappv1.GetProtocolProfileResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.GetProtocolProfileResponse{Error: ToProtoError(err)}, nil
	}
	profile, err := s.store.GetProtocolProfile(ctx, req.GetProtocolProfileId())
	if err != nil {
		return &waappv1.GetProtocolProfileResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.GetProtocolProfileResponse{ProtocolProfile: profile}, nil
}

func (s *Server) CreateWAAccount(ctx context.Context, req *waappv1.CreateWAAccountRequest) (*waappv1.CreateWAAccountResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.CreateWAAccountResponse{Error: ToProtoError(err)}, nil
	}
	phone := normalizePhone(req.GetPhone())
	if phone.GetE164Number() == "" {
		return &waappv1.CreateWAAccountResponse{Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "phone is required", false))}, nil
	}
	if existing, err := s.store.FindWAAccountByPhone(ctx, phone.GetE164Number()); err == nil {
		return &waappv1.CreateWAAccountResponse{Account: existing}, nil
	}
	now := s.clock.Now()
	account := newWAAccount(s.ids.NewID("waacc_"), "", phone, waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_PENDING_REGISTRATION, &waappv1.AuditStamp{CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)})
	account, err := s.saveWAAccount(ctx, account)
	if err != nil {
		return &waappv1.CreateWAAccountResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.CreateWAAccountResponse{Account: account}, nil
}

func (s *Server) GetWAAccount(ctx context.Context, req *waappv1.GetWAAccountRequest) (*waappv1.GetWAAccountResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.GetWAAccountResponse{Error: ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.GetWAAccountResponse{Error: ToProtoError(err)}, nil
	}
	account, err := s.getWAAccount(ctx, accountID)
	if err != nil {
		return &waappv1.GetWAAccountResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.GetWAAccountResponse{Account: account}, nil
}

func (s *Server) ListWAAccounts(ctx context.Context, req *waappv1.ListWAAccountsRequest) (*waappv1.ListWAAccountsResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.ListWAAccountsResponse{Error: ToProtoError(err)}, nil
	}
	accounts, nextCursor, err := s.listWAAccounts(ctx, req.GetCursor(), int(req.GetLimit()))
	if err != nil {
		return &waappv1.ListWAAccountsResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.ListWAAccountsResponse{Accounts: accounts, NextCursor: nextCursor}, nil
}

func (s *Server) DeleteWAAccount(ctx context.Context, req *waappv1.DeleteWAAccountRequest) (*waappv1.DeleteWAAccountResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.DeleteWAAccountResponse{Error: ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.DeleteWAAccountResponse{Error: ToProtoError(err)}, nil
	}
	found, err := s.deleteWAAccount(ctx, accountID)
	if err != nil {
		return &waappv1.DeleteWAAccountResponse{Error: ToProtoError(err)}, nil
	}
	if !found {
		return &waappv1.DeleteWAAccountResponse{Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, "WA account not found", false))}, nil
	}
	return &waappv1.DeleteWAAccountResponse{Success: true}, nil
}

func (s *Server) PrepareClientProfile(ctx context.Context, req *waappv1.PrepareClientProfileRequest) (*waappv1.PrepareClientProfileResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.PrepareClientProfileResponse{Error: ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.PrepareClientProfileResponse{Error: ToProtoError(err)}, nil
	}
	account, err := s.getWAAccount(ctx, accountID)
	if err != nil {
		return &waappv1.PrepareClientProfileResponse{Error: ToProtoError(err)}, nil
	}
	protocol, err := s.store.GetProtocolProfile(ctx, req.GetProtocolProfileId())
	if err != nil {
		return &waappv1.PrepareClientProfileResponse{Error: ToProtoError(err)}, nil
	}
	now := s.clock.Now()
	profile := &waappv1.ClientProfile{ClientProfileId: s.ids.NewID("wacp_"), WaAccountId: waAccountID(account), ProtocolProfileId: req.GetProtocolProfileId(), Status: waappv1.ClientProfileStatus_CLIENT_PROFILE_STATUS_PREPARING, RegistrationKeyState: waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_PENDING, MessagingKeyState: waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_PENDING, Audit: &waappv1.AuditStamp{CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}}
	if err := s.store.SaveClientProfile(ctx, profile); err != nil {
		return &waappv1.PrepareClientProfileResponse{Error: ToProtoError(err)}, nil
	}
	runErr := s.runner.PrepareClientProfile(ctx, EngineProfileInput{WAAccountID: waAccountID(account), ClientProfileID: profile.GetClientProfileId(), ProtocolProfileID: req.GetProtocolProfileId(), AppVersion: protocolAppVersion(protocol), Phone: account.GetPhone()})
	profile.Audit.UpdatedAt = timestamppb.New(s.clock.Now())
	if runErr != nil {
		profile.Status = waappv1.ClientProfileStatus_CLIENT_PROFILE_STATUS_REJECTED
		profile.RegistrationKeyState = waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_INVALID
		profile.MessagingKeyState = waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_INVALID
		profile.LastError = ToProtoError(runErr)
	} else {
		profile.Status = waappv1.ClientProfileStatus_CLIENT_PROFILE_STATUS_READY
		profile.RegistrationKeyState = waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_READY
		profile.MessagingKeyState = waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_READY
	}
	if err := s.store.SaveClientProfile(ctx, profile); err != nil {
		return &waappv1.PrepareClientProfileResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.PrepareClientProfileResponse{ClientProfile: profile, Error: profile.GetLastError()}, nil
}

func (s *Server) GetClientProfile(ctx context.Context, req *waappv1.GetClientProfileRequest) (*waappv1.GetClientProfileResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.GetClientProfileResponse{Error: ToProtoError(err)}, nil
	}
	profile, err := s.store.GetClientProfile(ctx, req.GetClientProfileId())
	if err != nil {
		return &waappv1.GetClientProfileResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.GetClientProfileResponse{ClientProfile: s.attachClientProfileRuntime(ctx, profile)}, nil
}

func (s *Server) ListClientProfiles(ctx context.Context, req *waappv1.ListClientProfilesRequest) (*waappv1.ListClientProfilesResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.ListClientProfilesResponse{Error: ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.ListClientProfilesResponse{Error: ToProtoError(err)}, nil
	}
	if _, err := s.getWAAccount(ctx, accountID); err != nil {
		return &waappv1.ListClientProfilesResponse{Error: ToProtoError(err)}, nil
	}
	profiles, nextCursor, err := s.store.ListClientProfiles(ctx, accountID, req.GetCursor(), int(req.GetLimit()))
	if err != nil {
		return &waappv1.ListClientProfilesResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.ListClientProfilesResponse{ClientProfiles: s.attachClientProfilesRuntime(ctx, profiles), NextCursor: nextCursor}, nil
}

func (s *Server) RetireClientProfile(ctx context.Context, req *waappv1.RetireClientProfileRequest) (*waappv1.RetireClientProfileResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.RetireClientProfileResponse{Error: ToProtoError(err)}, nil
	}
	profile, err := s.store.GetClientProfile(ctx, req.GetClientProfileId())
	if err != nil {
		return &waappv1.RetireClientProfileResponse{Error: ToProtoError(err)}, nil
	}
	profile.Status = waappv1.ClientProfileStatus_CLIENT_PROFILE_STATUS_RETIRED
	profile.Audit.UpdatedAt = timestamppb.New(s.clock.Now())
	if err := s.store.SaveClientProfile(ctx, profile); err != nil {
		return &waappv1.RetireClientProfileResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.RetireClientProfileResponse{ClientProfile: profile}, nil
}

func normalizePhone(phone *waappv1.PhoneTarget) *waappv1.PhoneTarget {
	if phone == nil {
		return &waappv1.PhoneTarget{}
	}
	cc := strings.TrimPrefix(strings.TrimSpace(phone.GetCountryCallingCode()), "+")
	national := strings.TrimSpace(phone.GetNationalNumber())
	e164 := strings.TrimSpace(phone.GetE164Number())
	if e164 == "" && cc != "" && national != "" {
		e164 = "+" + cc + national
	}
	if e164 != "" && !strings.HasPrefix(e164, "+") {
		e164 = "+" + e164
	}
	return &waappv1.PhoneTarget{E164Number: e164, CountryCallingCode: cc, NationalNumber: national, CountryIso2: strings.ToUpper(strings.TrimSpace(phone.GetCountryIso2()))}
}

func protocolAppVersion(profile *waappv1.ProtocolProfile) string {
	if profile == nil {
		return defaultWAAppVersion
	}
	return nativeAppVersion(profile.GetAppVersion())
}

func (s *Server) clientProfileAppVersion(ctx context.Context, profile *waappv1.ClientProfile) string {
	if s == nil || profile == nil {
		return defaultWAAppVersion
	}
	protocol, err := s.store.GetProtocolProfile(ctx, profile.GetProtocolProfileId())
	if err != nil {
		return defaultWAAppVersion
	}
	if protocol.GetProtocolProfileId() == "waproto_native" && nativeAppVersion(protocol.GetAppVersion()) != defaultWAAppVersion {
		protocol.AppVersion = defaultWAAppVersion
		_ = s.store.SaveProtocolProfile(ctx, protocol)
	}
	return protocolAppVersion(protocol)
}

func (s *Server) protocolIDAppVersion(ctx context.Context, protocolProfileID string) string {
	if s == nil || strings.TrimSpace(protocolProfileID) == "" {
		return defaultWAAppVersion
	}
	protocol, err := s.store.GetProtocolProfile(ctx, protocolProfileID)
	if err != nil {
		return defaultWAAppVersion
	}
	if protocol.GetProtocolProfileId() == "waproto_native" && nativeAppVersion(protocol.GetAppVersion()) != defaultWAAppVersion {
		protocol.AppVersion = defaultWAAppVersion
		_ = s.store.SaveProtocolProfile(ctx, protocol)
	}
	return protocolAppVersion(protocol)
}

func (s *Server) loginStateAppVersion(ctx context.Context, loginState *waappv1.LoginState) string {
	if s == nil || loginState == nil {
		return defaultWAAppVersion
	}
	profile, err := s.store.GetClientProfile(ctx, loginState.GetClientProfileId())
	if err != nil {
		return defaultWAAppVersion
	}
	return s.clientProfileAppVersion(ctx, profile)
}

func protoDurationSeconds(d interface{ GetSeconds() int64 }) time.Duration {
	if d == nil {
		return 0
	}
	return time.Duration(d.GetSeconds()) * time.Second
}
