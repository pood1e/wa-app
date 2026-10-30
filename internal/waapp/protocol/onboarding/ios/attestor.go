package ios

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/attestation"
)

const attestationTimeout = 30 * time.Second

// Attestor pins one registration flow to a remote App Attest context.
// The caller owns the gRPC connection and configures its transport credentials.
type Attestor struct {
	client      waappv1.AppAttestServiceClient
	contextID   string
	unavailable bool
}

// CreateAttestor creates a device key for one onboarding flow.
func CreateAttestor(ctx context.Context, client waappv1.AppAttestServiceClient) (*Attestor, error) {
	ctx, cancel := context.WithTimeout(ctx, attestationTimeout)
	defer cancel()
	binding, err := client.CreateContext(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("create app attest context: %w", err)
	}
	if binding.GetId() == "" {
		return nil, errors.New("app attest context is empty")
	}
	return &Attestor{client: client, contextID: binding.GetId()}, nil
}

// AttestKey binds the device key to the registration Noise public key.
// It returns the locally encoded Authorization header or the native absent proof.
func (a *Attestor) AttestKey(ctx context.Context, publicKey []byte) (string, error) {
	if len(publicKey) != 32 {
		return "", errors.New("registration Noise public key must be 32 bytes")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if a == nil || a.unavailable {
		return "", nil
	}
	operation, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	hash := sha256.Sum256(publicKey)
	result, err := a.client.AttestKey(operation, &waappv1.AttestAppAttestKeyRequest{
		Context: &waappv1.AppAttestContext{Id: a.contextID}, ClientDataHash: hash[:],
	})
	if err != nil {
		if attestation.Unavailable(ctx, err) {
			a.unavailable = true
			return "", nil
		}
		return "", fmt.Errorf("attest registration key: %w", err)
	}
	if result.GetContext().GetId() != a.contextID {
		return "", errors.New("app attest context changed")
	}
	return RegistrationAuthorization(result.GetKeyIdentifier(), result.GetKeyAttestation())
}

// Close releases the worker's in-memory state for this flow.
func (a *Attestor) Close(ctx context.Context) error {
	if a.contextID == "" {
		return nil
	}
	operation, cancel := context.WithTimeout(ctx, attestationTimeout)
	defer cancel()
	_, err := a.client.ReleaseContext(operation, &waappv1.AppAttestContext{Id: a.contextID})
	if err == nil || attestation.Unavailable(ctx, err) {
		a.contextID = ""
		return nil
	}
	return err
}

// RegistrationBody signs the exact frame hash and encodes the ENC/H form locally.
func (a *Attestor) RegistrationBody(ctx context.Context, frame []byte) ([]byte, error) {
	if len(frame) < 32+16 {
		return nil, errors.New("registration frame is shorter than public key and tag")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a == nil || a.unavailable {
		return RegistrationBody(frame, nil)
	}
	operation, cancel := context.WithTimeout(ctx, attestationTimeout)
	defer cancel()
	hash := sha256.Sum256(frame)
	result, err := a.client.GenerateAssertion(operation, &waappv1.GenerateAppAttestAssertionRequest{
		Context:        &waappv1.AppAttestContext{Id: a.contextID},
		ClientDataHash: hash[:],
	})
	if err != nil {
		if attestation.Unavailable(ctx, err) {
			a.unavailable = true
			return RegistrationBody(frame, nil)
		}
		return nil, fmt.Errorf("generate app attest assertion: %w", err)
	}
	if result.GetContext().GetId() != a.contextID {
		return nil, errors.New("app attest context changed")
	}
	if len(result.GetAssertion()) == 0 || len(result.GetAssertion()) > 4096 {
		return nil, errors.New("app attest assertion size is invalid")
	}
	return RegistrationBody(frame, result.GetAssertion())
}
