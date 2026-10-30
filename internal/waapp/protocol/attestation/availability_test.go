package attestation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUnavailablePreservesCancellationAndRejectsInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"absent", nil, false},
		{"offline", status.Error(codes.Unavailable, "offline"), true},
		{"busy", status.Error(codes.ResourceExhausted, "busy"), true},
		{"timeout", fmt.Errorf("SDK: %w", status.Error(codes.DeadlineExceeded, "timeout")), true},
		{"local deadline", context.DeadlineExceeded, true},
		{"unsupported", status.Error(codes.Unimplemented, "unsupported"), true},
		{"lost context", status.Error(codes.NotFound, "missing"), true},
		{"invalid binding", status.Error(codes.FailedPrecondition, "mismatched"), false},
		{"bad request", status.Error(codes.InvalidArgument, "invalid"), false},
		{"internal", status.Error(codes.Internal, "bug"), false},
		{"malformed proof", errors.New("invalid proof"), false},
		{"cancellation", context.Canceled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Unavailable(t.Context(), tc.err); got != tc.want {
				t.Fatalf("Unavailable = %v, want %v", got, tc.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if Unavailable(ctx, status.Error(codes.Unavailable, "offline")) || Unavailable(ctx, context.DeadlineExceeded) {
		t.Fatal("caller cancellation was converted to a fallback")
	}
}
