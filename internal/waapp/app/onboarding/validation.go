package onboarding

import (
	"context"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (s *Service) validateRegistration(ctx context.Context, material *waappv1.AcceptedAccountMaterial) (*waappv1.AcceptedAccountMaterial, error) {
	var lastErr error
	for _, delay := range [...]time.Duration{0, time.Second, 2 * time.Second} {
		if !shared.SleepContext(ctx, delay) || ctx.Err() != nil {
			return nil, ctx.Err()
		}
		attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
		validated, err := s.validate(attempt, material)
		cancel()
		if err == nil {
			if validated == nil {
				return nil, conflict("account validation returned no material")
			}
			return validated, nil
		}
		if !shared.ToProtoError(err).GetRetryable() {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}
