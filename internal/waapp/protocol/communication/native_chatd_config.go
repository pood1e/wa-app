package communication

import (
	"time"

	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

const (
	ChatdFallbackHost = "g-fallback.whatsapp.net"
)

func ChatdConfigForState(proxyURL string, state pstate.NativeState, timeout time.Duration) ChatdClientConfig {
	cfg := ChatdClientConfig{
		ProxyURL:    proxyURL,
		RoutingInfo: state.ChatRoutingInfo,
		Endpoints:   ChatdEndpointsForState(state),
	}
	if timeout > 0 {
		cfg.Timeout = timeout
	}
	return cfg
}

func AccountSettingsChatdConfig(proxyURL string, state pstate.NativeState) ChatdClientConfig {
	cfg := ChatdConfigForState(proxyURL, state, DefaultAccountIQTimeout)
	cfg.OpenTimeout = DefaultAccountSettingsOpenTimeout
	return cfg
}

func ChatdEndpointsForState(state pstate.NativeState) []ChatdEndpoint {
	endpoints := []ChatdEndpoint{}
	if state.ChatConnection.GetLastHost() != "" && state.ChatConnection.GetLastPort() > 0 {
		endpoints = append(endpoints, ChatdEndpoint{Host: state.ChatConnection.GetLastHost(), Port: int(state.ChatConnection.GetLastPort())})
	}
	endpoints = append(endpoints, ChatdEndpoint{Host: DefaultChatdHost, Port: DefaultChatdPort})
	endpoints = append(endpoints, ChatdEndpoint{Host: ChatdFallbackHost, Port: DefaultChatdPort})
	return endpoints
}
