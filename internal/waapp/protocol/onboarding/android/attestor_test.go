package android

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type deviceClient struct {
	waappv1.AndroidAttestationServiceClient
	sign           func(*waappv1.SignAndroidRegistrationRequest) *waappv1.AndroidRegistrationSignature
	integrity      func(*waappv1.AndroidAttestationContext) *waappv1.AndroidIntegrityResult
	proof          *waappv1.AndroidKeyAttestation
	released       bool
	signError      error
	integrityError error
	releaseError   error
	signCalls      int
	integrityCalls int
}

func (c *deviceClient) Sign(_ context.Context, r *waappv1.SignAndroidRegistrationRequest, _ ...grpc.CallOption) (*waappv1.AndroidRegistrationSignature, error) {
	c.signCalls++
	if c.signError != nil {
		return nil, c.signError
	}
	return c.sign(r), nil
}
func (c *deviceClient) RequestIntegrity(_ context.Context, r *waappv1.AndroidAttestationContext, _ ...grpc.CallOption) (*waappv1.AndroidIntegrityResult, error) {
	c.integrityCalls++
	if c.integrityError != nil {
		return nil, c.integrityError
	}
	return c.integrity(r), nil
}
func (c *deviceClient) CreateContext(context.Context, *waappv1.CreateAndroidAttestationContextRequest, ...grpc.CallOption) (*waappv1.AndroidKeyAttestation, error) {
	return c.proof, nil
}
func (c *deviceClient) ReleaseContext(context.Context, *waappv1.AndroidAttestationContext, ...grpc.CallOption) (*emptypb.Empty, error) {
	c.released = true
	return &emptypb.Empty{}, c.releaseError
}

func TestWorkerOutagePreservesFlowAndUsesNativeFailureOutcome(t *testing.T) {
	for _, tc := range []struct {
		name string
		sign bool
		code codes.Code
		want int32
	}{
		{"sign unavailable", true, codes.Unavailable, 1000},
		{"sign context lost", true, codes.NotFound, 1000},
		{"integrity unavailable", false, codes.Unavailable, 1000},
		{"integrity context lost", false, codes.NotFound, 1000},
		{"integrity timeout", false, codes.DeadlineExceeded, 1004},
		{"integrity busy", false, codes.ResourceExhausted, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := status.Error(tc.code, "worker failure")
			client := &deviceClient{signError: failure, integrityError: failure, releaseError: status.Error(codes.Unavailable, "offline")}
			reference := &waappv1.AndroidAttestationContext{Id: "synthetic-context"}
			a := &Attestor{client: client, reference: reference}
			if tc.sign {
				if signature, chain, err := a.SignRegistration(t.Context(), []byte("ENC")); err != nil || len(signature) != 0 || len(chain) != 0 {
					t.Fatal("worker outage did not produce an unsigned request")
				}
			}
			for range 2 {
				outcome, err := a.Integrity(t.Context())
				if err != nil || outcome.GetErrorCode() != tc.want || outcome.GetToken() != "" || outcome.GetContext().GetId() != reference.Id {
					t.Fatal("worker failure lost its context or native error outcome")
				}
			}
			if _, _, err := a.SignRegistration(t.Context(), []byte("another-ENC")); err != nil {
				t.Fatal(err)
			}
			if (tc.sign && (client.signCalls != 1 || client.integrityCalls != 0)) || (!tc.sign && (client.signCalls != 0 || client.integrityCalls != 1)) {
				t.Fatal("degraded flow kept calling its unavailable worker")
			}
			if err := a.Close(t.Context()); err != nil || !client.released {
				t.Fatal("unavailable worker blocked flow cleanup")
			}
		})
	}
}

func TestSignerVerifiesExactENCAndContext(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	reference := &waappv1.AndroidAttestationContext{Id: "synthetic-context"}
	message := []byte("exact-ENC-value")
	client := &deviceClient{}
	changedContext, changedMessage := false, false
	client.sign = func(r *waappv1.SignAndroidRegistrationRequest) *waappv1.AndroidRegistrationSignature {
		if !bytes.Equal(r.Message, message) || r.Context.GetId() != reference.Id {
			t.Fatal("signing input changed")
		}
		hash := sha256.Sum256(r.Message)
		if changedMessage {
			hash = sha256.Sum256([]byte("other"))
		}
		sig, err := ecdsa.SignASN1(rand.Reader, key, hash[:])
		if err != nil {
			t.Fatal(err)
		}
		ref := r.Context
		if changedContext {
			ref = &waappv1.AndroidAttestationContext{Id: "other"}
		}
		return &waappv1.AndroidRegistrationSignature{Context: ref, Signature: sig}
	}
	a := &Attestor{client: client, reference: reference, publicKey: &key.PublicKey, certificates: []byte("root-to-leaf")}
	if signature, chain, err := a.SignRegistration(t.Context(), message); err != nil || len(signature) == 0 || string(chain) != "root-to-leaf" {
		t.Fatal("valid signature was rejected")
	}
	changedMessage = true
	if _, _, err := a.SignRegistration(t.Context(), message); err == nil {
		t.Fatal("signature over different bytes accepted")
	}
	changedMessage = false
	changedContext = true
	if _, _, err := a.SignRegistration(t.Context(), message); err == nil {
		t.Fatal("different device context accepted")
	}
}

func TestInvalidCertificateProofReleasesDeviceKey(t *testing.T) {
	client := &deviceClient{proof: &waappv1.AndroidKeyAttestation{Context: &waappv1.AndroidAttestationContext{Id: "synthetic"}, CertificateChain: [][]byte{[]byte("invalid"), []byte("invalid")}}}
	if _, err := CreateAttestor(t.Context(), client, make([]byte, 32), nil); err == nil || !client.released {
		t.Fatal("invalid certificate proof did not release its key")
	}
}
