package communication

import (
	"context"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

const (
	DefaultContactUsyncTimeout = 32 * time.Second
	MaxContactUsyncBatchSize   = 50
)

func (e *contactsService) ResolveContacts(ctx context.Context, input wacore.EngineContactResolveInput) wacore.EngineContactResolveResult {
	return e.ResolveContactsWithSender(ctx, input, nil)
}

// ResolveContactsWithSender runs native usync through the caller-owned chatd connection.
func (e *contactsService) ResolveContactsWithSender(ctx context.Context, input wacore.EngineContactResolveInput, sender ChatdIQSender) wacore.EngineContactResolveResult {
	jids := NormalizeContactUsyncJIDs(input.JIDs)
	if len(jids) == 0 {
		return wacore.EngineContactResolveResult{}
	}
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return wacore.EngineContactResolveResult{Queried: len(jids), Err: err}
	}
	state.EnsureMaps()
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = EnsureChatStatic(state.ChatStatic)
		if err := e.SaveState(ctx, input.ClientProfileID, state); err != nil {
			return wacore.EngineContactResolveResult{Queried: len(jids), Err: err}
		}
	}
	allContacts := ContactsFromNativeStateMessagePayloads(input.WAAccountID, state, jids, e.clock.Now())
	jids = UnresolvedContactResolveJIDs(jids, allContacts)
	if len(jids) == 0 {
		allContacts = wamodel.DedupeWAContacts(allContacts)
		return wacore.EngineContactResolveResult{Contacts: allContacts, Queried: InputQueriedCount(input.JIDs), Resolved: ContactUsyncIdentityCount(allContacts)}
	}
	if sender == nil {
		proxyURL, err := e.proxyURL(ctx, input.ClientProfileID)
		if err != nil {
			return wacore.EngineContactResolveResult{Contacts: allContacts, Queried: InputQueriedCount(input.JIDs), Resolved: ContactUsyncIdentityCount(allContacts), Err: err}
		}
		timeout := input.RemoteTimeout
		if timeout <= 0 {
			timeout = DefaultContactUsyncTimeout
		}
		sender = NewChatdClient(ChatdConfigForState(proxyURL, state, timeout))
	}
	for _, batch := range contactUsyncBatches(jids) {
		if err := ctx.Err(); err != nil {
			return wacore.EngineContactResolveResult{Err: err}
		}
		request := BuildContactUsyncIQ(e.ids.NewID("waiq_"), e.ids.NewID("sync_sid_query_"), batch)
		response, update, err := sender.SendIQ(ctx, state, input.RegisteredIdentityID, input.AppVersion, request, "contact usync iq timed out")
		if ApplyChatdSessionUpdateState(&state, update) {
			if saveErr := e.SaveState(ctx, input.ClientProfileID, state); saveErr != nil {
				return wacore.EngineContactResolveResult{Err: saveErr}
			}
		}
		if err == nil {
			err = validateContactUsyncResponse(response, batch)
		}
		if err != nil {
			return wacore.EngineContactResolveResult{Contacts: allContacts, Queried: InputQueriedCount(input.JIDs), Resolved: ContactUsyncIdentityCount(allContacts), Err: ContactUsyncError(err)}
		}
		allContacts = append(allContacts, ContactsFromContactUsyncIQ(input.WAAccountID, response, e.clock.Now(), batch)...)
	}
	allContacts = wamodel.DedupeWAContacts(allContacts)
	return wacore.EngineContactResolveResult{Contacts: allContacts, Queried: InputQueriedCount(input.JIDs), Resolved: ContactUsyncIdentityCount(allContacts)}
}

func ContactsFromNativeStateMessagePayloads(accountID string, state pstate.NativeState, jids []string, now time.Time) []*waappv1.WAContact {
	if accountID == "" || len(jids) == 0 || (len(state.MessagePayloads) == 0 && len(state.ContactHints) == 0) {
		return nil
	}
	requested := map[string]struct{}{}
	for _, jid := range jids {
		jid = wacore.NormalizeWAJID(jid)
		if strings.HasSuffix(jid, "@lid") {
			requested[jid] = struct{}{}
		}
	}
	if len(requested) == 0 {
		return nil
	}
	hints := []wacore.WAContactHint{}
	for _, hint := range state.ContactHints {
		if _, ok := requested[hint.Normalized().LIDJID]; ok {
			hints = append(hints, hint)
		}
	}
	for _, payload := range state.MessagePayloads {
		for _, hint := range ContactHintsFromNativePayloadMetadata(payload) {
			if _, ok := requested[hint.Normalized().LIDJID]; ok {
				hints = append(hints, hint)
			}
		}
	}
	return wamodel.ContactsFromContactHints(accountID, nil, hints, now)
}

func UnresolvedContactResolveJIDs(jids []string, contacts []*waappv1.WAContact) []string {
	resolved := map[string]struct{}{}
	for _, contact := range contacts {
		if ContactUsyncHasDisplayIdentity(contact) {
			resolved[contact.GetJid()] = struct{}{}
		}
	}
	out := []string{}
	for _, jid := range jids {
		if _, ok := resolved[jid]; ok {
			continue
		}
		out = append(out, jid)
	}
	return out
}

func InputQueriedCount(values []string) int {
	return len(NormalizeContactUsyncJIDs(values))
}

func ContactUsyncError(err error) error {
	message := ChatdFailureMessage("native contact usync failed", err)
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, message, IsRetryableTransportError(err))
}
