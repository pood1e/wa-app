package account

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func TestPresenceUpdatesKeepParticipantsAndIgnoreOlderEvents(t *testing.T) {
	now := time.Now()
	current := &waappv1.WAContactPresence{State: waappv1.WAContactPresenceState_WA_CONTACT_PRESENCE_STATE_ONLINE, ObservedAt: timestamppb.New(now), Activities: []*waappv1.WAChatActivity{{ParticipantRef: "first", Type: waappv1.WAChatActivityType_WA_CHAT_ACTIVITY_TYPE_TYPING, UpdatedAt: timestamppb.New(now)}}}
	mergeContactPresence(current, &waappv1.WAContactPresence{Activities: []*waappv1.WAChatActivity{{ParticipantRef: "second", Type: waappv1.WAChatActivityType_WA_CHAT_ACTIVITY_TYPE_TYPING, UpdatedAt: timestamppb.New(now)}}})
	mergeContactPresence(current, &waappv1.WAContactPresence{State: waappv1.WAContactPresenceState_WA_CONTACT_PRESENCE_STATE_OFFLINE, ObservedAt: timestamppb.New(now.Add(-time.Minute))})
	if current.GetState() != waappv1.WAContactPresenceState_WA_CONTACT_PRESENCE_STATE_ONLINE || len(current.GetActivities()) != 2 {
		t.Fatal("older presence regressed online state or discarded another group participant")
	}
	mergeContactPresence(current, &waappv1.WAContactPresence{State: waappv1.WAContactPresenceState_WA_CONTACT_PRESENCE_STATE_OFFLINE, ObservedAt: timestamppb.New(now.Add(time.Second))})
	if len(current.GetActivities()) != 0 {
		t.Fatal("offline user retained a typing indicator")
	}
}
