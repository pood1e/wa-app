package account

import (
	"bytes"
	"context"
	"crypto/sha256"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (a *Application) accept(ctx context.Context, req *waappv1.AcceptAccountRequest) (*waappv1.WAAccount, error) {
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(req.GetMaterial())
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	material := req.GetMaterial()
	requestID := req.GetContext().GetRequestId()
	var account *waappv1.WAAccount
	err = a.store.WithinAccountTransaction(ctx, requestID, material.GetPhone().GetE164Number(), func(tx AccountTransaction) error {
		receipt, err := tx.GetAccountAcceptance(ctx, requestID)
		if err == nil {
			if !bytes.Equal(receipt.GetMaterialSha256(), digest[:]) {
				return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "acceptance request ID was already used for different material", false)
			}
			account = receipt.GetAccount()
			return nil
		}
		if !wamodel.IsWAAccountNotFound(err) {
			return err
		}
		account, err = acceptMaterial(ctx, tx, material, requestID)
		if err != nil {
			return err
		}
		return tx.SaveAccountAcceptance(ctx, requestID, &waappv1.AccountAcceptanceRecord{MaterialSha256: digest[:], Account: account})
	})
	if err != nil {
		return nil, err
	}
	return account, nil
}
func acceptMaterial(ctx context.Context, tx AccountTransaction, material *waappv1.AcceptedAccountMaterial, requestID string) (*waappv1.WAAccount, error) {
	phone := material.GetPhone().GetE164Number()
	existing, err := tx.FindWAAccountByPhone(ctx, phone)
	if err != nil && !wamodel.IsWAAccountNotFound(err) {
		return nil, err
	}
	if wamodel.IsWAAccountNotFound(err) {
		existing = nil
	}
	now := time.Now().UTC()
	audit := shared.NewAuditStamp(now, now)
	account := wamodel.NewWAAccount("waacc_"+shared.StableID(phone), "", material.GetPhone(), waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_ACTIVE, audit)
	if existing != nil {
		account.WaAccountId = existing.GetWaAccountId()
		account.DisplayName = existing.GetDisplayName()
		account.Audit.CreatedAt = existing.GetAudit().GetCreatedAt()
		if err := retireAccountLogins(ctx, tx, existing.GetWaAccountId(), now); err != nil {
			return nil, err
		}
	}
	if err := tx.SaveWAAccount(ctx, account); err != nil {
		return nil, err
	}
	if material.GetProxy() != nil {
		if err := tx.SaveAccountProxy(ctx, account.GetWaAccountId(), material.GetProxy()); err != nil {
			return nil, err
		}
	}
	profileID := (shared.RandomIDGenerator{}).NewID("wacp_")
	protocolID, err := acceptedProtocolProfile(ctx, tx, material, now)
	if err != nil {
		return nil, err
	}
	profile := &waappv1.ClientProfile{ClientProfileId: profileID, WaAccountId: account.GetWaAccountId(), ProtocolProfileId: protocolID,
		Status: waappv1.ClientProfileStatus_CLIENT_PROFILE_STATUS_READY, RegistrationKeyState: waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_READY,
		MessagingKeyState: waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_READY, Audit: audit}
	if err := tx.SaveClientProfile(ctx, profile); err != nil {
		return nil, err
	}
	if err := tx.SaveProtocolState(ctx, profileID, material.GetCredentials(), material.GetRecovery()); err != nil {
		return nil, err
	}
	login := &waappv1.LoginState{LoginStateId: "walogin_" + shared.StableID(profileID), RegistrationId: requestID, WaAccountId: account.GetWaAccountId(), ClientProfileId: profileID,
		RegisteredIdentityId: "waid_" + shared.StableID(profileID), ServiceAccountId: material.GetServiceAccountId(), ServiceLoginId: material.GetServiceLoginId(),
		Status: waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE, RegisteredAt: timestamppb.New(now), LastVerifiedAt: timestamppb.New(now), Audit: audit}
	if err := tx.SaveLoginState(ctx, login); err != nil {
		return nil, err
	}
	return account, nil
}

func retireAccountLogins(ctx context.Context, tx AccountTransaction, accountID string, now time.Time) error {
	for {
		login, err := tx.GetActiveLoginState(ctx, accountID, "")
		if err != nil {
			if shared.ToProtoError(err).GetCode() == waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND {
				return nil
			}
			return err
		}
		login.Status = waappv1.LoginStateStatus_LOGIN_STATE_STATUS_REVOKED
		login.LastError = nil
		if login.Audit == nil {
			login.Audit = shared.NewAuditStamp(now, now)
		}
		login.Audit.UpdatedAt = timestamppb.New(now)
		if err := tx.SaveLoginState(ctx, login); err != nil {
			return err
		}
	}
}

func acceptedProtocolProfile(ctx context.Context, tx AccountTransaction, material *waappv1.AcceptedAccountMaterial, now time.Time) (string, error) {
	client := material.GetRecovery().GetClient()
	id := "waproto_" + shared.StableID(client.GetPlatform().String()+":"+client.GetAppVersion())
	artifactID := "waart_" + shared.StableID(id)
	artifact := &waappv1.AppArtifact{ArtifactId: artifactID, Label: client.GetPlatform().String(), VersionLabel: client.GetAppVersion(), ObservedAt: timestamppb.New(now)}
	if err := tx.SaveAppArtifact(ctx, artifact); err != nil {
		return "", err
	}
	profile := &waappv1.ProtocolProfile{ProtocolProfileId: id, AppArtifactId: artifactID, DisplayName: artifact.GetLabel(), AppVersion: client.GetAppVersion(),
		Status: waappv1.ProtocolProfileStatus_PROTOCOL_PROFILE_STATUS_ACTIVE, MessageTransports: []waappv1.MessageTransportKind{waappv1.MessageTransportKind_MESSAGE_TRANSPORT_KIND_LONG_CONNECTION},
		DiscoveredAt: timestamppb.New(now), Audit: shared.NewAuditStamp(now, now)}
	return id, tx.SaveProtocolProfile(ctx, profile)
}
