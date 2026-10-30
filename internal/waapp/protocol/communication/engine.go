package communication

import (
	"context"
	"errors"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type StateRepository interface {
	SaveProtocolState(context.Context, string, *protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState) error
	GetProtocolState(context.Context, string) (*protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState, error)
	ListRetiredProtocolProfiles(context.Context, string) ([]string, error)
}

type engineCore struct {
	stateStore     StateRepository
	activeProxyURL string
	http           *wire.NativeHTTPClient
	clock          shared.Clock
	ids            shared.IDGenerator
	proxyResolver  ProxyResolver
}

type loginService struct{ *engineCore }

type Engine struct {
	*engineCore
	*loginService
	*messagingService
	*contactsService
	*accountSettingsService
}

func New(repository StateRepository, proxyURL string, clock shared.Clock, ids shared.IDGenerator, options ...Option) (*Engine, error) {
	if repository == nil {
		return nil, errors.New("protocol state repository is required")
	}
	if clock == nil {
		clock = shared.SystemClock{}
	}
	if ids == nil {
		ids = shared.RandomIDGenerator{}
	}
	proxyURL, err := wire.NormalizeProxyURLString(proxyURL)
	if err != nil {
		return nil, err
	}
	client, err := wire.NewNativeHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	core := &engineCore{stateStore: repository, activeProxyURL: proxyURL, http: client, clock: clock, ids: ids}
	for _, option := range options {
		option(core)
	}
	return &Engine{core, &loginService{core}, &messagingService{core}, &contactsService{core}, &accountSettingsService{core}}, nil
}

func (e *engineCore) CloseIdleConnections() { e.http.CloseIdleConnections() }

func (e *engineCore) loadState(ctx context.Context, id string) (state.NativeState, error) {
	credentials, recovery, err := e.stateStore.GetProtocolState(ctx, id)
	if err != nil {
		return state.NativeState{}, err
	}
	return state.Restore(credentials, recovery)
}

func (e *engineCore) SaveState(ctx context.Context, id string, current state.NativeState) error {
	credentials, recovery, err := current.Export()
	if err != nil {
		return err
	}
	return e.stateStore.SaveProtocolState(ctx, id, credentials, recovery)
}
