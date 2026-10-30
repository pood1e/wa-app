package rpc

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func NewServer(token string) (*grpc.Server, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("service token is required")
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		if info.FullMethod != healthv1.Health_Check_FullMethodName {
			values := metadata.ValueFromIncomingContext(ctx, "authorization")
			if len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte("Bearer "+token)) != 1 {
				return nil, status.Error(codes.Unauthenticated, "service authentication required")
			}
		}
		return next(ctx, req)
	}))
	healthServer := health.NewServer()
	healthv1.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", healthv1.HealthCheckResponse_SERVING)
	return server, nil
}

func Dial(target, token string) (*grpc.ClientConn, error) {
	if strings.TrimSpace(target) == "" || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("gRPC target and service token are required")
	}
	return grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDisableRetry(),
		grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, conn *grpc.ClientConn, next grpc.UnaryInvoker, options ...grpc.CallOption) error {
			return next(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token), method, req, reply, conn, options...)
		}))
}

func Serve(ctx context.Context, address string, server *grpc.Server) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	stopped := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
		case <-stopped:
			return
		}
		done := make(chan struct{})
		go func() { server.GracefulStop(); close(done) }()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			server.Stop()
			<-done
		}
	}()
	err = server.Serve(listener)
	close(stopped)
	<-shutdownDone
	if ctx.Err() != nil {
		return nil
	}
	return err
}

type AcceptanceClient struct {
	client waappv1.WaAccountAcceptanceServiceClient
}

func NewAcceptanceClient(conn grpc.ClientConnInterface) *AcceptanceClient {
	return &AcceptanceClient{waappv1.NewWaAccountAcceptanceServiceClient(conn)}
}
func (c *AcceptanceClient) AcceptAccount(ctx context.Context, req *waappv1.AcceptAccountRequest) (*waappv1.AcceptAccountResponse, error) {
	return c.client.AcceptAccount(ctx, req)
}
func (c *AcceptanceClient) GetAccountAcceptance(ctx context.Context, req *waappv1.GetAccountAcceptanceRequest) (*waappv1.AcceptAccountResponse, error) {
	return c.client.GetAccountAcceptance(ctx, req)
}
