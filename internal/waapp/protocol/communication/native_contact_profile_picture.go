package communication

import (
	"bytes"
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

var ContactProfilePictureQueryTypes = []string{pwire.ProfilePictureTypeImage, pwire.ProfilePictureTypePreview}

type ChatdIQSender interface {
	SendIQ(context.Context, pstate.NativeState, string, string, pwire.ChatdNode, string) (pwire.ChatdNode, ChatdSessionUpdate, error)
}

func (e *contactsService) ResolveContactProfilePicture(ctx context.Context, input wacore.EngineContactProfilePictureInput) wacore.EngineContactProfilePictureResult {
	ctx, cancel := context.WithTimeout(ctx, profilePictureTimeout(input))
	defer cancel()
	location, userAgent, err := e.lookupContactProfilePicture(ctx, input, nil)
	if err != nil {
		return wacore.EngineContactProfilePictureResult{Err: err}
	}
	return e.downloadProfilePicture(ctx, input.ClientProfileID, location, userAgent)
}

func profilePictureTimeout(input wacore.EngineContactProfilePictureInput) time.Duration {
	if input.RemoteTimeout > 0 {
		return input.RemoteTimeout
	}
	return pwire.DefaultContactProfilePictureTimeout
}

func (e *contactsService) lookupContactProfilePicture(ctx context.Context, input wacore.EngineContactProfilePictureInput, sender ChatdIQSender) (pwire.ContactProfilePictureLocation, string, error) {
	jid := wacore.NormalizeWAJID(input.ContactJID)
	if input.WAAccountID == "" || input.ClientProfileID == "" || input.RegisteredIdentityID == "" || jid == "" {
		return pwire.ContactProfilePictureLocation{}, "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "WA contact profile picture input is incomplete", false)
	}
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return pwire.ContactProfilePictureLocation{}, "", err
	}
	state.EnsureMaps()
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = EnsureChatStatic(state.ChatStatic)
		if err := e.SaveState(ctx, input.ClientProfileID, state); err != nil {
			return pwire.ContactProfilePictureLocation{}, "", err
		}
	}
	if sender == nil {
		proxyURL, err := e.proxyURL(ctx, input.ClientProfileID)
		if err != nil {
			return pwire.ContactProfilePictureLocation{}, "", err
		}
		cfg := ChatdConfigForState(proxyURL, state, profilePictureTimeout(input))
		cfg.MaxEndpoints = 1
		sender = NewChatdClient(cfg)
	}
	locations, update, err := e.ContactProfilePictureLocationsFromProfileIQ(ctx, sender, state, input, jid)
	if ApplyChatdSessionUpdateState(&state, update) {
		_ = e.SaveState(ctx, input.ClientProfileID, state)
	}
	if err != nil && len(locations) == 0 {
		return pwire.ContactProfilePictureLocation{}, "", err
	}
	if len(locations) == 0 {
		return pwire.ContactProfilePictureLocation{}, "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "WA profile picture not found", false)
	}
	userAgent := "WhatsApp/" + shared.FirstNonEmpty(state.Client.GetAppVersion(), input.AppVersion) + " Android/" + state.Client.GetOsVersion()
	return locations[0], userAgent, nil
}

func (e *contactsService) downloadProfilePicture(ctx context.Context, profileID string, location pwire.ContactProfilePictureLocation, userAgent string) wacore.EngineContactProfilePictureResult {
	if len(location.InlineData) > 0 {
		contentType, err := pwire.ProfilePictureContentType(location.InlineData, "")
		return wacore.EngineContactProfilePictureResult{ProfilePictureID: location.ID, ContentType: contentType, Data: location.InlineData, Err: err}
	}
	data, contentType, err := e.DownloadContactProfilePicture(ctx, profileID, location, userAgent)
	return wacore.EngineContactProfilePictureResult{ProfilePictureID: location.ID, ContentType: contentType, Data: data, Err: err}
}

func (e *contactsService) ContactProfilePictureLocationsFromProfileIQ(ctx context.Context, sender ChatdIQSender, state pstate.NativeState, input wacore.EngineContactProfilePictureInput, jid string) ([]pwire.ContactProfilePictureLocation, ChatdSessionUpdate, error) {
	targets := ContactProfilePictureTargets(jid, input.ContactPNJID)
	if len(targets) == 0 {
		return nil, ChatdSessionUpdate{}, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "WA contact profile picture target is incomplete", false)
	}
	locations := []pwire.ContactProfilePictureLocation{}
	var lastUpdate ChatdSessionUpdate
	var lastErr error
	for _, target := range targets {
		for _, pictureType := range ContactProfilePictureQueryTypes {
			for _, pictureID := range ContactProfilePictureRequestIDs(input.ContactPictureID) {
				trustedContactToken := TrustedContactTokenForProfilePicture(state, target, e.clock.Now())
				request := BuildContactProfilePictureIQ(e.ids.NewID("wappic_"), target, pictureType, pictureID, trustedContactToken)
				response, update, err := sender.SendIQ(ctx, state, input.RegisteredIdentityID, input.AppVersion, request, "profile picture iq timed out")
				lastUpdate = MergeContactProfilePictureUpdate(lastUpdate, update)
				ApplyChatdSessionUpdateState(&state, update)
				if err != nil {
					if ctx.Err() != nil {
						return locations, lastUpdate, err
					}
					lastErr = err
					LogWAContactProfilePictureIQFailure(target, pictureType, pictureID != "", err)
					continue
				}
				location, err := ContactProfilePictureLocationFromIQ(response)
				if err != nil {
					lastErr = err
					LogWAContactProfilePictureIQFailure(target, pictureType, pictureID != "", err)
					continue
				}
				if !ContactProfilePictureLocationDownloadable(location) {
					lastErr = shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "WA profile picture download location not found", false)
					LogWAContactProfilePictureIQFailure(target, pictureType, pictureID != "", lastErr)
					continue
				}
				locations = append(locations, location)
				LogWAContactProfilePictureIQLocation(target, pictureType, pictureID != "", location)
				return locations, lastUpdate, nil
			}
		}
	}
	if len(locations) > 0 {
		return locations, lastUpdate, nil
	}
	if lastErr == nil {
		lastErr = shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "WA profile picture not found", false)
	}
	return nil, lastUpdate, lastErr
}

func MergeContactProfilePictureUpdate(current ChatdSessionUpdate, next ChatdSessionUpdate) ChatdSessionUpdate {
	return MergeChatdSessionUpdate(current, next)
}

func ContactProfilePictureTargets(jid string, pnJID string) []string {
	jid = wacore.NormalizeWAJID(jid)
	pnJID = wacore.NormalizeWAJID(pnJID)
	candidates := []string{pnJID, jid}
	out := []string{}
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = wacore.NormalizeWAJID(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func ContactProfilePictureRequestIDs(pictureID string) []string {
	pictureID = ContactProfilePictureRequestID(pictureID)
	if pictureID == "" {
		return []string{""}
	}
	return []string{pictureID, ""}
}

func BuildContactProfilePictureIQ(id string, jid string, pictureType string, pictureID string, trustedContactToken []byte) pwire.ChatdNode {
	pictureType = shared.FirstNonEmpty(pictureType, pwire.ProfilePictureTypeImage)
	pictureAttrs := map[string]string{"type": pictureType}
	if ContactProfilePictureNeedsURLQuery(jid, pictureType) {
		pictureAttrs["query"] = "url"
	}
	if requestID := ContactProfilePictureRequestID(pictureID); requestID != "" {
		pictureAttrs["id"] = requestID
	}
	picture := pwire.ChatdNode{Tag: "picture", Attrs: pictureAttrs}
	if len(trustedContactToken) > 0 {
		picture.Content = []pwire.ChatdNode{{Tag: "tctoken", Content: bytes.Clone(trustedContactToken)}}
	}
	return pwire.ChatdNode{
		Tag:     "iq",
		Attrs:   map[string]string{"xmlns": "w:profile:picture", "id": id, "to": "s.whatsapp.net", "type": "get", "target": wacore.NormalizeWAJID(jid)},
		Content: []pwire.ChatdNode{picture},
	}
}

func ContactProfilePictureNeedsURLQuery(jid string, pictureType string) bool {
	return pictureType == pwire.ProfilePictureTypeImage || ContactProfilePictureTargetNeedsURLQuery(jid)
}

func ContactProfilePictureTargetNeedsURLQuery(jid string) bool {
	jid = wacore.NormalizeWAJID(jid)
	user := strings.SplitN(jid, "@", 2)[0]
	user = strings.SplitN(user, ":", 2)[0]
	value, ok := ParsePositiveInt64(user)
	if !ok {
		return false
	}
	return (value >= 13135550000 && value <= 13135559999) || (value >= 13165550000 && value <= 13165550099)
}

func ParsePositiveInt64(value string) (int64, bool) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed, err == nil && parsed > 0
}

func (e *contactsService) DownloadContactProfilePicture(ctx context.Context, profileID string, location pwire.ContactProfilePictureLocation, userAgent string) ([]byte, string, error) {
	httpClient, finish, err := e.HTTPForProfile(ctx, profileID)
	if err != nil {
		return nil, "", err
	}
	defer finish()
	return httpClient.GetProfilePicture(ctx, location, userAgent)
}

func LogWAContactProfilePictureIQFailure(target string, pictureType string, requestIDPresent bool, err error) {
	if err == nil {
		return
	}
	log.Printf("WA contact profile picture iq failed target_kind=%s picture_type=%s request_id=%t reason=%s", ContactProfilePictureTargetKind(target), shared.SafeProxyLogToken(pictureType, "unknown"), requestIDPresent, ContactProfilePictureFailureReason(err))
}

func LogWAContactProfilePictureIQLocation(target string, pictureType string, requestIDPresent bool, location pwire.ContactProfilePictureLocation) {
	log.Printf("WA contact profile picture iq location target_kind=%s picture_type=%s request_id=%t inline=%t direct_path=%t url=%t", ContactProfilePictureTargetKind(target), shared.SafeProxyLogToken(pictureType, "unknown"), requestIDPresent, len(location.InlineData) > 0, location.DirectPath != "", location.URL != "")
}
