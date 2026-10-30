package account

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/runtime"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type pictureRepository struct{ Repository }

func (pictureRepository) ListActiveLoginStates(context.Context) ([]*waappv1.LoginState, error) {
	return []*waappv1.LoginState{{WaAccountId: "waacc_cached", Status: waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE}}, nil
}
func (pictureRepository) GetWAAccount(context.Context, string) (*waappv1.WAAccount, error) {
	return &waappv1.WAAccount{WaAccountId: "waacc_cached", Phone: &waappv1.PhoneTarget{E164Number: "+12025550123"}}, nil
}
func (pictureRepository) GetWAContact(context.Context, string) (*waappv1.WAContact, error) {
	return &waappv1.WAContact{ContactId: "wact_cached", WaAccountId: "waacc_cached", ProfilePictureId: "fixture-picture"}, nil
}

func TestCachedPicturesRemainAvailableWhenRemoteLookupsAreBusy(t *testing.T) {
	ephemeral := runtime.NewMemoryRuntime()
	defer ephemeral.Close()
	application := New(pictureRepository{}, ephemeral, nil, nil, nil, nil)
	picture := WAContactProfilePicture{ProfilePictureID: "fixture-picture", ContentType: "image/jpeg", Data: []byte("fixture-image")}
	application.cacheWAAccountProfilePicture(t.Context(), "waacc_cached", picture)
	application.cacheWAContactProfilePicture(t.Context(), "wact_cached", picture)
	first, err := application.acquireProfilePictureLookup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	second, err := application.acquireProfilePictureLookup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	account, err := application.GetWAAccountProfilePicture(t.Context(), "waacc_cached")
	if err != nil || string(account.Data) != "fixture-image" {
		t.Fatal("busy remote lookups hid the cached account picture")
	}
	contact, err := application.GetWAContactProfilePicture(t.Context(), "wact_cached")
	if err != nil || string(contact.Data) != "fixture-image" {
		t.Fatal("busy remote lookups hid the cached contact picture")
	}
	application.deleteWAAccountProfilePictureCache(t.Context(), "waacc_cached")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = application.GetWAAccountProfilePicture(ctx, "waacc_cached")
	if !errors.Is(err, context.Canceled) {
		t.Fatal("uncached lookup ignored caller cancellation")
	}
}

func TestPictureLookupQueuesAndCancelsWithoutLosingSlots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		application := New(pictureRepository{}, nil, nil, nil, nil, nil)
		first, err := application.acquireProfilePictureLookup(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		second, err := application.acquireProfilePictureLookup(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer second()
		acquired := make(chan func(), 1)
		go func() {
			finish, err := application.acquireProfilePictureLookup(t.Context())
			if err != nil {
				t.Error(err)
			}
			acquired <- finish
		}()
		synctest.Wait()
		select {
		case <-acquired:
			t.Fatal("a saturated picture lookup did not wait for capacity")
		default:
		}
		first()
		finish := <-acquired
		if finish == nil {
			t.Fatal("queued picture lookup failed after capacity became available")
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, err := application.acquireProfilePictureLookup(ctx); done <- err }()
		synctest.Wait()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("queued picture lookup ignored cancellation", err)
		}
		finish()
		next, err := application.acquireProfilePictureLookup(t.Context())
		if err != nil {
			t.Fatal("canceled wait retained a picture lookup slot", err)
		}
		next()
	})
}

func TestPictureLookupLimitReleasesAndHonorsCancellation(t *testing.T) {
	application := New(pictureRepository{}, nil, nil, nil, nil, nil)
	for range 3 {
		_, err := application.GetWAAccountProfilePicture(t.Context(), "waacc_cached")
		if err == nil || shared.ToProtoError(err).GetCode() == waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED {
			t.Fatal("a failed lookup retained its remote slot")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if finish, err := application.acquireProfilePictureLookup(ctx); err == nil || finish != nil {
		t.Fatal("cancelled caller acquired a remote slot")
	}
}

func TestContactPictureCacheRetainsDiscoveredVersion(t *testing.T) {
	ephemeral := runtime.NewMemoryRuntime()
	defer ephemeral.Close()
	application := New(pictureRepository{}, ephemeral, nil, nil, nil, nil)
	picture := WAContactProfilePicture{ProfilePictureID: "discovered", ContentType: "image/jpeg", Data: []byte("fixture-image")}
	application.cacheWAContactProfilePicture(t.Context(), "contact", picture)
	for _, version := range []string{"", "discovered"} {
		cached, ok := application.cachedWAContactProfilePicture(t.Context(), &waappv1.WAContact{ContactId: "contact", ProfilePictureId: version})
		if !ok || string(cached.Data) != "fixture-image" || cached.ProfilePictureID != "discovered" {
			t.Fatal("a picture requested before metadata refresh missed the discovered image")
		}
	}
	if _, ok := application.cachedWAContactProfilePicture(t.Context(), &waappv1.WAContact{ContactId: "contact", ProfilePictureId: "changed"}); ok {
		t.Fatal("a newer picture version returned a stale cached image")
	}
}
