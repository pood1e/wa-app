package communication

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

type ProxyResolver interface {
	GetProfileProxy(context.Context, string) (*waappv1.AccountProxy, error)
}

type Option func(*engineCore)

func WithProxyResolver(resolver ProxyResolver) Option {
	return func(core *engineCore) { core.proxyResolver = resolver }
}

func (e *engineCore) proxyURL(ctx context.Context, profileID string) (string, error) {
	if e.proxyResolver == nil {
		return e.activeProxyURL, nil
	}
	value, err := e.proxyResolver.GetProfileProxy(ctx, profileID)
	if err != nil {
		return "", err
	}
	if value == nil {
		return e.activeProxyURL, nil
	}
	return wamodel.AccountProxyURL(value)
}

// HTTPForProfile returns a routed HTTP client and its operation cleanup.
func (e *engineCore) HTTPForProfile(ctx context.Context, profileID string) (*wire.NativeHTTPClient, func(), error) {
	proxyURL, err := e.proxyURL(ctx, profileID)
	if err != nil {
		return nil, nil, err
	}
	return e.httpForProxy(proxyURL)
}

func (e *engineCore) httpForProxy(proxyURL string) (*wire.NativeHTTPClient, func(), error) {
	if proxyURL == e.activeProxyURL {
		return e.http, func() {}, nil
	}
	client, err := wire.NewNativeHTTPClient(proxyURL)
	if err != nil {
		return nil, nil, err
	}
	return client, client.CloseIdleConnections, nil
}
