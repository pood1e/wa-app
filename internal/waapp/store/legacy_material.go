package store

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type legacyRepository interface {
	GetClientProfile(context.Context, string) (*waappv1.ClientProfile, error)
	GetWAAccount(context.Context, string) (*waappv1.WAAccount, error)
	GetProtocolProfile(context.Context, string) (*waappv1.ProtocolProfile, error)
	SaveProtocolState(context.Context, string, *protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState) error
	GetProtocolState(context.Context, string) (*protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState, error)
}

func migrateLegacyMaterial(ctx context.Context, repo legacyRepository, id string, data []byte) error {
	var previous state.NativeState
	if err := json.Unmarshal(data, &previous); err != nil {
		return fmt.Errorf("invalid legacy protocol state")
	}
	profile, err := repo.GetClientProfile(ctx, id)
	if err != nil {
		return err
	}
	account, err := repo.GetWAAccount(ctx, profile.GetWaAccountId())
	if err != nil {
		return err
	}
	protocol, err := repo.GetProtocolProfile(ctx, profile.GetProtocolProfileId())
	if err != nil {
		return err
	}
	if previous.Client == nil {
		previous.Client = state.AndroidClientMetadata(previous, protocol.GetAppVersion())
	}
	credentials, recovery, err := previous.Export()
	if err != nil {
		return fmt.Errorf("convert legacy protocol state")
	}
	material := &waappv1.AcceptedAccountMaterial{Phone: account.GetPhone(), ServiceLoginId: shared.DigitsOnly(account.GetPhone().GetE164Number()), ServiceAccountId: "legacy", Credentials: credentials, Recovery: recovery}
	if err := communication.ValidateMaterial(material); err != nil {
		return fmt.Errorf("legacy account protocol material is incomplete")
	}
	if err := repo.SaveProtocolState(ctx, id, credentials, recovery); err != nil {
		return err
	}
	savedKeys, savedState, err := repo.GetProtocolState(ctx, id)
	if err != nil {
		return err
	}
	if !proto.Equal(savedKeys, credentials) || !proto.Equal(savedState, recovery) {
		return fmt.Errorf("migrated protocol material differs from source")
	}
	return nil
}
