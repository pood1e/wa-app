package store_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/store"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func TestPostgresContactSavedNameUpgrade(t *testing.T) {
	dsn := postgresTestDSN(t)
	open := func() *store.PostgresStore {
		repo, err := store.NewPostgresStore(t.Context(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(repo.Close)
		return repo
	}
	repo := open()
	const accountID = "waacc_contact_upgrade"
	if err := repo.SaveWAAccount(t.Context(), &waappv1.WAAccount{WaAccountId: accountID, Phone: &waappv1.PhoneTarget{E164Number: "+12025550123"}}); err != nil {
		t.Fatal(err)
	}
	contact := wamodel.ContactFromRef(accountID, "12025550124@s.whatsapp.net", time.Now(), time.Now())
	contact.DisplayName = "Existing contact"
	if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{contact}); err != nil {
		t.Fatal(err)
	}
	repo.Close()
	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(t.Context())
	if _, err := conn.Exec(t.Context(), `ALTER TABLE wa_contacts DROP COLUMN saved_name`); err != nil {
		t.Fatal(err)
	}
	repo = open()
	stored, err := repo.GetWAContact(t.Context(), contact.GetContactId())
	if err != nil || stored.GetDisplayName() != "Existing contact" || stored.GetSavedName() != "" {
		t.Fatal("contact schema upgrade changed an existing contact", err)
	}
	stored.SavedName = "Saved contact"
	if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{stored}); err != nil {
		t.Fatal(err)
	}
	repo.Close()
	repo = open()
	stored, err = repo.GetWAContactByRef(t.Context(), accountID, contact.GetJid())
	if err != nil || stored.GetSavedName() != "Saved contact" || stored.GetDisplayName() != "Saved contact" {
		t.Fatal("saved contact name did not survive reopening the upgraded store", err)
	}
}
