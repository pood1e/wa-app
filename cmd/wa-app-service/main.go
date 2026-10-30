package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/config"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/httpapi"
	"github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/rpc"
	"github.com/pood1e/wa-app/internal/waapp/runtime"
	"github.com/pood1e/wa-app/internal/waapp/store"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadRuntime()
	if err != nil {
		return err
	}
	if len(os.Args) == 2 && os.Args[1] == "health" {
		return rpc.CheckHealth(ctx, cfg.GRPCListenAddr)
	}
	repository, err := openRepository(ctx, cfg)
	if err != nil {
		return err
	}
	defer repository.Close()
	ephemeral, err := openEphemeralState(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = ephemeral.Close() }()
	protocol, err := communication.New(repository, cfg.CommonProxy, nil, nil, communication.WithProxyResolver(repository))
	if err != nil {
		return err
	}
	defer protocol.CloseIdleConnections()
	factory := func(ctx context.Context, input wacore.EngineMessageInput) (wacore.Communication, error) {
		return communication.NewConnection(protocol, communication.ConnectionOptions{Context: ctx, Input: input}), nil
	}
	application := account.New(repository, ephemeral, protocol, factory, nil, nil)
	server, err := rpc.NewServer(cfg.ServiceToken)
	if err != nil {
		return err
	}
	rpc.RegisterRuntime(server, application)
	adding, err := rpc.Dial(cfg.AccountAddTarget, cfg.ServiceToken)
	if err != nil {
		return err
	}
	defer adding.Close()
	group, runCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return rpc.Serve(runCtx, cfg.GRPCListenAddr, server) })
	group.Go(func() error { return application.RunLongConnections(runCtx) })
	group.Go(func() error {
		return httpapi.Serve(runCtx, httpapi.Config{ListenAddr: cfg.DashboardHTTPAddr, StaticDir: cfg.DashboardStaticDir, Password: cfg.DashboardAuthPass}, application, waappv1.NewWaAccountAddServiceClient(adding))
	})
	return group.Wait()
}

type repository interface {
	account.Repository
	communication.StateRepository
	communication.ProxyResolver
	Close()
}

func openRepository(ctx context.Context, cfg config.Runtime) (repository, error) {
	switch cfg.Store {
	case "sqlite":
		return store.NewSQLiteStoreFile(ctx, cfg.SQLiteFile)
	case "postgres":
		return store.NewPostgresStore(ctx, cfg.PGDSN)
	default:
		return nil, fmt.Errorf("WA_APP_STORE must be postgres or sqlite")
	}
}

type ephemeralState interface {
	account.EphemeralState
	Close() error
}

func openEphemeralState(ctx context.Context, cfg config.Runtime) (ephemeralState, error) {
	switch cfg.EphemeralStore {
	case "memory":
		return runtime.NewMemoryRuntime(), nil
	case "valkey":
		return runtime.NewValkeyRuntime(ctx, cfg.ValkeyURL)
	default:
		return nil, fmt.Errorf("WA_APP_EPHEMERAL_STORE must be memory or valkey")
	}
}
