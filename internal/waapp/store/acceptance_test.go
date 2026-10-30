package store_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/store"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

type repository interface {
	account.Repository
	communication.StateRepository
	Close()
}

func TestAcceptance(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			for name, test := range map[string]func(*testing.T, repository, func() repository){
				"durable_retry": testDurableRetry, "ios_durable_retry": testIOSDurableRetry, "rollback": testRollback, "concurrent_retry": testConcurrentRetry, "concurrent_accounts": testConcurrentAccounts, "cancellation": testAcceptanceCancellation,
			} {
				t.Run(name, func(t *testing.T) { repo, reopen := openRepository(t, backend); test(t, repo, reopen) })
			}
		})
	}
}

func testDurableRetry(t *testing.T, repo repository, reopen func() repository) {
	testDurableRetryWithMaterial(t, repo, reopen, testMaterial(t))
}

func testDurableRetryWithMaterial(t *testing.T, repo repository, reopen func() repository, material *waappv1.AcceptedAccountMaterial) {
	t.Helper()
	ctx := t.Context()
	application := account.New(repo, nil, nil, nil, nil, nil)
	request := &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "durable-retry"}, Material: material}
	first, err := application.AcceptAccount(ctx, request)
	if err != nil || first.GetError() != nil || first.GetAccount() == nil {
		t.Fatalf("initial acceptance failed: %s", first.GetError().GetMessage())
	}
	login, err := repo.GetActiveLoginState(ctx, first.GetAccount().GetWaAccountId(), "")
	if err != nil {
		t.Fatal(err)
	}
	credentials, recovery, err := repo.GetProtocolState(ctx, login.GetClientProfileId())
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(credentials, material.GetCredentials()) || !proto.Equal(recovery, material.GetRecovery()) {
		t.Fatal("accepted protocol material changed")
	}
	restored, err := state.Restore(credentials, recovery)
	if err != nil {
		t.Fatal(err)
	}
	if restored.AuthKey != "" || restored.Profile.FDID != "" || len(restored.LastCodeParams) != 0 {
		t.Fatal("registration preparation leaked into runtime state")
	}
	changed := proto.Clone(first.GetAccount()).(*waappv1.WAAccount)
	changed.DisplayName = "updated"
	if err := repo.SaveWAAccount(ctx, changed); err != nil {
		t.Fatal(err)
	}
	repo.Close()
	repo = reopen()
	credentials, recovery, err = repo.GetProtocolState(ctx, login.GetClientProfileId())
	if err != nil || !proto.Equal(credentials, material.GetCredentials()) || !proto.Equal(recovery, material.GetRecovery()) {
		t.Fatal("restart lost account credentials or platform metadata")
	}
	application = account.New(repo, nil, nil, nil, nil, nil)
	repeated, err := application.AcceptAccount(ctx, request)
	if err != nil || repeated.GetError() != nil || !proto.Equal(first, repeated) {
		t.Fatal("retry after restart did not return the original result")
	}
	material = proto.Clone(material).(*waappv1.AcceptedAccountMaterial)
	material.Recovery.PushName = "different"
	request.Material = material
	rejected, _ := application.AcceptAccount(ctx, request)
	if rejected.GetError().GetCode() != waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT {
		t.Fatal("request ID reuse with different material was accepted")
	}
}

type failingRepository struct {
	account.Repository
	profileID string
}
type failingTransaction struct {
	account.AccountTransaction
	owner *failingRepository
}

func (f *failingRepository) WithinAccountTransaction(ctx context.Context, id, phone string, run func(account.AccountTransaction) error) error {
	return f.Repository.WithinAccountTransaction(ctx, id, phone, func(tx account.AccountTransaction) error { return run(&failingTransaction{tx, f}) })
}
func (f *failingTransaction) SaveClientProfile(ctx context.Context, profile *waappv1.ClientProfile) error {
	f.owner.profileID = profile.GetClientProfileId()
	return f.AccountTransaction.SaveClientProfile(ctx, profile)
}
func (f *failingTransaction) SaveAccountAcceptance(context.Context, string, *waappv1.AccountAcceptanceRecord) error {
	return errors.New("injected receipt failure")
}

func testRollback(t *testing.T, repo repository, _ func() repository) {
	failed := &failingRepository{Repository: repo}
	application := account.New(failed, nil, nil, nil, nil, nil)
	material := testMaterial(t)
	response, err := application.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "rollback"}, Material: material})
	if err != nil || response.GetError() == nil || response.GetAccount() != nil {
		t.Fatal("injected failure did not fail acceptance")
	}
	if failed.profileID == "" {
		t.Fatal("receipt failure was not reached")
	}
	if _, err := repo.FindWAAccountByPhone(t.Context(), material.GetPhone().GetE164Number()); !wamodel.IsWAAccountNotFound(err) {
		t.Fatal("account survived rollback")
	}
	if _, _, err := repo.GetProtocolState(t.Context(), failed.profileID); err == nil {
		t.Fatal("protocol material survived rollback")
	}
	if _, err := repo.GetAccountAcceptance(t.Context(), "rollback"); !wamodel.IsWAAccountNotFound(err) {
		t.Fatal("receipt survived rollback")
	}
	logins, err := repo.ListActiveLoginStates(t.Context())
	if err != nil || len(logins) != 0 {
		t.Fatal("login state survived rollback")
	}
}

func testConcurrentRetry(t *testing.T, repo repository, _ func() repository) {
	request := &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "concurrent-retry"}, Material: testMaterial(t)}
	responses := make(chan *waappv1.AcceptAccountResponse, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			response, _ := account.New(repo, nil, nil, nil, nil, nil).AcceptAccount(t.Context(), request)
			responses <- response
		})
	}
	workers.Wait()
	close(responses)
	var first *waappv1.AcceptAccountResponse
	for response := range responses {
		if response.GetError() != nil || response.GetAccount() == nil {
			t.Fatal("concurrent retry failed")
		}
		if first == nil {
			first = response
		} else if !proto.Equal(first, response) {
			t.Fatal("concurrent retry returned another result")
		}
	}
	logins, err := repo.ListActiveLoginStates(t.Context())
	if err != nil || len(logins) != 1 {
		t.Fatal("concurrent retry created multiple login states")
	}
}

func testConcurrentAccounts(t *testing.T, repo repository, _ func() repository) {
	material := testMaterial(t)
	responses := make(chan *waappv1.AcceptAccountResponse, 2)
	var workers sync.WaitGroup
	for _, id := range []string{"first", "second"} {
		workers.Go(func() {
			response, _ := account.New(repo, nil, nil, nil, nil, nil).AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: id}, Material: material})
			responses <- response
		})
	}
	workers.Wait()
	close(responses)
	var accountID string
	for response := range responses {
		if response.GetError() != nil || response.GetAccount() == nil {
			t.Fatal("concurrent account handoff failed")
		}
		if accountID != "" && accountID != response.GetAccount().GetWaAccountId() {
			t.Fatal("concurrent handoff duplicated the account")
		}
		accountID = response.GetAccount().GetWaAccountId()
	}
	logins, err := repo.ListActiveLoginStates(t.Context())
	if err != nil || len(logins) != 1 || logins[0].GetWaAccountId() != accountID {
		t.Fatal("concurrent handoff retained multiple active identities")
	}
}

func testAcceptanceCancellation(t *testing.T, repo repository, _ func() repository) {
	ctx, cancel := context.WithCancel(t.Context())
	material := testMaterial(t)
	err := repo.WithinAccountTransaction(ctx, "cancelled", material.GetPhone().GetE164Number(), func(tx account.AccountTransaction) error {
		now := (shared.SystemClock{}).Now()
		record := wamodel.NewWAAccount("waacc_cancelled", "", material.GetPhone(), waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_ACTIVE, shared.NewAuditStamp(now, now))
		if err := tx.SaveWAAccount(ctx, record); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if err == nil {
		t.Fatal("cancelled transaction committed")
	}
	if _, err := repo.FindWAAccountByPhone(t.Context(), material.GetPhone().GetE164Number()); !wamodel.IsWAAccountNotFound(err) {
		t.Fatal("cancelled account survived rollback")
	}
}

func testMaterial(t *testing.T) *waappv1.AcceptedAccountMaterial {
	t.Helper()
	phone := &waappv1.PhoneTarget{E164Number: "+12025550123", CountryCallingCode: "1", NationalNumber: "2025550123", CountryIso2: "US"}
	profile := protocoltest.AndroidBuild(t)
	parameters := protocoltest.AndroidClient()
	parameters.Options.EnablePqRegistration = true
	flow, err := onboarding.NewAndroidFlow(t.Context(), phone, profile, parameters, "", onboarding.AndroidDependencies{})
	if err != nil {
		t.Fatal("generate protocol material failed")
	}
	defer flow.Close()
	material, err := flow.Material("12025550123", "test-account")
	if err != nil {
		t.Fatal("export protocol material failed")
	}
	material.Recovery.RegistrationJid = "12025550123@s.whatsapp.net"
	material.Recovery.Sessions = map[string]*protocolv1.SignalSession{"peer": {Sender: "peer", RootKey: "synthetic-root", ReceiverChains: map[string]*protocolv1.SignalChain{"chain": {Index: 7, RootKey: "synthetic-chain"}}}}
	if err := communication.ValidateMaterial(material); err != nil {
		t.Fatal("synthetic account material is invalid")
	}
	return material
}

func openRepository(t *testing.T, backend string) (repository, func() repository) {
	t.Helper()
	if backend == "sqlite" {
		path := filepath.Join(t.TempDir(), "accounts.sqlite3")
		reopen := func() repository {
			repo, err := store.NewSQLiteStoreFile(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(repo.Close)
			return repo
		}
		return reopen(), reopen
	}
	address := postgresTestDSN(t)
	reopen := func() repository {
		repo, err := store.NewPostgresStore(t.Context(), address)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(repo.Close)
		return repo
	}
	return reopen(), reopen
}

func postgresTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("WA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("WA_TEST_PG_DSN is not configured")
	}
	control, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "wa_test_" + shared.StableID(t.Name())
	if _, err := control.Exec(t.Context(), "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = control.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		_ = control.Close(context.Background())
	})
	address, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := address.Query()
	query.Set("search_path", schema)
	address.RawQuery = query.Encode()
	return address.String()
}
