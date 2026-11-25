package account

import (
	"context"

	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *serverCore) accountConnection(ctx context.Context, ref string) (wacore.Communication, error) {
	id, err := wamodel.RequireWAAccountID(ref)
	if err != nil {
		return nil, err
	}
	if _, err := s.GetWAAccountRecord(ctx, id); err != nil {
		return nil, err
	}
	login, err := s.activeContactResolveLoginState(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.longConnections.Acquire(ctx, login)
}
