package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/byte-v-forge/wa-app/internal/app"
	"github.com/byte-v-forge/wa-app/internal/config"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	defaultGRPCListenAddr     = ":50091"
	defaultDashboardHTTPAddr  = ":8080"
	defaultDashboardStaticDir = "/app/dashboard/wa"
	defaultWAAppDataDirectory = "/var/lib/wa-app"
)

func main() {
	cfg := config.Load()
	dataDir := configValue(cfg.DataDir, defaultWAAppDataDirectory)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := newDurableStore(ctx, cfg, dataDir)
	if err != nil {
		log.Fatalf("initialize wa-app durable store: %v", err)
	}
	defer store.Close()

	runtime, err := newRuntimeState(ctx, cfg, dataDir)
	if err != nil {
		log.Fatalf("initialize wa-app runtime state: %v", err)
	}
	defer func() { _ = runtime.Close() }()

	clock := app.SystemClock{}
	ids := app.RandomIDGenerator{}
	engine, err := app.NewNativeEngine(store, clock, ids)
	if err != nil {
		log.Fatalf("initialize wa-app native engine: %v", err)
	}
	if strings.TrimSpace(cfg.CommonProxy) != "" {
		engine, err = engine.WithProxyURL(cfg.CommonProxy)
		if err != nil {
			log.Fatalf("initialize wa-app common proxy: %v", err)
		}
	}
	service := app.NewServer(store, runtime, engine, clock, ids)
	service.SetCommonProxyURL(cfg.CommonProxy)
	service.SetRegistrationProxyLeaseMode(cfg.RegistrationProxyLeaseMode)
	service.SetRegistrationProxyLeaseAccountID(cfg.RegistrationProxyLeaseAccountID)
	service.SetRegistrationProxyLeaseHTTPProvider(cfg.RegistrationProxyLeaseAPIBaseURL, cfg.RegistrationProxyLeaseAuthToken)
	authConfig := newDashboardAuthConfig(cfg.DashboardAuthPass)
	grpcListenAddr := configValue(cfg.GRPCListenAddr, defaultGRPCListenAddr)
	dashboardHTTPAddr := configValue(cfg.DashboardHTTPAddr, defaultDashboardHTTPAddr)
	dashboardStaticDir := configValue(cfg.DashboardStaticDir, defaultDashboardStaticDir)
	listener, err := net.Listen("tcp", grpcListenAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", grpcListenAddr, err)
	}
	server := grpc.NewServer()
	waappv1.RegisterWaDiscoveryServiceServer(server, service)
	waappv1.RegisterWaProfileServiceServer(server, service)
	waappv1.RegisterWaRegistrationServiceServer(server, service)
	waappv1.RegisterWaMessagingServiceServer(server, service)
	waappv1.RegisterWaExtractionServiceServer(server, service)
	waappv1.RegisterWaContactServiceServer(server, service)
	waappv1.RegisterWaToolingServiceServer(server, service)
	waappv1.RegisterWaAccountSettingsServiceServer(server, service)
	healthServer := health.NewServer()
	healthv1.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", healthv1.HealthCheckResponse_SERVING)

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		log.Printf("wa-app-service listening on %s", grpcListenAddr)
		if err := server.Serve(listener); err != nil && groupCtx.Err() == nil {
			return err
		}
		return nil
	})
	group.Go(func() error {
		<-groupCtx.Done()
		server.GracefulStop()
		return nil
	})
	group.Go(func() error {
		return runDashboardHTTP(groupCtx, dashboardHTTPAddr, dashboardStaticDir, service, newWAActionHandler(service), authConfig)
	})
	group.Go(func() error {
		return service.RunLongConnections(groupCtx)
	})
	if err := group.Wait(); err != nil {
		stop()
		log.Fatalf("wa-app-service failed: %v", err)
	}
}

func configValue(value string, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

func newDurableStore(ctx context.Context, cfg config.Config, dataDir string) (app.Store, error) {
	if strings.TrimSpace(cfg.PGDSN) != "" {
		return app.NewPostgresStore(ctx, cfg.PGDSN)
	}
	log.Printf("WA_APP_PG_DSN is not configured; wa-app uses sqlite durable store in %s", dataDir)
	return app.NewSQLiteStore(ctx, dataDir)
}

func newRuntimeState(ctx context.Context, cfg config.Config, dataDir string) (app.RuntimeState, error) {
	if strings.TrimSpace(cfg.RedisURL) != "" {
		return app.NewRedisRuntime(ctx, cfg.RedisURL)
	}
	log.Printf("WA_APP_REDIS_URL is not configured; wa-app uses sqlite runtime state in %s", dataDir)
	return app.NewSQLiteRuntime(ctx, dataDir)
}
