package communication

import (
	"context"
	"errors"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type AccountSettingsIQSender interface {
	SendIQ(context.Context, pstate.NativeState, string, string, pwire.ChatdNode, string) (pwire.ChatdNode, ChatdSessionUpdate, error)
}

func (e *accountSettingsService) ApplyAccountSettings(ctx context.Context, input wacore.EngineAccountSettingsInput) wacore.EngineAccountSettingsResult {
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: err}
	}
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = EnsureChatStatic(state.ChatStatic)
		_ = e.SaveState(ctx, input.ClientProfileID, state)
	}
	proxyURL, err := e.proxyURL(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: err}
	}
	return e.ApplyAccountSettingsWithSender(ctx, input, state, NewChatdClient(AccountSettingsChatdConfig(proxyURL, state)))
}

func (e *accountSettingsService) ApplyAccountSettingsWithSender(ctx context.Context, input wacore.EngineAccountSettingsInput, state pstate.NativeState, sender AccountSettingsIQSender) wacore.EngineAccountSettingsResult {
	if input.Kind == waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_TWO_FACTOR_AUTH_STATUS_GET || input.Kind == waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_TWO_FACTOR_AUTH_SETTINGS {
		return e.applyAccountSecurity(ctx, input, &state, sender)
	}
	if input.Kind == waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_PROFILE_NAME_SET {
		return e.ApplyAccountProfileName(ctx, input, state, sender)
	}
	request := BuildAccountSettingsIQ(e.ids.NewID("waiq_"), input)
	if request.Tag == "" {
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "account settings operation is not supported", false)}
	}
	response, update, err := sender.SendIQ(ctx, state, input.RegisteredIdentityID, input.AppVersion, request, AccountSettingsIQTimeoutMessage)
	if ApplyChatdSessionUpdateState(&state, update) {
		_ = e.SaveState(ctx, input.ClientProfileID, state)
	}
	if err != nil {
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: AccountSettingsRequestError(err)}
	}
	logAccountSecurityIQ(input.Kind, state.Client.GetPlatform(), response)
	return AccountSettingsResultFromIQ(input, response)
}

func (e *accountSettingsService) ApplyAccountProfileName(ctx context.Context, input wacore.EngineAccountSettingsInput, state pstate.NativeState, sender AccountSettingsIQSender) wacore.EngineAccountSettingsResult {
	state.PushName = input.DisplayName
	if err := e.SaveState(ctx, input.ClientProfileID, state); err != nil {
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "native account profile name could not be saved", true)}
	}
	timestampMS := e.clock.Now().UnixMilli()
	if timestampMS < 0 {
		timestampMS = 0
	}
	request, collection, err := BuildNativePushNamePatch(&state, input.DisplayName, uint64(timestampMS))
	if err != nil {
		if IsNativePushNamePatchOptionalError(err) {
			return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED}
		}
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: err}
	}
	request.Attrs["id"] = e.ids.NewID("waiq_")
	response, update, err := sender.SendIQ(ctx, state, input.RegisteredIdentityID, input.AppVersion, request, AccountSettingsIQTimeoutMessage)
	changed := ApplyChatdSessionUpdateState(&state, update)
	if err != nil {
		if changed {
			_ = e.SaveState(ctx, input.ClientProfileID, state)
		}
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED}
	}
	if err := ChatdIQError(response); err != nil {
		if changed {
			_ = e.SaveState(ctx, input.ClientProfileID, state)
		}
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED}
	}
	state.EnsureMaps()
	state.AppState.Collections[WaAppStatePushNameCollection] = collection
	_ = e.SaveState(ctx, input.ClientProfileID, state)
	return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED}
}

func IsNativePushNamePatchOptionalError(err error) bool {
	var appErr *shared.AppError
	if !errors.As(err, &appErr) {
		return false
	}
	if appErr.Code != waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT {
		return false
	}
	switch appErr.Message {
	case WaAppStateKeyUnavailable,
		"WA app-state key id is invalid",
		"WA app-state key data is invalid",
		"WA app-state mutation key is invalid":
		return true
	default:
		return false
	}
}

func BuildAccountSettingsIQ(id string, input wacore.EngineAccountSettingsInput) pwire.ChatdNode {
	switch input.Kind {
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_SET:
		return BuildSetAccountEmailIQ(id, input.EmailAddress, input.GoogleIDToken)
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_REQUEST:
		return BuildRequestAccountEmailOtpIQ(id, shared.FirstNonEmpty(input.LocaleLanguage, "en"), shared.FirstNonEmpty(input.LocaleCountry, "US"))
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_VERIFY:
		return BuildVerifyAccountEmailOtpIQ(id, input.Code)
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_PROFILE_PICTURE_SET:
		return BuildAccountProfilePictureIQ(id, input.ProfilePicture)
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_PROFILE_PICTURE_REMOVE:
		return BuildAccountProfilePictureIQ(id, nil)
	default:
		return pwire.ChatdNode{}
	}
}

func BuildAccountIQ(id string, iqType string, children []pwire.ChatdNode) pwire.ChatdNode {
	return pwire.ChatdNode{Tag: "iq", Attrs: map[string]string{"to": "s.whatsapp.net", "id": id, "xmlns": "urn:xmpp:whatsapp:account", "type": iqType}, Content: children}
}

func BuildSetAccountEmailIQ(id string, emailAddress string, googleIDToken string) pwire.ChatdNode {
	children := []pwire.ChatdNode{}
	if strings.TrimSpace(googleIDToken) != "" {
		children = append(children, pwire.ChatdNode{Tag: "id_token", Content: googleIDToken})
	}
	children = append(children, pwire.ChatdNode{Tag: "email_address", Content: emailAddress})
	return BuildAccountIQ(id, "set", []pwire.ChatdNode{{Tag: "email", Content: children}})
}

func BuildRequestAccountEmailOtpIQ(id string, language string, country string) pwire.ChatdNode {
	return BuildAccountIQ(id, "set", []pwire.ChatdNode{{Tag: "verify_email", Content: []pwire.ChatdNode{{Tag: "lg", Content: language}, {Tag: "lc", Content: country}}}})
}

func BuildVerifyAccountEmailOtpIQ(id string, code string) pwire.ChatdNode {
	return BuildAccountIQ(id, "get", []pwire.ChatdNode{{Tag: "verify_email", Content: []pwire.ChatdNode{{Tag: "code", Content: code}}}})
}

func BuildAccountProfilePictureIQ(id string, image []byte) pwire.ChatdNode {
	picture := pwire.ChatdNode{Tag: "picture", Attrs: map[string]string{"type": "image"}}
	if len(image) > 0 {
		picture.Content = append([]byte(nil), image...)
	}
	return pwire.ChatdNode{
		Tag:     "iq",
		Attrs:   map[string]string{"xmlns": "w:profile:picture", "id": id, "to": "s.whatsapp.net", "type": "set"},
		Content: []pwire.ChatdNode{picture},
	}
}

func AccountSettingsResultFromIQ(input wacore.EngineAccountSettingsInput, node pwire.ChatdNode) wacore.EngineAccountSettingsResult {
	if err := ChatdIQError(node); err != nil {
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: err}
	}
	switch input.Kind {
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_SET:
		return EmailSetResultFromIQ(node, input.EmailAddress)
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_REQUEST:
		return EmailOtpRequestResultFromIQ(node)
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_VERIFY:
		return EmailOtpVerifyResultFromIQ(node)
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_PROFILE_PICTURE_SET:
		return AccountProfilePictureSetResultFromIQ(node)
	default:
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED}
	}
}

func AccountProfilePictureSetResultFromIQ(node pwire.ChatdNode) wacore.EngineAccountSettingsResult {
	picture, ok := ChatdChild(node, "picture")
	if !ok {
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED}
	}
	return wacore.EngineAccountSettingsResult{
		Status:           waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED,
		ProfilePictureID: ContactProfilePictureIDFromPicture(picture),
		HasStaging:       ChatdNodeBool(picture, "has_staging"),
	}
}
