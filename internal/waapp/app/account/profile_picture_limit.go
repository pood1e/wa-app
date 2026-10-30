package account

import "context"

func (s *serverCore) acquireProfilePictureLookup(ctx context.Context) (func(), error) {
	if err := s.pictureLookups.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	return func() { s.pictureLookups.Release(1) }, nil
}
