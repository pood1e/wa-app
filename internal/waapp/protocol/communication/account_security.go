package communication

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/graph"
	"github.com/pood1e/wa-app/internal/waapp/protocol/security"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func (e *accountSettingsService) applyAccountSecurity(ctx context.Context, input wacore.EngineAccountSettingsInput, state *pstate.NativeState, sender AccountSettingsIQSender) wacore.EngineAccountSettingsResult {
	mex := func(ctx context.Context, op security.Operation, variables any) (json.RawMessage, error) {
		return e.accountSecurityMEX(ctx, input, state, sender, op, variables)
	}
	var nonce security.NonceReceiver
	if receiver, ok := sender.(interface {
		WaitCanonicalNonce(context.Context) (string, error)
	}); ok {
		nonce = receiver.WaitCanonicalNonce
	}
	client := security.New(mex, nil, state.Client, e.clock.Now, nonce)
	if input.Kind == waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_TWO_FACTOR_AUTH_STATUS_GET {
		status, err := client.Status(ctx)
		return accountSecurityResult(status, err)
	}
	http, finish, err := e.HTTPForProfile(ctx, input.ClientProfileID)
	if err != nil {
		return accountSecurityResult(nil, err)
	}
	defer finish()
	client = security.New(mex, http.Client, state.Client, e.clock.Now, nonce)
	credentials, err := client.Credentials(ctx, state.Canonical)
	if err != nil {
		return accountSecurityResult(nil, err)
	}
	if credentials != state.Canonical {
		state.Canonical = credentials
		if err = e.SaveState(ctx, input.ClientProfileID, *state); err != nil {
			return accountSecurityResult(nil, errors.New("account security credentials could not be saved"))
		}
	}
	status, err := client.SetPassword(ctx, input.Password, credentials)
	return accountSecurityResult(status, err)
}

func (e *accountSettingsService) accountSecurityMEX(ctx context.Context, input wacore.EngineAccountSettingsInput, state *pstate.NativeState, sender AccountSettingsIQSender, op security.Operation, variables any) (json.RawMessage, error) {
	typ, err := op.ResponseType()
	if err != nil {
		return nil, errors.New("account security response schema is unavailable")
	}
	payload, err := json.Marshal(struct {
		Variables any `json:"variables"`
	}{variables})
	if err != nil {
		return nil, errors.New("account security request could not be encoded")
	}
	node := wire.ChatdNode{Tag: "iq", Attrs: map[string]string{"id": e.ids.NewID("waiq_"), "to": "s.whatsapp.net", "xmlns": "w:mex", "type": "get"}, Content: []wire.ChatdNode{{Tag: "query", Attrs: map[string]string{"query_id": op.DocumentID()}, Content: payload}}}
	response, update, err := sender.SendIQ(ctx, *state, input.RegisteredIdentityID, input.AppVersion, node, AccountSettingsIQTimeoutMessage)
	if ApplyChatdSessionUpdateState(state, update) {
		if saveErr := e.SaveState(ctx, input.ClientProfileID, *state); saveErr != nil {
			return nil, errors.New("account security connection state could not be saved")
		}
	}
	if err != nil {
		return nil, AccountSettingsRequestError(err)
	}
	if err = ChatdIQError(response); err != nil {
		return nil, err
	}
	result, ok := ChatdChild(response, "result")
	if !ok {
		return nil, errors.New("account security response is missing result")
	}
	raw, ok := result.Content.([]byte)
	if !ok {
		return nil, errors.New("account security response body is invalid")
	}
	format := result.Attrs["format"]
	data, decodeErr := graph.Decode(raw, format, typ)
	if decodeErr != nil {
		log.Printf("WA account security GraphQL op=%s format=%s bytes=%d failed=true", op, format, len(raw))
	}
	return data, decodeErr
}

func accountSecurityResult(status *waappv1.TwoFactorAuthStatus, err error) wacore.EngineAccountSettingsResult {
	if err != nil {
		var appErr *shared.AppError
		if !errors.As(err, &appErr) {
			err = shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, err.Error(), graph.Retryable(err) || AccountSettingsTimeoutError(err))
		}
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: err}
	}
	return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED, TwoFactorStatus: status}
}
