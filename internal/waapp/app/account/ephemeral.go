package account

import (
	"context"
	"time"
)

type EphemeralState interface {
	SaveTransientState(context.Context, string, []byte, time.Duration) error
	GetTransientState(context.Context, string) ([]byte, error)
	DeleteTransientState(context.Context, string) error
	ClaimLease(context.Context, string, string, time.Duration) (bool, error)
	RenewLease(context.Context, string, string, time.Duration) (bool, error)
	ReleaseLease(context.Context, string, string) error
	OpenSessionLease(context.Context, string, time.Duration) error
	CloseSessionLease(context.Context, string) error
}
