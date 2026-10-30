// Package android binds registration requests to device-owned Android keys.
package android

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/attestation"
)

const operationTimeout = 30 * time.Second

// IntegrityUnavailableCode is the native generic SDK/bridge failure outcome.
const IntegrityUnavailableCode int32 = 1000

// Attestor owns one bounded device key context. Its caller serializes operations.
type Attestor struct {
	client       waappv1.AndroidAttestationServiceClient
	reference    *waappv1.AndroidAttestationContext
	publicKey    *ecdsa.PublicKey
	certificates []byte
	failureCode  int32
}

// CreateAttestor creates a device key and verifies its binding to this flow.
func CreateAttestor(ctx context.Context, client waappv1.AndroidAttestationServiceClient, publicKey []byte, build *protocolv1.AndroidBuildMaterial) (*Attestor, error) {
	if len(publicKey) != 32 {
		return nil, errors.New("registration Noise public key must be 32 bytes")
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	result, err := client.CreateContext(ctx, &waappv1.CreateAndroidAttestationContextRequest{RegistrationPublicKey: publicKey})
	if err != nil {
		return nil, fmt.Errorf("create Android attestation context: %w", err)
	}
	if result.GetContext().GetId() == "" {
		return nil, errors.New("Android attestation context is empty")
	}
	a := &Attestor{client: client, reference: result.GetContext()}
	certificates, err := verifyCertificates(result.GetCertificateChain(), publicKey, build, time.Now(), trustedRoots)
	if err == nil {
		a.publicKey, _ = certificates[0].PublicKey.(*ecdsa.PublicKey)
		if a.publicKey == nil || a.publicKey.Curve != elliptic.P256() {
			err = errors.New("Android attestation key must be P-256")
		}
	}
	if err != nil {
		return nil, errors.Join(err, a.Close(context.Background()))
	}
	// The Android HTTP adapter sends the original SDK chain in reverse order.
	for i := len(certificates) - 1; i >= 0; i-- {
		a.certificates = append(a.certificates, certificates[i].Raw...)
	}
	return a, nil
}

// SignRegistration signs the exact ENC value and verifies the returned signature.
func (a *Attestor) SignRegistration(ctx context.Context, message []byte) ([]byte, []byte, error) {
	if a.failureCode != 0 {
		return nil, nil, ctx.Err()
	}
	operation, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	result, err := a.client.Sign(operation, &waappv1.SignAndroidRegistrationRequest{Context: a.reference, Message: message})
	if err != nil {
		if attestation.Unavailable(ctx, err) {
			a.fail(err)
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("sign Android registration: %w", err)
	}
	if result.GetContext().GetId() != a.reference.GetId() {
		return nil, nil, errors.New("Android attestation context changed")
	}
	hash := sha256.Sum256(message)
	if !ecdsa.VerifyASN1(a.publicKey, hash[:], result.GetSignature()) {
		return nil, nil, errors.New("Android registration signature is invalid")
	}
	return result.GetSignature(), a.certificates, nil
}

// Integrity requests the SDK outcome bound to this flow's Noise public key.
func (a *Attestor) Integrity(ctx context.Context) (*waappv1.AndroidIntegrityResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.failureCode != 0 {
		return a.integrityFailure(), nil
	}
	operation, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	result, err := a.client.RequestIntegrity(operation, a.reference)
	if err != nil {
		if attestation.Unavailable(ctx, err) {
			a.fail(err)
			return a.integrityFailure(), nil
		}
		return nil, fmt.Errorf("request Android integrity: %w", err)
	}
	if result.GetContext().GetId() != a.reference.GetId() {
		return nil, errors.New("Android integrity context changed")
	}
	switch outcome := result.GetOutcome().(type) {
	case *waappv1.AndroidIntegrityResult_Token:
		if len(outcome.Token) > 0 && len(outcome.Token) <= 16384 {
			return result, nil
		}
	case *waappv1.AndroidIntegrityResult_ErrorCode:
		if outcome.ErrorCode != 0 {
			return result, nil
		}
	}
	return nil, errors.New("Android integrity outcome is invalid")
}

// Close releases the private device key for this flow.
func (a *Attestor) Close(ctx context.Context) error {
	if a.reference == nil {
		return nil
	}
	operation, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	_, err := a.client.ReleaseContext(operation, a.reference)
	if err == nil || attestation.Unavailable(ctx, err) {
		a.reference = nil
		return nil
	}
	return err
}

func (a *Attestor) fail(err error) {
	a.failureCode = IntegrityUnavailableCode
	if status.Code(err) == codes.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded) {
		a.failureCode = 1004
	}
}

func (a *Attestor) integrityFailure() *waappv1.AndroidIntegrityResult {
	return &waappv1.AndroidIntegrityResult{Context: a.reference,
		Outcome: &waappv1.AndroidIntegrityResult_ErrorCode{ErrorCode: a.failureCode}}
}
