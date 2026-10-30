package onboarding

import (
	"context"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding/android"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

type integrityProvider interface {
	Integrity(context.Context) (*waappv1.AndroidIntegrityResult, error)
}

type deviceWamsysProvider struct {
	attestor integrityProvider
}

func (p deviceWamsysProvider) RegistrationMaterial(ctx context.Context, input state.WamsysMaterialInput) (*protocolv1.WamsysCapture, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input.Capture == nil && NativeShouldSendRegistrationGPIA(input.State) {
		input.Integrity = &waappv1.AndroidIntegrityResult{Outcome: &waappv1.AndroidIntegrityResult_ErrorCode{ErrorCode: android.IntegrityUnavailableCode}}
		if p.attestor != nil {
			var err error
			input.Integrity, err = p.attestor.Integrity(ctx)
			if err != nil {
				return nil, err
			}
		}
	}
	return (state.LocalWamsysMaterialProvider{}).RegistrationMaterial(ctx, input)
}

func (e *engineCore) WamsysProvider() state.WamsysMaterialProvider {
	if e.attestor != nil {
		return deviceWamsysProvider{attestor: e.attestor}
	}
	return deviceWamsysProvider{}
}
