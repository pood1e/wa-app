package ios

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type attestServer struct {
	waappv1.UnimplementedAppAttestServiceServer
	bind       func() *waappv1.AppAttestContext
	createErr  error
	attest     func(*waappv1.AttestAppAttestKeyRequest) (*waappv1.AttestAppAttestKeyResponse, error)
	release    func(*waappv1.AppAttestContext)
	releaseErr error
	sign       func(context.Context, *waappv1.GenerateAppAttestAssertionRequest) (*waappv1.GenerateAppAttestAssertionResponse, error)
}

func (s *attestServer) CreateContext(context.Context, *emptypb.Empty) (*waappv1.AppAttestContext, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.bind(), nil
}

func (s *attestServer) GenerateAssertion(ctx context.Context, request *waappv1.GenerateAppAttestAssertionRequest) (*waappv1.GenerateAppAttestAssertionResponse, error) {
	return s.sign(ctx, request)
}

func syntheticAttestContext() *waappv1.AppAttestContext {
	return &waappv1.AppAttestContext{Id: "synthetic-binding"}
}

func (s *attestServer) AttestKey(_ context.Context, request *waappv1.AttestAppAttestKeyRequest) (*waappv1.AttestAppAttestKeyResponse, error) {
	if s.attest != nil {
		return s.attest(request)
	}
	return syntheticAttestBinding(), nil
}

func (s *attestServer) ReleaseContext(_ context.Context, request *waappv1.AppAttestContext) (*emptypb.Empty, error) {
	if s.release != nil {
		s.release(request)
	}
	return &emptypb.Empty{}, s.releaseErr
}

func syntheticAttestBinding() *waappv1.AttestAppAttestKeyResponse {
	return &waappv1.AttestAppAttestKeyResponse{
		Context:        &waappv1.AppAttestContext{Id: "synthetic-binding"},
		KeyIdentifier:  base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xff}, 32)),
		KeyAttestation: []byte("synthetic-key-attestation"),
	}
}

func attestClient(t *testing.T, server *attestServer) waappv1.AppAttestServiceClient {
	t.Helper()
	listener := bufconn.Listen(64 * 1024)
	transport := grpc.NewServer()
	waappv1.RegisterAppAttestServiceServer(transport, server)
	go func() { _ = transport.Serve(listener) }()
	t.Cleanup(func() { transport.Stop() })
	connection, err := grpc.NewClient("passthrough:///attest-test",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDisableRetry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return waappv1.NewAppAttestServiceClient(connection)
}

func TestAttestorSignsBinaryFrameAndBuildsBodyLocally(t *testing.T) {
	frame := bytes.Repeat([]byte{0xff}, 49)
	wantHash := sha256.Sum256(frame)
	wantAssertion := []byte{0x43, 0xff, 0xff, 0xff}
	observed := make(chan *waappv1.GenerateAppAttestAssertionRequest, 1)
	keyHash := make(chan []byte, 1)
	released := make(chan string, 1)
	client := attestClient(t, &attestServer{
		bind: syntheticAttestContext,
		attest: func(request *waappv1.AttestAppAttestKeyRequest) (*waappv1.AttestAppAttestKeyResponse, error) {
			keyHash <- request.ClientDataHash
			return syntheticAttestBinding(), nil
		},
		release: func(request *waappv1.AppAttestContext) { released <- request.Id },
		sign: func(_ context.Context, request *waappv1.GenerateAppAttestAssertionRequest) (*waappv1.GenerateAppAttestAssertionResponse, error) {
			observed <- request
			return &waappv1.GenerateAppAttestAssertionResponse{Context: request.Context, Assertion: wantAssertion}, nil
		},
	})
	attestor, err := CreateAttestor(t.Context(), client)
	if err != nil {
		t.Fatal(err)
	}
	wantAuthorization, err := RegistrationAuthorization(syntheticAttestBinding().KeyIdentifier, syntheticAttestBinding().KeyAttestation)
	authorization, attestErr := attestor.AttestKey(t.Context(), make([]byte, 32))
	if err != nil || attestErr != nil || authorization != wantAuthorization {
		t.Fatal("bound key proof did not produce local Authorization")
	}
	wantKeyHash := sha256.Sum256(make([]byte, 32))
	if !bytes.Equal(<-keyHash, wantKeyHash[:]) {
		t.Fatal("key attestation did not bind the registration public key")
	}
	body, err := attestor.RegistrationBody(t.Context(), frame)
	if err != nil {
		t.Fatal(err)
	}
	request := <-observed
	if !bytes.Equal(request.ClientDataHash, wantHash[:]) || request.Context.Id != "synthetic-binding" {
		t.Fatal("assertion request did not bind the exact binary frame and context")
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatal(err)
	}
	decodedFrame, err := base64.URLEncoding.DecodeString(form.Get("ENC"))
	if err != nil || !bytes.Equal(decodedFrame, frame) {
		t.Fatal("ENC did not preserve the frame")
	}
	payload, err := base64.RawURLEncoding.DecodeString(form.Get("H"))
	if err != nil {
		t.Fatal(err)
	}
	var proof struct{ Assertion []byte }
	if err := json.Unmarshal(payload, &proof); err != nil || !bytes.Equal(proof.Assertion, wantAssertion) {
		t.Fatal("H did not preserve the assertion")
	}
	if err := attestor.Close(t.Context()); err != nil || <-released != "synthetic-binding" {
		t.Fatal("close did not release the bound context")
	}
}

func TestAttestorRejectsMismatchedKeyAttestation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*waappv1.AttestAppAttestKeyResponse)
	}{
		{"changed context", func(response *waappv1.AttestAppAttestKeyResponse) { response.Context.Id = "other-binding" }},
		{"missing proof", func(response *waappv1.AttestAppAttestKeyResponse) { response.KeyAttestation = nil }},
		{"invalid key identifier", func(response *waappv1.AttestAppAttestKeyResponse) { response.KeyIdentifier = "invalid" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := attestClient(t, &attestServer{
				bind: syntheticAttestContext,
				attest: func(*waappv1.AttestAppAttestKeyRequest) (*waappv1.AttestAppAttestKeyResponse, error) {
					response := syntheticAttestBinding()
					test.mutate(response)
					return response, nil
				},
			})
			attestor, err := CreateAttestor(t.Context(), client)
			if err != nil {
				t.Fatal(err)
			}
			if authorization, err := attestor.AttestKey(t.Context(), make([]byte, 32)); err == nil || authorization != "" {
				t.Fatal("invalid key proof produced an Authorization header")
			}
		})
	}
}

func TestAttestorRejectsChangedContextAndMissingAssertion(t *testing.T) {
	for _, test := range []struct {
		name    string
		context string
		proof   []byte
	}{
		{"changed context", "other-binding", []byte("synthetic")},
		{"missing assertion", "synthetic-binding", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := attestClient(t, &attestServer{
				bind: syntheticAttestContext,
				sign: func(context.Context, *waappv1.GenerateAppAttestAssertionRequest) (*waappv1.GenerateAppAttestAssertionResponse, error) {
					return &waappv1.GenerateAppAttestAssertionResponse{Context: &waappv1.AppAttestContext{Id: test.context}, Assertion: test.proof}, nil
				},
			})
			attestor, err := CreateAttestor(t.Context(), client)
			if err != nil {
				t.Fatal(err)
			}
			if body, err := attestor.RegistrationBody(t.Context(), make([]byte, 48)); err == nil || body != nil {
				t.Fatal("invalid proof produced a registration body")
			}
		})
	}
}

func TestAttestorPropagatesCancellation(t *testing.T) {
	started := make(chan struct{})
	client := attestClient(t, &attestServer{
		bind: syntheticAttestContext,
		sign: func(ctx context.Context, _ *waappv1.GenerateAppAttestAssertionRequest) (*waappv1.GenerateAppAttestAssertionResponse, error) {
			close(started)
			<-ctx.Done()
			return nil, status.FromContextError(ctx.Err()).Err()
		},
	})
	attestor, err := CreateAttestor(t.Context(), client)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := attestor.RegistrationBody(ctx, make([]byte, 48))
		result <- err
	}()
	select {
	case <-started:
	case <-t.Context().Done():
		t.Fatal("assertion request did not start")
	}
	cancel()
	if err := <-result; status.Code(err) != codes.Canceled {
		t.Fatalf("status = %v, want canceled", status.Code(err))
	}
}
