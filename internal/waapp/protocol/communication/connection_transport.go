package communication

import (
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func LongConnectionChatdEndpoints(state pstate.NativeState) []ChatdEndpoint {
	endpoints := []ChatdEndpoint{}
	seen := map[string]struct{}{}
	add := func(host string, port int) {
		endpoint := ChatdEndpoint{Host: host, Port: port}.Normalized(DefaultChatdHost, DefaultChatdPort)
		if endpoint.Host == "" || endpoint.Port != DefaultChatdPort {
			return
		}
		key := endpoint.Address()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		endpoints = append(endpoints, endpoint)
	}
	if state.ChatConnection.GetLastHost() != "" {
		add(state.ChatConnection.GetLastHost(), int(state.ChatConnection.GetLastPort()))
	}
	add(DefaultChatdHost, DefaultChatdPort)
	add(ChatdFallbackHost, DefaultChatdPort)
	return endpoints
}

func (e *Connection) CloseLocked() error {
	if e.Session == nil {
		return nil
	}
	if e.stopSessionCancel != nil {
		e.stopSessionCancel()
		e.stopSessionCancel = nil
	}
	err := e.Session.Close()
	e.Session = nil
	return err
}

func (e *Connection) closeFailedSessionLocked() {
	if e.Session != nil && e.Session.Transport.failure != nil {
		_ = e.CloseLocked()
	}
}

var _ wacore.Communication = (*Connection)(nil)

var _ interface{ Close() error } = (*Connection)(nil)
