package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type dashboardHTTP struct {
	staticDir string
	service   *account.Application
	adding    waappv1.WaAccountAddServiceClient
}

type Config struct {
	ListenAddr string
	StaticDir  string
	Password   string
}

func Serve(ctx context.Context, cfg Config, service *account.Application, adding waappv1.WaAccountAddServiceClient) error {
	listenAddr, staticDir := cfg.ListenAddr, cfg.StaticDir
	auth := newDashboardAuthConfig(cfg.Password)
	if strings.TrimSpace(listenAddr) == "" {
		return nil
	}
	server := &dashboardHTTP{
		staticDir: firstNonEmpty(staticDir, "/app/dashboard/wa"),
		service:   service,
		adding:    adding,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/wa/health", server.handleHealth)
	mux.HandleFunc("/api/wa/account-add", server.handleAccountAdd)
	mux.HandleFunc("/api/wa/account-add/", server.handleAccountAdd)
	mux.HandleFunc("/api/wa/account-settings/2fa/status", server.handleGetTwoFactorAuthStatus)
	mux.HandleFunc("/api/wa/account-settings/2fa", server.handleSetTwoFactorAuthSettings)
	mux.HandleFunc("/api/wa/account-settings/email", server.handleSetAccountEmail)
	mux.HandleFunc("/api/wa/account-settings/email/otp/request", server.handleRequestAccountEmailOtp)
	mux.HandleFunc("/api/wa/account-settings/email/otp/verify", server.handleVerifyAccountEmailOtp)
	mux.HandleFunc("/api/wa/account-settings/profile/name", server.handleSetAccountProfileName)
	mux.HandleFunc("/api/wa/account-settings/profile/picture", server.handleSetAccountProfilePicture)
	mux.HandleFunc("/api/wa/account-settings/profile/picture/remove", server.handleRemoveAccountProfilePicture)
	mux.HandleFunc("/api/wa/accounts", server.handleAccounts)
	mux.HandleFunc("/api/wa/accounts/", server.handleAccount)
	mux.HandleFunc("/api/wa/account-otp-messages", server.handleAccountOTPMessages)
	mux.HandleFunc("/api/wa/messages/read", server.handleMarkMessagesRead)
	mux.HandleFunc("/api/wa/messages/delete", server.handleDeleteMessages)
	mux.HandleFunc("/api/wa/messages/send", server.handleSendTextMessage)
	mux.HandleFunc("/api/wa/messages/edit", server.handleEditMessage)
	mux.HandleFunc("/api/wa/messages/react", server.handleReactToMessage)
	mux.HandleFunc("/api/wa/messages", server.handleMessages)
	mux.HandleFunc("/api/wa/contacts/resolve", server.handleResolveContacts)
	mux.HandleFunc("/api/wa/contacts/presence", server.handleContactPresence)
	mux.HandleFunc("/api/wa/groups", server.handleGroup)
	mux.HandleFunc("/api/wa/groups/", server.handleGroupAction)
	mux.HandleFunc("/api/wa/contacts/", server.handleContactResource)
	mux.HandleFunc("/api/wa/contacts", server.handleContacts)
	mux.HandleFunc("/api/wa/long-connections", server.handleLongConnections)
	mux.HandleFunc("/mf/wa/", http.NotFound)
	mux.HandleFunc("/healthz", server.handleHealth)
	mux.Handle("/", standaloneDashboard(server.staticDir))
	handler := withCORS(withOptionalDashboardAuth(mux, auth))
	httpServer := &http.Server{Addr: listenAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	finished := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
		case <-finished:
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			_ = httpServer.Close()
		}
	}()
	log.Printf("wa-app dashboard BFF listening on %s", listenAddr)
	err := httpServer.ListenAndServe()
	close(finished)
	<-shutdownDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("wa-app dashboard BFF failed: %w", err)
	}
	return nil
}
