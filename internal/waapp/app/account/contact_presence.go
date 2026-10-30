package account

import (
	"context"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

const contactPresenceTTL = 5 * time.Minute
const chatActivityTTL = 15 * time.Second

type presenceSubscriber interface {
	SubscribePresence(context.Context, string, string) error
}

func presenceKey(loginID, contact string) string {
	return "wa-presence:" + loginID + ":" + contact
}

func (s *contactHandler) SubscribeWAContactPresence(ctx context.Context, req *waappv1.SubscribeWAContactPresenceRequest) (*waappv1.SubscribeWAContactPresenceResponse, error) {
	id, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.SubscribeWAContactPresenceResponse{Error: shared.ToProtoError(err)}, nil
	}
	account, err := s.GetWAAccountRecord(ctx, id)
	if err != nil {
		return &waappv1.SubscribeWAContactPresenceResponse{Error: shared.ToProtoError(err)}, nil
	}
	contact, err := s.store.GetWAContactByRef(ctx, id, req.GetContactRef())
	if err != nil {
		return &waappv1.SubscribeWAContactPresenceResponse{Error: shared.ToProtoError(err)}, nil
	}
	login, err := s.activeContactResolveLoginState(ctx, id)
	if err != nil {
		return &waappv1.SubscribeWAContactPresenceResponse{Error: shared.ToProtoError(err)}, nil
	}
	response := &waappv1.SubscribeWAContactPresenceResponse{}
	if s.runtime == nil || s.longConnections == nil {
		return response, nil
	}
	refs := contactActionRefs(req.GetContactRef(), contact)
	for _, ref := range refs {
		presence := s.cachedContactPresence(ctx, login.GetLoginStateId(), ref)
		if presence != nil {
			response.Presence = presence
			break
		}
	}
	// Group chat activity arrives on the existing connection without subscriptions.
	if contact.GetKind() == waappv1.WAContactKind_WA_CONTACT_KIND_GROUP || contact.GetKind() == waappv1.WAContactKind_WA_CONTACT_KIND_SYSTEM {
		return response, nil
	}
	runner := s.longConnections.Runner(login)
	subscriber, ok := runner.(presenceSubscriber)
	if !ok {
		return response, nil
	}
	key := presenceKey(login.GetLoginStateId(), contact.GetJid()) + ":subscription"
	owner := s.ids.NewID("presence_")
	claimed, err := s.runtime.ClaimLease(ctx, key, owner, time.Minute)
	if err != nil || !claimed {
		return response, nil
	}
	if err := subscriber.SubscribePresence(ctx, contact.GetJid(), account.GetDisplayName()); err != nil {
		_ = s.runtime.ReleaseLease(ctx, key, owner)
		response.Error = shared.ToProtoError(err)
	}
	return response, nil
}

func (s *serverCore) cachedContactPresence(ctx context.Context, loginID, ref string) *waappv1.WAContactPresence {
	data, err := s.runtime.GetTransientState(ctx, presenceKey(loginID, ref))
	if err != nil || len(data) == 0 {
		return nil
	}
	presence := new(waappv1.WAContactPresence)
	if proto.Unmarshal(data, presence) != nil {
		return nil
	}
	now := s.clock.Now()
	if now.Sub(presence.GetObservedAt().AsTime()) > contactPresenceTTL {
		presence.State = waappv1.WAContactPresenceState_WA_CONTACT_PRESENCE_STATE_UNSPECIFIED
	}
	presence.Activities = slices.DeleteFunc(presence.GetActivities(), func(activity *waappv1.WAChatActivity) bool {
		return activity.GetUpdatedAt() == nil || now.Sub(activity.GetUpdatedAt().AsTime()) > chatActivityTTL
	})
	return presence
}

func (s *serverCore) saveContactPresences(ctx context.Context, session *waappv1.MessageSession, updates []*waappv1.WAContactPresence) error {
	if s.runtime == nil || len(updates) == 0 {
		return nil
	}
	login, err := s.store.GetLoginStateByRegisteredIdentity(ctx, session.GetRegisteredIdentityId())
	if err != nil {
		return err
	}
	for _, update := range updates {
		if strings.TrimSpace(update.GetContactRef()) == "" {
			continue
		}
		current := s.cachedContactPresence(ctx, login.GetLoginStateId(), update.GetContactRef())
		if current == nil {
			current = &waappv1.WAContactPresence{ContactRef: update.GetContactRef()}
		}
		mergeContactPresence(current, update)
		data, err := proto.Marshal(current)
		if err != nil {
			return err
		}
		if err := s.runtime.SaveTransientState(ctx, presenceKey(login.GetLoginStateId(), update.GetContactRef()), data, contactPresenceTTL); err != nil {
			return err
		}
	}
	return nil
}

func mergeContactPresence(current, update *waappv1.WAContactPresence) {
	if update.GetObservedAt() != nil && (current.GetObservedAt() == nil || !update.GetObservedAt().AsTime().Before(current.GetObservedAt().AsTime())) {
		current.State = update.GetState()
		current.ObservedAt = update.GetObservedAt()
		current.LastSeenAt = update.GetLastSeenAt()
		if current.GetState() == waappv1.WAContactPresenceState_WA_CONTACT_PRESENCE_STATE_OFFLINE {
			current.Activities = nil
		}
	}
	for _, activity := range update.GetActivities() {
		index := slices.IndexFunc(current.GetActivities(), func(item *waappv1.WAChatActivity) bool {
			return item.GetParticipantRef() == activity.GetParticipantRef()
		})
		if index < 0 {
			current.Activities = append(current.Activities, activity)
		} else if !activity.GetUpdatedAt().AsTime().Before(current.Activities[index].GetUpdatedAt().AsTime()) {
			current.Activities[index] = activity
		}
	}
}
