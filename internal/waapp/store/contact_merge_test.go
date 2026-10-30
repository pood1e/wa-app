package store_test

import (
	"testing"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func TestContactUpdatesKeepNamesAndRefreshPictures(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, _ := openRepository(t, backend)
			accepted, _ := account.New(repo, nil, nil, nil, nil, nil).AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "contact-merge"}, Material: testMaterial(t)})
			if accepted.GetError() != nil {
				t.Fatal("cannot seed account")
			}
			id := accepted.GetAccount().GetWaAccountId()
			now := time.Now().UTC()
			group := wamodel.ContactFromRef(id, "120363000001@g.us", now, now)
			group.DisplayName, group.ProfilePictureId = "Named group", "first"
			if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{group}); err != nil {
				t.Fatal(err)
			}
			fallback := wamodel.ContactFromRef(id, group.GetJid(), now, now)
			fallback.ProfilePictureId = "second"
			if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{fallback}); err != nil {
				t.Fatal(err)
			}
			stored, err := repo.GetWAContact(t.Context(), group.GetContactId())
			if err != nil || stored.GetDisplayName() != "Named group" || stored.GetProfilePictureId() != "second" {
				t.Fatal("incoming messages overwrote the group name or prevented picture refresh", err)
			}
			group.DisplayName, group.ProfilePictureId = "Renamed group", ""
			if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{group}); err != nil {
				t.Fatal(err)
			}
			stored, err = repo.GetWAContact(t.Context(), group.GetContactId())
			if err != nil || stored.GetDisplayName() != "Renamed group" || stored.GetProfilePictureId() != "second" {
				t.Fatal("group rename lost existing contact metadata", err)
			}
			phone := wamodel.ContactFromRef(id, "15550000001@s.whatsapp.net", now, now)
			phone.SavedName = "Saved contact"
			if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{phone}); err != nil {
				t.Fatal(err)
			}
			incoming := wamodel.ContactFromRef(id, phone.GetJid(), now, now)
			incoming.DisplayName = "Remote profile"
			if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{incoming}); err != nil {
				t.Fatal(err)
			}
			stored, err = repo.GetWAContact(t.Context(), phone.GetContactId())
			if err != nil || stored.GetSavedName() != "Saved contact" || stored.GetDisplayName() != "Saved contact" {
				t.Fatal("remote metadata replaced the saved contact name", err)
			}
			phone.SavedName = "Renamed contact"
			if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{phone}); err != nil {
				t.Fatal(err)
			}
			stored, err = repo.GetWAContact(t.Context(), phone.GetContactId())
			if err != nil || stored.GetDisplayName() != "Renamed contact" {
				t.Fatal("explicit contact rename was discarded", err)
			}
			lid := wamodel.ContactFromRef(id, "100001@lid", now, now)
			lid.Number = phone.GetNumber()
			if err := repo.SaveWAContacts(t.Context(), []*waappv1.WAContact{lid, phone}); err != nil {
				t.Fatal(err)
			}
			for _, ref := range []string{phone.GetNumber(), phone.GetJid(), lid.GetJid()} {
				stored, err = repo.GetWAContactByRef(t.Context(), id, ref)
				if err != nil || stored.GetJid() != lid.GetJid() {
					t.Fatal("phone alias did not resolve to the LID conversation", err)
				}
			}
		})
	}
}
