package store_test

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/store"
)

type heldDeletion struct {
	repository
	entered, release chan struct{}
}

func (s heldDeletion) WithinAccountTransaction(ctx context.Context, id, phone string, run func(account.AccountTransaction) error) error {
	if strings.HasPrefix(id, "delete/") {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.repository.WithinAccountTransaction(ctx, id, phone, run)
}

func TestReacceptanceWaitsForAccountDeletion(t *testing.T) {
	material := testMaterial(t)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		repo, err := store.NewSQLiteStoreFile(ctx, ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer repo.Close()
		held := heldDeletion{repository: repo, entered: make(chan struct{}), release: make(chan struct{})}
		app := account.New(held, nil, nil, nil, nil, nil)
		first, err := app.AcceptAccount(ctx, &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "old-account"}, Material: material})
		if err != nil || first.GetError() != nil {
			t.Fatal("cannot seed old account")
		}
		record := first.GetAccount()
		record.Status = waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_TRANSFERRED_OUT
		if err := repo.SaveWAAccount(ctx, record); err != nil {
			t.Fatal(err)
		}
		login, err := repo.GetActiveLoginState(ctx, record.GetWaAccountId(), "")
		if err != nil {
			t.Fatal(err)
		}
		login.Status = waappv1.LoginStateStatus_LOGIN_STATE_STATUS_REVOKED
		if err := repo.SaveLoginState(ctx, login); err != nil {
			t.Fatal(err)
		}
		deleted := make(chan *waappv1.DeleteWAAccountResponse, 1)
		go func() {
			result, _ := app.DeleteWAAccount(ctx, &waappv1.DeleteWAAccountRequest{WaAccountId: record.GetWaAccountId()})
			deleted <- result
		}()
		<-held.entered
		accepted := make(chan *waappv1.AcceptAccountResponse, 1)
		go func() {
			result, _ := app.AcceptAccount(ctx, &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "new-account"}, Material: material})
			accepted <- result
		}()
		synctest.Wait()
		select {
		case <-accepted:
			t.Fatal("acceptance raced with deletion of the previous identity")
		default:
		}
		close(held.release)
		if result := <-deleted; result.GetError() != nil {
			t.Fatal("deletion failed")
		}
		result := <-accepted
		if result.GetError() != nil || result.GetAccount() == nil {
			t.Fatal("reacceptance failed after deletion")
		}
		if _, err := repo.GetActiveLoginState(ctx, record.GetWaAccountId(), ""); err != nil {
			t.Fatal("deletion removed the newly accepted identity")
		}
	})
}
