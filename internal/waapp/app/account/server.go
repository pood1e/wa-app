package account

import (
	"context"
	"strings"

	"golang.org/x/sync/semaphore"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	protocolstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	protocolwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/session"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

// core holds dependencies shared by account use cases.
type serverCore struct {
	lifecycle      *semaphore.Weighted
	pictureLookups *semaphore.Weighted
	store          Repository
	runtime        EphemeralState
	runner         wacore.Communication
	clock          shared.Clock
	ids            shared.IDGenerator

	factory         SessionFactory
	longConnections *session.Manager
}

// Each service groups the operations of one application capability.
type discoveryHandler struct {
	*serverCore
}
type profileHandler struct {
	*serverCore
}
type loginHandler struct {
	*serverCore
}
type messagingHandler struct {
	*serverCore
}
type extractionHandler struct {
	*serverCore
}
type contactHandler struct {
	*serverCore
}

type accountSettingsHandler struct {
	*serverCore
}

// Application composes account and messaging use cases.
type Application struct {
	*serverCore
	*discoveryHandler
	*profileHandler
	*loginHandler
	*messagingHandler
	*extractionHandler
	*contactHandler

	*accountSettingsHandler
	*accountReviewHandler
	*groupHandler
}

func newApplication(core *serverCore) *Application {
	server := &Application{
		serverCore:             core,
		discoveryHandler:       &discoveryHandler{serverCore: core},
		profileHandler:         &profileHandler{serverCore: core},
		loginHandler:           &loginHandler{serverCore: core},
		messagingHandler:       &messagingHandler{serverCore: core},
		extractionHandler:      &extractionHandler{serverCore: core},
		contactHandler:         &contactHandler{serverCore: core},
		accountSettingsHandler: &accountSettingsHandler{serverCore: core},
		accountReviewHandler:   &accountReviewHandler{serverCore: core},
		groupHandler:           &groupHandler{serverCore: core},
	}
	return server
}

func New(store Repository, runtime EphemeralState, runner wacore.Communication, factory SessionFactory, clock shared.Clock, ids shared.IDGenerator) *Application {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	if ids == nil {
		ids = shared.RandomIDGenerator{}
	}
	server := newApplication(&serverCore{lifecycle: semaphore.NewWeighted(1), pictureLookups: semaphore.NewWeighted(2), store: store, runtime: runtime, runner: runner, clock: clock, ids: ids, factory: factory})
	server.longConnections = session.New(server, store, runtime, clock, ids)
	return server
}

func (s *serverCore) RunLongConnections(ctx context.Context) error {
	if s == nil || s.longConnections == nil {
		return nil
	}
	return s.longConnections.Run(ctx)
}

func (s *discoveryHandler) RegisterAppArtifact(ctx context.Context, req *waappv1.RegisterAppArtifactRequest) (*waappv1.RegisterAppArtifactResponse, error) {

	if strings.TrimSpace(req.GetLabel()) == "" {
		return &waappv1.RegisterAppArtifactResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "artifact label is required", false))}, nil
	}
	now := s.clock.Now()
	artifact := &waappv1.AppArtifact{ArtifactId: s.ids.NewID("waart_"), Label: req.GetLabel(), VersionLabel: req.GetVersionLabel(), Sha256: req.GetSha256(), ObservedAt: timestamppb.New(now)}
	if err := s.store.SaveAppArtifact(ctx, artifact); err != nil {
		return &waappv1.RegisterAppArtifactResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.RegisterAppArtifactResponse{Artifact: artifact}, nil
}

func (s *discoveryHandler) RecordProtocolProfile(ctx context.Context, req *waappv1.RecordProtocolProfileRequest) (*waappv1.RecordProtocolProfileResponse, error) {

	if _, err := s.store.GetAppArtifact(ctx, req.GetAppArtifactId()); err != nil {
		return &waappv1.RecordProtocolProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	now := s.clock.Now()
	profile := &waappv1.ProtocolProfile{
		ProtocolProfileId: s.ids.NewID("waproto_"),
		AppArtifactId:     req.GetAppArtifactId(),
		DisplayName:       shared.FirstNonEmpty(req.GetDisplayName(), "WA protocol profile"),
		AppVersion:        protocolstate.NativeAppVersion(req.GetAppVersion()),
		Status:            waappv1.ProtocolProfileStatus_PROTOCOL_PROFILE_STATUS_ACTIVE,
		Capabilities:      req.GetCapabilities(),
		RegistrationFlows: req.GetRegistrationFlows(),
		MessageTransports: req.GetMessageTransports(),
		DiscoveredAt:      timestamppb.New(now),
		Audit:             &waappv1.AuditStamp{CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)},
	}
	if err := s.store.SaveProtocolProfile(ctx, profile); err != nil {
		return &waappv1.RecordProtocolProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.RecordProtocolProfileResponse{ProtocolProfile: profile}, nil
}

func (s *discoveryHandler) GetProtocolProfile(ctx context.Context, req *waappv1.GetProtocolProfileRequest) (*waappv1.GetProtocolProfileResponse, error) {

	profile, err := s.store.GetProtocolProfile(ctx, req.GetProtocolProfileId())
	if err != nil {
		return &waappv1.GetProtocolProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.GetProtocolProfileResponse{ProtocolProfile: profile}, nil
}

func (s *profileHandler) GetWAAccount(ctx context.Context, req *waappv1.GetWAAccountRequest) (*waappv1.GetWAAccountResponse, error) {

	accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.GetWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	account, err := s.GetWAAccountRecord(ctx, accountID)
	if err != nil {
		return &waappv1.GetWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.GetWAAccountResponse{Account: account}, nil
}

func (s *profileHandler) ListWAAccounts(ctx context.Context, req *waappv1.ListWAAccountsRequest) (*waappv1.ListWAAccountsResponse, error) {

	accounts, nextCursor, err := s.listWAAccounts(ctx, req.GetCursor(), int(req.GetLimit()))
	if err != nil {
		return &waappv1.ListWAAccountsResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.ListWAAccountsResponse{Accounts: accounts, NextCursor: nextCursor}, nil
}

func (s *profileHandler) DeleteWAAccount(ctx context.Context, req *waappv1.DeleteWAAccountRequest) (*waappv1.DeleteWAAccountResponse, error) {

	accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.DeleteWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	found, err := s.deleteWAAccount(ctx, accountID)
	if err != nil {
		return &waappv1.DeleteWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	if !found {
		return &waappv1.DeleteWAAccountResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, "WA account not found", false))}, nil
	}
	return &waappv1.DeleteWAAccountResponse{Success: true}, nil
}

func protocolAppVersion(profile *waappv1.ProtocolProfile) string {
	if profile == nil {
		return protocolwire.DefaultWAAppVersion
	}
	return strings.TrimSpace(profile.GetAppVersion())
}

func (s *serverCore) clientProfileAppVersion(ctx context.Context, profile *waappv1.ClientProfile) string {
	if s == nil || profile == nil {
		return protocolwire.DefaultWAAppVersion
	}
	protocol, err := s.store.GetProtocolProfile(ctx, profile.GetProtocolProfileId())
	if err != nil {
		return protocolwire.DefaultWAAppVersion
	}
	return protocolAppVersion(protocol)
}

func (s *serverCore) protocolIDAppVersion(ctx context.Context, protocolProfileID string) string {
	if s == nil || strings.TrimSpace(protocolProfileID) == "" {
		return protocolwire.DefaultWAAppVersion
	}
	protocol, err := s.store.GetProtocolProfile(ctx, protocolProfileID)
	if err != nil {
		return protocolwire.DefaultWAAppVersion
	}
	return protocolAppVersion(protocol)
}

func (s *serverCore) loginStateAppVersion(ctx context.Context, loginState *waappv1.LoginState) string {
	if s == nil || loginState == nil {
		return protocolwire.DefaultWAAppVersion
	}
	profile, err := s.store.GetClientProfile(ctx, loginState.GetClientProfileId())
	if err != nil {
		return protocolwire.DefaultWAAppVersion
	}
	return s.clientProfileAppVersion(ctx, profile)
}
