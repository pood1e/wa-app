package session

import (
	"context"
	"testing"
	"testing/synctest"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func TestHandoffJoinsOldIdentityAndPreservesCurrentConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		oldContext, oldCancel := context.WithCancel(t.Context())
		currentContext, currentCancel := context.WithCancel(t.Context())
		defer currentCancel()
		otherContext, otherCancel := context.WithCancel(t.Context())
		defer otherCancel()
		cleanup, oldDone := make(chan struct{}), make(chan struct{})
		go func() { <-oldContext.Done(); <-cleanup; close(oldDone) }()
		login := &waappv1.LoginState{WaAccountId: "account", RegisteredIdentityId: "current", Status: waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE}
		manager := &Manager{rootCtx: t.Context(), entries: map[string]*longConnectionEntry{
			"old":     {cancel: oldCancel, snapshot: &waappv1.LongConnectionState{WaAccountId: "account"}, done: oldDone},
			"current": {cancel: currentCancel, snapshot: &waappv1.LongConnectionState{WaAccountId: "account"}},
			"other":   {cancel: otherCancel, snapshot: &waappv1.LongConnectionState{WaAccountId: "another-account"}},
		}}
		finished := make(chan error, 1)
		go func() { finished <- manager.Handoff(t.Context(), login) }()
		synctest.Wait()
		select {
		case <-finished:
			t.Fatal("handoff did not wait for old connection cleanup")
		default:
		}
		close(cleanup)
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		if err := manager.Handoff(t.Context(), login); err != nil {
			t.Fatal(err)
		}
		if currentContext.Err() != nil || otherContext.Err() != nil || !manager.entries["old"].revoked {
			t.Fatal("handoff canceled the current or unrelated connection")
		}
	})
}
