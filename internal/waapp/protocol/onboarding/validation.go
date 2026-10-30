package onboarding

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

type temporaryState struct {
	material *waappv1.AcceptedAccountMaterial
}

func (*temporaryState) ListRetiredProtocolProfiles(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *temporaryState) GetProtocolState(context.Context, string) (*protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState, error) {
	return s.material.Credentials, s.material.Recovery, nil
}
func (s *temporaryState) SaveProtocolState(_ context.Context, _ string, credentials *protocolv1.AccountCredentials, recovery *protocolv1.AccountRecoveryState) error {
	s.material.Credentials = credentials
	s.material.Recovery = recovery
	return nil
}

// ValidateLogin closes the temporary connection before returning its learned state.
func ValidateLogin(ctx context.Context, material *waappv1.AcceptedAccountMaterial, proxyURL string) (*waappv1.AcceptedAccountMaterial, error) {
	if err := communication.ValidateMaterial(material); err != nil {
		return nil, err
	}
	if material.GetProxy() != nil {
		var err error
		proxyURL, err = wamodel.AccountProxyURL(material.GetProxy())
		if err != nil {
			return nil, err
		}
	}
	repository := &temporaryState{material: proto.Clone(material).(*waappv1.AcceptedAccountMaterial)}
	engine, err := communication.New(repository, proxyURL, nil, nil)
	if err != nil {
		return nil, err
	}
	defer engine.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result := engine.CheckLoginState(ctx, wacore.EngineLoginCheckInput{RegisteredIdentityID: material.GetServiceLoginId(), AppVersion: material.GetRecovery().GetClient().GetAppVersion(), RemoteTimeout: 20 * time.Second})
	if result.Err != nil {
		return nil, result.Err
	}
	if result.Status != waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "temporary account login was not accepted", false)
	}
	return repository.material, nil
}
