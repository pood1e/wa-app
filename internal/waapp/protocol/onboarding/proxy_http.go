package onboarding

import "github.com/pood1e/wa-app/internal/waapp/protocol/wire"

func (e *engineCore) HttpForProxy() (*wire.NativeHTTPClient, error) {
	if _, err := e.proxyURL(); err != nil {
		return nil, err
	}
	return e.http, nil
}
