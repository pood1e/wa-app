package communication

import (
	"context"
	"errors"
	"strconv"

	"go.mau.fi/whatsmeow/proto/waVnameCert"
	"google.golang.org/protobuf/proto"

	modelv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/model/v1"
	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func businessNameIQ(id string, iqType string, name pwire.ChatdNode) pwire.ChatdNode {
	return pwire.ChatdNode{Tag: "iq", Attrs: map[string]string{
		"id": id, "to": "s.whatsapp.net", "xmlns": "w:biz", "type": iqType,
	}, Content: []pwire.ChatdNode{name}}
}

func (e *accountSettingsService) setAccountBusinessName(ctx context.Context, input wacore.EngineAccountSettingsInput, state pstate.NativeState, sender AccountSettingsIQSender) wacore.EngineAccountSettingsResult {
	result := wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED}
	if state.Client.GetAccountType() != modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS {
		result.Err = shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VERIFICATION_REQUIRED, "business registration must be verified before setting the business name", false)
		return result
	}
	identity, err := ResolveLoginIdentity(input.RegisteredIdentityID, state)
	if err != nil {
		result.Err = err
		return result
	}
	read := func() (*protocolv1.BusinessIdentity, string, error) {
		request := businessNameIQ(e.ids.NewID("waiq_"), "get", pwire.ChatdNode{Tag: "verified_name", Attrs: map[string]string{"jid": identity.Jid}})
		response, err := e.businessIQ(ctx, input, &state, sender, request)
		if err != nil {
			return nil, "", err
		}
		return businessIdentityFromIQ(response)
	}
	confirmed, name, err := read()
	if err != nil {
		result.Err = err
		return result
	}
	if name != input.DisplayName || !businessCertificateOwned(confirmed, state.Signal.Identity.Public) {
		private, err := pwire.DecodeB64Any(state.Signal.Identity.Private)
		if err != nil {
			result.Err = errors.New("business name update identity key is invalid")
			return result
		}
		certificate, err := pwire.NewBusinessCertificate(private, input.DisplayName)
		if err != nil {
			result.Err = err
			return result
		}
		request := businessNameIQ(e.ids.NewID("waiq_"), "set", pwire.ChatdNode{Tag: "verified_name", Attrs: map[string]string{"v": "2"}, Content: certificate})
		response, err := e.businessIQ(ctx, input, &state, sender, request)
		if err != nil {
			result.Err = err
			return result
		}
		if err := ChatdIQError(response); err != nil {
			result.Err = err
			return result
		}
		ack, ok := ChatdChild(response, "verified_name")
		if !ok || ack.Attrs["id"] == "" {
			result.Err = businessConfirmationError()
			return result
		}
		confirmed, name, err = read()
		if err != nil {
			result.Err = err
			return result
		}
		if name != input.DisplayName || !businessCertificateOwned(confirmed, state.Signal.Identity.Public) {
			result.Err = businessConfirmationError()
			return result
		}
		confirmed.CertificateId = ack.Attrs["id"]
	}
	state.BusinessIdentity = confirmed
	state.Client = proto.CloneOf(state.Client)
	state.Client.AccountType = modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS
	if err := e.SaveState(ctx, input.ClientProfileID, state); err != nil {
		result.Err = err
		return result
	}
	result.Status = waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED
	return result
}

func (e *accountSettingsService) businessIQ(ctx context.Context, input wacore.EngineAccountSettingsInput, state *pstate.NativeState, sender AccountSettingsIQSender, request pwire.ChatdNode) (pwire.ChatdNode, error) {
	response, update, err := sender.SendIQ(ctx, *state, input.RegisteredIdentityID, input.AppVersion, request, AccountSettingsIQTimeoutMessage)
	if ApplyChatdSessionUpdateState(state, update) {
		err = errors.Join(err, e.SaveState(ctx, input.ClientProfileID, *state))
	}
	if err != nil {
		return pwire.ChatdNode{}, AccountSettingsRequestError(err)
	}
	if response.Attrs["type"] == "error" {
		failure, _ := ChatdChild(response, "error")
		if request.Attrs["type"] != "get" || failure.Attrs["code"] != "404" {
			return pwire.ChatdNode{}, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "business "+request.Attrs["type"]+" request was rejected"+ChatdIQErrorCodeMessage(failure.Attrs["code"]), false)
		}
	}
	return response, nil
}

func businessIdentityFromIQ(response pwire.ChatdNode) (*protocolv1.BusinessIdentity, string, error) {
	if err := ChatdIQError(response); err != nil {
		failure, ok := ChatdChild(response, "error")
		if ok && failure.Attrs["code"] == "404" {
			return nil, "", nil
		}
		return nil, "", err
	}
	node, ok := ChatdChild(response, "verified_name")
	if !ok || node.Attrs["v"] != "1" || node.Attrs["verified_level"] == "" {
		return nil, "", businessConfirmationError()
	}
	data, ok := node.Content.([]byte)
	if !ok || len(data) == 0 || len(data) > 64<<10 {
		return nil, "", businessConfirmationError()
	}
	var certificate waVnameCert.VerifiedNameCertificate
	var details waVnameCert.VerifiedNameCertificate_Details
	if proto.Unmarshal(data, &certificate) != nil || proto.Unmarshal(certificate.GetDetails(), &details) != nil ||
		details.GetIssuer() != "smb:wa" || details.GetSerial() == 0 || len(certificate.GetSignature()) != 64 {
		return nil, "", businessConfirmationError()
	}
	return &protocolv1.BusinessIdentity{Certificate: append([]byte(nil), data...), CertificateId: strconv.FormatUint(details.GetSerial(), 10)}, details.GetVerifiedName(), nil
}

func businessCertificateOwned(identity *protocolv1.BusinessIdentity, publicKey string) bool {
	public, err := pwire.DecodeB64Any(publicKey)
	if err != nil {
		return false
	}
	var certificate waVnameCert.VerifiedNameCertificate
	if proto.Unmarshal(identity.GetCertificate(), &certificate) != nil {
		return false
	}
	valid, err := pwire.XeddsaVerifyCurve25519(public, certificate.GetDetails(), certificate.GetSignature())
	return err == nil && valid
}

func businessConfirmationError() error {
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "business account confirmation is incomplete; retry to check the current account", true)
}
