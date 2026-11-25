package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	modelv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/model/v1"
	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding/ios"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/store"
)

func testBusinessConversion(t *testing.T, repo repository, reopen func() repository) {
	testBusinessConversionPlatform(t, repo, reopen, false)
}

func testIOSBusinessConversion(t *testing.T, repo repository, reopen func() repository) {
	testBusinessConversionPlatform(t, repo, reopen, true)
}

func testBusinessConversionPlatform(t *testing.T, repo repository, reopen func() repository, iosClient bool) {
	t.Helper()
	personal := testMaterial(t)
	application := account.New(repo, nil, nil, nil, nil, nil)
	first, err := application.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "personal"}, Material: personal})
	if err != nil || first.GetError() != nil {
		t.Fatal("personal account acceptance failed", err, first.GetError())
	}
	original, err := repo.GetWAAccount(t.Context(), first.GetAccount().GetWaAccountId())
	if err != nil {
		t.Fatal(err)
	}
	oldLogin, err := repo.GetActiveLoginState(t.Context(), first.GetAccount().GetWaAccountId(), "")
	if err != nil {
		t.Fatal(err)
	}
	material := businessConversionMaterial(t, iosClient)
	converted, err := application.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "business"}, Material: material})
	if err != nil || converted.GetError() != nil {
		t.Fatal("business account acceptance failed", err, converted.GetError())
	}
	if converted.GetAccount().GetWaAccountId() != original.GetWaAccountId() || converted.GetAccount().GetDisplayName() != "WA Lab" || converted.GetAccount().GetAccountType() != modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS {
		t.Fatal("conversion replaced the account record or lost its confirmed business type")
	}
	if !proto.Equal(original.GetAudit().GetCreatedAt(), converted.GetAccount().GetAudit().GetCreatedAt()) {
		t.Fatal("conversion changed the account's persisted creation time")
	}
	repo.Close()
	repo = reopen()
	account, err := repo.GetWAAccount(t.Context(), first.GetAccount().GetWaAccountId())
	if err != nil || account.GetAccountType() != modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS {
		t.Fatal("restart lost the account's business type", err)
	}
	listed, _, err := repo.ListWAAccounts(t.Context(), "", 10)
	if err != nil || len(listed) != 1 || listed[0].GetAccountType() != modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS {
		t.Fatal("account listing lost the business type", err)
	}
	login, err := repo.GetActiveLoginState(t.Context(), account.GetWaAccountId(), "")
	if err != nil || login.GetLoginStateId() == oldLogin.GetLoginStateId() {
		t.Fatal("conversion retained the old active login", err)
	}
	credentials, recovery, err := repo.GetProtocolState(t.Context(), login.GetClientProfileId())
	if err != nil || !proto.Equal(credentials, material.GetCredentials()) || !proto.Equal(recovery, material.GetRecovery()) {
		t.Fatal("restart lost the business protocol state", err)
	}
	credentials, recovery, err = repo.GetProtocolState(t.Context(), oldLogin.GetClientProfileId())
	if err != nil || !proto.Equal(credentials, personal.GetCredentials()) || !proto.Equal(recovery, personal.GetRecovery()) {
		t.Fatal("conversion lost the previous identity used to read account history", err)
	}
}

func businessConversionMaterial(t *testing.T, iosClient bool) *waappv1.AcceptedAccountMaterial {
	t.Helper()
	material := testMaterial(t)
	material.Recovery.Client.AccountType = modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS
	material.Recovery.Client.AppVersion = "2.26.42.74"
	if iosClient {
		profile, err := ios.DefaultBusinessProfile()
		if err != nil {
			t.Fatal(err)
		}
		material.Recovery.Client = profile.GetClient()
	}
	material.Recovery.PushName = "WA Lab"
	certificate, err := wire.NewBusinessCertificate(material.GetCredentials().GetIdentity().GetPrivate(), "WA Lab")
	if err != nil {
		t.Fatal(err)
	}
	material.Recovery.BusinessIdentity = &protocolv1.BusinessIdentity{Certificate: certificate, CertificateId: "synthetic-business-certificate"}
	return material
}

func TestPostgresAccountTypeUpgrade(t *testing.T) {
	dsn := postgresTestDSN(t)
	repo, err := store.NewPostgresStore(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	personal := &waappv1.WAAccount{WaAccountId: "waacc_type_upgrade", DisplayName: "Existing account", Phone: &waappv1.PhoneTarget{E164Number: "+12025550123"}}
	if err := repo.SaveWAAccount(t.Context(), personal); err != nil {
		repo.Close()
		t.Fatal(err)
	}
	repo.Close()
	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(t.Context())
	if _, err := conn.Exec(t.Context(), `ALTER TABLE wa_accounts DROP COLUMN account_type`); err != nil {
		t.Fatal(err)
	}
	if obsolete, err := store.NewPostgresStore(t.Context(), dsn); err == nil {
		obsolete.Close()
		t.Fatal("store opened before the manual account type upgrade")
	}
	if _, err := conn.Exec(t.Context(), `ALTER TABLE wa_accounts ADD COLUMN account_type TEXT NOT NULL DEFAULT 'WA_ACCOUNT_TYPE_PERSONAL'`); err != nil {
		t.Fatal(err)
	}
	repo, err = store.NewPostgresStore(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)
	stored, err := repo.GetWAAccount(t.Context(), personal.GetWaAccountId())
	if err != nil || stored.GetDisplayName() != personal.GetDisplayName() || stored.GetAccountType() != modelv1.WAAccountType_WA_ACCOUNT_TYPE_PERSONAL {
		t.Fatal("manual upgrade changed the existing personal account", err)
	}
}
