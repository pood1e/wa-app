package store_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/runtime"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type logoutConnection struct {
	wacore.Communication
	started chan string
	closed  atomic.Bool
}

func (c *logoutConnection) ReceiveMessageBatch(ctx context.Context, input wacore.EngineMessageInput) wacore.EngineMessageBatchResult {
	c.started <- input.MessageSessionID
	<-ctx.Done()
	return wacore.EngineMessageBatchResult{Err: ctx.Err()}
}
func (c *logoutConnection) Close() error { c.closed.Store(true); return nil }

func startLogoutApp(t *testing.T, repo account.Repository) (*account.Application, *waappv1.WAAccount, *waappv1.LoginState, *logoutConnection, string) {
	t.Helper()
	ephemeral := runtime.NewMemoryRuntime()
	t.Cleanup(func() { _ = ephemeral.Close() })
	connection := &logoutConnection{started: make(chan string, 1)}
	app := account.New(repo, ephemeral, connection, func(context.Context, wacore.EngineMessageInput) (wacore.Communication, error) { return connection, nil }, nil, nil)
	response, err := app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "logout-fixture"}, Material: testMaterial(t)})
	if err != nil || response.GetError() != nil {
		t.Fatal("seed account failed")
	}
	record := response.GetAccount()
	record.DisplayName = "retained account"
	record.TwoFactorAuth = &waappv1.TwoFactorAuthStatus{Configured: true}
	if err := repo.SaveWAAccount(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	login, err := repo.GetActiveLoginState(t.Context(), record.GetWaAccountId(), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); _ = app.RunLongConnections(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case sessionID := <-connection.started:
		return app, record, login, connection, sessionID
	case <-time.After(5 * time.Second):
		t.Fatal("owned connection did not start")
		return nil, nil, nil, nil, ""
	}
}

func TestLogoutPreservesAccountAndHistory(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, reopen := openRepository(t, backend)
			app, record, login, connection, sessionID := startLogoutApp(t, repo)
			credentials, recovery, err := repo.GetProtocolState(t.Context(), login.GetClientProfileId())
			if err != nil {
				t.Fatal(err)
			}
			contact := &waappv1.WAContact{ContactId: "history-contact", WaAccountId: record.GetWaAccountId(), Jid: "12025550999@s.whatsapp.net", DisplayName: "retained contact"}
			if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{contact}); err != nil {
				t.Fatal(err)
			}
			message := &waappv1.InboundMessage{
				MessageId: "history-message", MessageSessionId: sessionID,
				Kind:            waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE,
				EncryptionState: waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_PLAINTEXT,
				Direction:       waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND,
				Source:          waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_LONG_CONNECTION,
				DeleteStatus:    waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_NOT_DELETED,
				SenderRef:       contact.GetJid(), ContactRef: contact.GetJid(),
				PayloadRef: "plaintext:retained history", ReceivedAt: timestamppb.New(time.Now().UTC().Truncate(time.Microsecond)),
			}
			if err := repo.SaveInboundMessages(t.Context(), []*waappv1.InboundMessage{message}); err != nil {
				t.Fatal(err)
			}
			contact, err = repo.GetWAContact(t.Context(), contact.GetContactId())
			if err != nil {
				t.Fatal(err)
			}
			request := &waappv1.LogoutWAAccountRequest{WaAccountId: record.GetWaAccountId()}
			for range 2 {
				response, err := app.LogoutWAAccount(t.Context(), request)
				if err != nil || response.GetError() != nil || response.GetAccount().GetStatus() != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_LOGGED_OUT || !connection.closed.Load() {
					t.Fatal("logout did not persist state and join the owned connection")
				}
			}
			if err := repo.SaveLoginState(t.Context(), login); shared.ToProtoError(err).GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT {
				t.Fatal("stale heartbeat reactivated the logged-out identity")
			}
			app.AccountTransferredOut(t.Context(), login, errors.New("late remote logout"))
			restored := reopen()
			current, err := restored.GetWAAccount(t.Context(), record.GetWaAccountId())
			if err != nil || current.GetStatus() != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_LOGGED_OUT || current.GetDisplayName() != record.GetDisplayName() || !proto.Equal(current.GetPhone(), record.GetPhone()) || !proto.Equal(current.GetTwoFactorAuth(), record.GetTwoFactorAuth()) {
				t.Fatal("logout lost account details or a late event changed its status")
			}
			active, err := restored.ListActiveLoginStates(t.Context())
			if err != nil || len(active) != 0 {
				t.Fatal("logged-out account would reconnect after restart")
			}
			storedCredentials, storedRecovery, err := restored.GetProtocolState(t.Context(), login.GetClientProfileId())
			if err != nil || !proto.Equal(credentials, storedCredentials) || !proto.Equal(recovery, storedRecovery) {
				t.Fatal("logout changed retained protocol material")
			}
			storedContact, err := restored.GetWAContact(t.Context(), contact.GetContactId())
			if err != nil || !proto.Equal(storedContact, contact) {
				t.Fatal("logout removed the contact")
			}
			storedMessage, err := restored.GetInboundMessage(t.Context(), message.GetMessageId())
			if err != nil || !proto.Equal(storedMessage, message) {
				t.Fatal("logout changed retained message history", err)
			}
			closed, err := restored.GetMessageSession(t.Context(), sessionID)
			if err != nil || closed.GetStatus() != waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_CLOSED {
				t.Fatal("logout left a persistent message session open")
			}
			check, err := app.CheckLoginState(t.Context(), &waappv1.CheckLoginStateRequest{LoginStateId: login.GetLoginStateId()})
			if err != nil || check.GetError() == nil {
				t.Fatal("login check revived an explicitly logged-out identity")
			}
			read, err := app.MarkAccountMessagesRead(t.Context(), &waappv1.MarkAccountMessagesReadRequest{WaAccountId: record.GetWaAccountId(), ContactRef: contact.GetJid(), LocalOnly: true})
			if err != nil || read.GetError() != nil || read.GetUpdatedCount() != 1 || read.GetReceiptSentCount() != 0 {
				t.Fatal("logged-out account could not read local history without network access")
			}
			replacement, err := account.New(restored, nil, nil, nil, nil, nil).AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "login-again"}, Material: testMaterial(t)})
			if err != nil || replacement.GetError() != nil || replacement.GetAccount().GetWaAccountId() != record.GetWaAccountId() || replacement.GetAccount().GetStatus() != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_ACTIVE {
				t.Fatal("adding the same phone after logout did not restore the existing account")
			}
			if _, err := restored.GetInboundMessage(t.Context(), message.GetMessageId()); err != nil {
				t.Fatal("logging in again removed retained history")
			}
		})
	}
}

type logoutRepository struct {
	repository
	fail        bool
	afterCommit func()
}
type logoutTransaction struct{ account.AccountTransaction }

func (logoutTransaction) SaveWAAccount(context.Context, *waappv1.WAAccount) error {
	return errors.New("fixture logout transaction failure")
}
func (r logoutRepository) WithinAccountTransaction(ctx context.Context, id, phone string, run func(account.AccountTransaction) error) error {
	if !strings.HasPrefix(id, "account-logout/") {
		return r.repository.WithinAccountTransaction(ctx, id, phone, run)
	}
	err := r.repository.WithinAccountTransaction(ctx, id, phone, func(tx account.AccountTransaction) error {
		if r.fail {
			return run(logoutTransaction{tx})
		}
		return run(tx)
	})
	if err == nil && r.afterCommit != nil {
		r.afterCommit()
	}
	return err
}

func TestLogoutTransactionAndCancellation(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			for _, fail := range []bool{false, true} {
				name := "cancel_after_commit"
				if fail {
					name = "rollback"
				}
				t.Run(name, func(t *testing.T) {
					repo, _ := openRepository(t, backend)
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					app, record, _, connection, _ := startLogoutApp(t, logoutRepository{repository: repo, fail: fail, afterCommit: cancel})
					response, err := app.LogoutWAAccount(ctx, &waappv1.LogoutWAAccountRequest{WaAccountId: record.GetWaAccountId()})
					if err != nil || (response.GetError() != nil) != fail || connection.closed.Load() == fail {
						t.Fatal("logout did not respect transaction outcome or complete cleanup after cancellation")
					}
					_, err = repo.GetActiveLoginState(t.Context(), record.GetWaAccountId(), "")
					if (err == nil) != fail {
						t.Fatal("logout left an inconsistent active login state")
					}
				})
			}
		})
	}
}
