package session

import (
	"context"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type LeaseStore interface {
	ClaimLease(context.Context, string, string, time.Duration) (bool, error)
	RenewLease(context.Context, string, string, time.Duration) (bool, error)
	ReleaseLease(context.Context, string, string) error
}

const ownershipTTL = 30 * time.Second

func (m *Manager) own(ctx context.Context, accountID string) (context.Context, func(), error) {
	holder := m.ids.NewID("holder_")
	key := "account:" + accountID
	claimCtx, cancelClaim := context.WithTimeout(ctx, 5*time.Second)
	granted, err := m.leases.ClaimLease(claimCtx, key, holder, ownershipTTL)
	cancelClaim()
	if err != nil {
		return nil, nil, err
	}
	if !granted {
		return nil, nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "account session is owned by another holder", true)
	}
	owned, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(ownershipTTL / 3)
		defer ticker.Stop()
		for {
			select {
			case <-owned.Done():
				return
			case <-ticker.C:
				renewCtx, stop := context.WithTimeout(owned, 5*time.Second)
				renewed, err := m.leases.RenewLease(renewCtx, key, holder, ownershipTTL)
				stop()
				if err != nil || !renewed {
					cancel()
					return
				}
			}
		}
	}()
	release := func() {
		cancel()
		<-done
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_ = m.leases.ReleaseLease(cleanup, key, holder)
	}
	return owned, release, nil
}
