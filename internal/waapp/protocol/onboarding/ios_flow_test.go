package onboarding

import (
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding/ios"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestIOSFailuresIdentifyTheBoundaryWithoutLeakingRPCDetails(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"signer", fmt.Errorf("attest key: %w", status.Error(codes.Unavailable, "PRIVATE_SENTINEL")), "iOS signing service is unavailable"},
		{"locked", fmt.Errorf("create context: %w", status.Error(codes.FailedPrecondition, "device_locked")), "iOS signing device is locked"},
		{"busy", status.Error(codes.ResourceExhausted, "PRIVATE_SENTINEL"), "iOS signing device is busy"},
		{"HTTP", &ios.HTTPStatusError{StatusCode: 502}, "iOS registration server returned HTTP 502"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := shared.ToProtoError(iosResultError(nil, test.err))
			if result.GetMessage() != test.want || strings.Contains(result.GetMessage(), "PRIVATE_SENTINEL") {
				t.Fatal("iOS error lost its source or leaked private RPC details")
			}
		})
	}
}
