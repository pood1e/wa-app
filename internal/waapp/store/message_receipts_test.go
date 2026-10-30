package store_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func TestReceiptFactsSurviveDuplicatesReorderingAndRestart(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, reopen := openRepository(t, backend)
			app := account.New(repo, nil, nil, nil, nil, nil)
			accepted, err := app.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "receipt-fixture"}, Material: testMaterial(t)})
			if err != nil || accepted.GetError() != nil {
				t.Fatal("seed account failed")
			}
			id := accepted.GetAccount().GetWaAccountId()
			now := time.Now().UTC().Truncate(time.Millisecond)
			read := &waappv1.MessageReceipt{ProviderMessageId: "same-id", ContactRef: "120363000001@g.us", RecipientRef: "12025550124@s.whatsapp.net", Type: waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_READ, Timestamp: timestamppb.New(now)}
			later := proto.Clone(read).(*waappv1.MessageReceipt)
			later.Timestamp = timestamppb.New(now.Add(time.Minute))
			delivery := proto.Clone(read).(*waappv1.MessageReceipt)
			delivery.Type = waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_DELIVERED
			delivery.Timestamp = timestamppb.New(now.Add(-time.Minute))
			otherMember := proto.Clone(read).(*waappv1.MessageReceipt)
			otherMember.RecipientRef = "12025550125@s.whatsapp.net"
			otherChat := proto.Clone(read).(*waappv1.MessageReceipt)
			otherChat.ContactRef = "120363000002@g.us"
			if err := repo.SaveMessageReceipts(t.Context(), id, []*waappv1.MessageReceipt{read, later, delivery, otherMember, otherChat}); err != nil {
				t.Fatal(err)
			}
			otherAccount := &waappv1.WAAccount{WaAccountId: "fixture-other", Phone: &waappv1.PhoneTarget{E164Number: "+12025550126"}, Audit: shared.NewAuditStamp(now, now)}
			if err := repo.SaveWAAccount(t.Context(), otherAccount); err != nil {
				t.Fatal(err)
			}
			if err := repo.SaveMessageReceipts(t.Context(), otherAccount.GetWaAccountId(), []*waappv1.MessageReceipt{read}); err != nil {
				t.Fatal(err)
			}
			repo.Close()
			repo = reopen()
			facts, err := repo.ListMessageReceipts(t.Context(), id, []string{"same-id"})
			if err != nil || len(facts) != 4 {
				t.Fatal("receipt persistence, idempotency or account scoping failed", err)
			}
			for _, fact := range facts {
				if fact.GetType() == waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_READ && !fact.GetTimestamp().AsTime().Equal(now) {
					t.Fatal("a duplicate changed the first read timestamp")
				}
			}
			if err := repo.DeleteWAAccount(t.Context(), id); err != nil {
				t.Fatal(err)
			}
			facts, err = repo.ListMessageReceipts(t.Context(), id, []string{"same-id"})
			if err != nil || len(facts) != 0 {
				t.Fatal("account deletion retained receipt facts")
			}
			facts, err = repo.ListMessageReceipts(t.Context(), otherAccount.GetWaAccountId(), []string{"same-id"})
			if err != nil || len(facts) != 1 {
				t.Fatal("account deletion removed another account's receipts")
			}
		})
	}
}
