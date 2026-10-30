package session

import (
	"context"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type refreshConnection struct {
	wacore.Communication
	calls int
}

func (c *refreshConnection) Reconnect(context.Context) error { c.calls++; return nil }

func TestReconnectOnlyRefreshesSelectedAccount(t *testing.T) {
	selected, other := &refreshConnection{}, &refreshConnection{}
	login := &waappv1.LoginState{RegisteredIdentityId: "proxy-selected"}
	otherLogin := &waappv1.LoginState{RegisteredIdentityId: "proxy-other"}
	entry := &longConnectionEntry{runner: selected, snapshot: &waappv1.LongConnectionState{Status: waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_CONNECTED}}
	manager := &Manager{entries: map[string]*longConnectionEntry{longConnectionKey(login): entry, longConnectionKey(otherLogin): {runner: other}}}
	if err := manager.Reconnect(t.Context(), login); err != nil {
		t.Fatal(err)
	}
	if selected.calls != 1 || other.calls != 0 || entry.runner != selected || entry.revoked {
		t.Fatal("proxy refresh changed ownership or another account")
	}
	entry.revoked = true
	if err := manager.Reconnect(t.Context(), login); err != nil || selected.calls != 1 || !entry.revoked {
		t.Fatal("proxy change revived revoked identity")
	}
}
