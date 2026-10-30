package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/store"
)

func TestLegacyMigration(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		for _, corrupt := range []bool{false, true} {
			name := "preserves_material"
			if corrupt {
				name = "invalid_material_rolls_back"
			}
			t.Run(backend+"/"+name, func(t *testing.T) { testLegacyMigration(t, backend, corrupt, false) })
		}
	}
}

func TestLegacyLargeRecoveryState(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) { testLegacyMigration(t, backend, false, true) })
	}
}

func testLegacyMigration(t *testing.T, backend string, corrupt, large bool) {
	fixture := newMigrationFixture(t, backend)
	material := testMaterial(t)
	application := account.New(fixture.repo, nil, nil, nil, nil, nil)
	response, err := application.AcceptAccount(t.Context(), &waappv1.AcceptAccountRequest{Context: &waappv1.RequestContext{RequestId: "migration-fixture"}, Material: material})
	if err != nil || response.GetError() != nil {
		t.Fatal("cannot seed completed account")
	}
	before, err := fixture.repo.GetWAAccount(t.Context(), response.GetAccount().GetWaAccountId())
	if err != nil {
		t.Fatal(err)
	}
	logins, err := fixture.repo.ListActiveLoginStates(t.Context())
	if err != nil || len(logins) != 1 {
		t.Fatal("missing seeded login")
	}
	id := logins[0].GetClientProfileId()
	if large {
		material.Recovery.MessagePayloads = map[string]*protocolv1.MessagePayload{"fixture": {Payload: strings.Repeat("x", 4<<20)}}
	}
	old, err := state.Restore(material.GetCredentials(), material.GetRecovery())
	if err != nil {
		t.Fatal("restore synthetic state")
	}
	client := material.GetRecovery().GetClient()
	old.Profile = state.NativePhoneProfile{DeviceVendor: client.GetManufacturer(), DeviceModel: client.GetDevice(), AndroidVersion: client.GetOsVersion(), FDID: client.GetPhoneId(), ExpIDUUID: client.GetDeviceExperimentId(), AdditionalMapFields: map[string]string{"mcc": client.GetMcc(), "mnc": client.GetMnc()}}
	old.AuthKey = "discarded-preparation"
	old.LastCodeParams = map[string]string{"fixture": "discarded-attempt"}
	if corrupt {
		old.Signal.Identity.Private = "invalid"
	}
	payload, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	fixture.seedLegacy(t, id, payload)
	fixture.repo.Close()
	if err := fixture.reopenError(); !errors.Is(err, store.ErrLegacySchema) {
		t.Fatal("runtime accepted an unmigrated database")
	}
	err = fixture.migrate()
	if corrupt {
		if err == nil {
			t.Fatal("invalid keys passed migration")
		}
		if err := fixture.reopenError(); !errors.Is(err, store.ErrLegacySchema) {
			t.Fatal("failed migration removed the old schema")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	repo := fixture.open(t)
	defer repo.Close()
	keys, recovery, err := repo.GetProtocolState(t.Context(), id)
	if err != nil || !proto.Equal(keys, material.GetCredentials()) || !proto.Equal(recovery, material.GetRecovery()) {
		t.Fatal("keys or recovery counters changed during migration")
	}
	restored, err := state.Restore(keys, recovery)
	if err != nil || restored.AuthKey != "" || len(restored.LastCodeParams) != 0 || restored.Profile.FDID != "" {
		t.Fatal("preparation material survived migration")
	}
	account, err := repo.GetWAAccount(t.Context(), response.GetAccount().GetWaAccountId())
	if err != nil || !proto.Equal(account, before) {
		t.Fatal("account identity or metadata changed")
	}
	if err := fixture.migrate(); err == nil {
		t.Fatal("migration did not reject an already migrated database")
	}
}

type migrationFixture struct {
	repo        repository
	open        func(*testing.T) repository
	reopenError func() error
	migrate     func() error
	seedLegacy  func(*testing.T, string, []byte)
}

func newMigrationFixture(t *testing.T, backend string) migrationFixture {
	t.Helper()
	ctx := t.Context()
	if backend == "sqlite" {
		path := filepath.Join(t.TempDir(), "accounts.sqlite3")
		open := func(t *testing.T) repository {
			t.Helper()
			repo, err := store.NewSQLiteStoreFile(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			return repo
		}
		return migrationFixture{repo: open(t), open: open,
			reopenError: func() error {
				repo, err := store.NewSQLiteStoreFile(ctx, path)
				if repo != nil {
					repo.Close()
				}
				return err
			},
			migrate: func() error { return store.MigrateLegacySQLite(ctx, path) },
			seedLegacy: func(t *testing.T, id string, payload []byte) {
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				for _, query := range []string{
					`CREATE TABLE wa_sqlite_native_states(client_profile_id TEXT PRIMARY KEY,state_json TEXT NOT NULL,updated_at INTEGER NOT NULL)`,
					`CREATE TABLE wa_sqlite_runtime_state(key TEXT PRIMARY KEY)`,
					`DELETE FROM wa_sqlite_account_credentials`, `DELETE FROM wa_sqlite_protocol_states`, `DELETE FROM wa_sqlite_account_acceptances`,
				} {
					if _, err := db.ExecContext(ctx, query); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := db.ExecContext(ctx, `INSERT INTO wa_sqlite_native_states VALUES(?,?,0)`, id, payload); err != nil {
					t.Fatal(err)
				}
			},
		}
	}
	dsn := postgresTestDSN(t)
	open := func(t *testing.T) repository {
		t.Helper()
		repo, err := store.NewPostgresStore(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		return repo
	}
	return migrationFixture{repo: open(t), open: open,
		reopenError: func() error {
			repo, err := store.NewPostgresStore(ctx, dsn)
			if repo != nil {
				repo.Close()
			}
			return err
		},
		migrate: func() error { return store.MigrateLegacyPostgres(ctx, dsn) },
		seedLegacy: func(t *testing.T, id string, payload []byte) {
			conn, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(context.Background())
			for _, query := range []string{
				`CREATE TABLE wa_client_profile_states(client_profile_id TEXT PRIMARY KEY REFERENCES wa_client_profiles(client_profile_id),state_json JSONB NOT NULL)`,
				`CREATE TABLE wa_registrations(registration_id TEXT PRIMARY KEY)`,
				`INSERT INTO wa_registrations VALUES('migration-fixture')`,
				`ALTER TABLE wa_login_states ADD CONSTRAINT wa_login_states_registration_id_fkey FOREIGN KEY(registration_id) REFERENCES wa_registrations(registration_id)`,
				`DELETE FROM wa_account_credentials`, `DELETE FROM wa_protocol_states`, `DELETE FROM wa_account_acceptances`,
			} {
				if _, err := conn.Exec(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := conn.Exec(ctx, `INSERT INTO wa_client_profile_states VALUES($1,$2::jsonb)`, id, string(payload)); err != nil {
				t.Fatal(err)
			}
		},
	}
}
