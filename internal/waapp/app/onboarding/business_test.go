package onboarding

import (
	"testing"

	modelv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/model/v1"
	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func TestBusinessIntentIsNormalizedAndRetainedAcrossPlatforms(t *testing.T) {
	for _, platform := range []protocolv1.ClientPlatform{protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID, protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS} {
		t.Run(platform.String(), func(t *testing.T) {
			service, _, _ := testService(t)
			req := beginRequest(true)
			req.Platform = platform
			req.Business = &modelv1.BusinessRegistration{Name: " WA Lab "}
			first, err := service.BeginAccountAdd(t.Context(), req)
			if err != nil || first.GetError() != nil || first.GetFlow().GetPlatform() != platform || first.GetFlow().GetBusiness().GetName() != "WA Lab" {
				t.Fatal("business intent was rejected or changed", err, first.GetError())
			}
			req.Business.Name = "Changed"
			first.Flow.Business.Name = "Changed"
			current, err := service.GetAccountAdd(t.Context(), &waappv1.AccountAddRequest{FlowId: first.GetFlow().GetFlowId()})
			if err != nil || current.GetFlow().GetBusiness().GetName() != "WA Lab" {
				t.Fatal("flow shares its business intent with a caller", err)
			}
		})
	}
}
