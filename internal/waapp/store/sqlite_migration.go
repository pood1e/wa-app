package store

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

// MigrateLegacySQLite converts the stopped service's database in one transaction.
func MigrateLegacySQLite(ctx context.Context, path string) error {
	source, err := openSQLite(ctx, path)
	if err != nil {
		return err
	}
	defer source.Close()
	if err := source.requireCurrentSchema(ctx); !errors.Is(err, ErrLegacySchema) {
		if err != nil {
			return err
		}
		return fmt.Errorf("database is not a legacy SQLite store")
	}
	tx, err := source.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	repo := &SQLiteStore{queries: tx}
	if err := rebuildSQLiteTables(ctx, repo); err != nil {
		return err
	}
	// Profiles are decoded once with the removed preparation fields discarded.
	if err := scrubLegacySQLiteProfiles(ctx, repo); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.id FROM wa_sqlite_client_profiles p
 WHERE EXISTS(SELECT 1 FROM wa_sqlite_login_states l WHERE l.client_profile_id=p.id)
 OR EXISTS(SELECT 1 FROM wa_sqlite_message_sessions m WHERE m.client_profile_id=p.id)`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		var data []byte
		if err := tx.QueryRowContext(ctx, `SELECT state_json FROM wa_sqlite_native_states WHERE client_profile_id=?`, id).Scan(&data); err != nil {
			return err
		}
		if err := migrateLegacyMaterial(ctx, repo, id, data); err != nil {
			return err
		}
	}

	for _, statement := range []string{
		`DROP TABLE wa_sqlite_native_states`,
		`DROP TABLE IF EXISTS wa_sqlite_registrations`,
		`DROP TABLE IF EXISTS wa_sqlite_verification_requests`,
		`DROP TABLE IF EXISTS wa_sqlite_account_probes`,
		`DROP TABLE IF EXISTS wa_sqlite_runtime_state`,
		`DELETE FROM wa_sqlite_client_profiles WHERE id NOT IN(SELECT client_profile_id FROM wa_sqlite_login_states) AND id NOT IN(SELECT client_profile_id FROM wa_sqlite_message_sessions)`,
		`DELETE FROM wa_sqlite_accounts WHERE id NOT IN(SELECT wa_account_id FROM wa_sqlite_login_states) AND id NOT IN(SELECT wa_account_id FROM wa_sqlite_message_sessions) AND id NOT IN(SELECT wa_account_id FROM wa_sqlite_contacts) AND id NOT IN(SELECT wa_account_id FROM wa_sqlite_otp_messages)`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scrubLegacySQLiteProfiles(ctx context.Context, repo *SQLiteStore) error {
	rows, err := repo.queries.QueryContext(ctx, `SELECT payload FROM wa_sqlite_client_profiles`)
	if err != nil {
		return err
	}
	var profiles []*waappv1.ClientProfile
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			_ = rows.Close()
			return err
		}
		profile := &waappv1.ClientProfile{}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(payload, profile); err != nil {
			_ = rows.Close()
			return fmt.Errorf("invalid legacy profile")
		}
		profiles = append(profiles, profile)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if err := repo.SaveClientProfile(ctx, profile); err != nil {
			return err
		}
	}
	return nil
}
