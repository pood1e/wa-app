package store

import (
	"context"
	"fmt"
)

var sqliteBusinessTables = []string{
	"wa_sqlite_artifacts", "wa_sqlite_protocol_profiles", "wa_sqlite_accounts", "wa_sqlite_client_profiles",
	"wa_sqlite_login_states", "wa_sqlite_message_sessions", "wa_sqlite_inbound_messages",
	"wa_sqlite_decrypted_messages", "wa_sqlite_candidates", "wa_sqlite_otp_messages", "wa_sqlite_contacts",
	"wa_sqlite_account_credentials", "wa_sqlite_protocol_states", "wa_sqlite_account_acceptances",
}

func rebuildSQLiteTables(ctx context.Context, repo *SQLiteStore) error {
	if _, err := repo.queries.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
		return err
	}
	for _, statement := range []string{
		`DELETE FROM wa_sqlite_client_profiles WHERE id NOT IN(SELECT client_profile_id FROM wa_sqlite_login_states) AND id NOT IN(SELECT client_profile_id FROM wa_sqlite_message_sessions)`,
		`DELETE FROM wa_sqlite_accounts WHERE id NOT IN(SELECT wa_account_id FROM wa_sqlite_login_states) AND id NOT IN(SELECT wa_account_id FROM wa_sqlite_message_sessions) AND id NOT IN(SELECT wa_account_id FROM wa_sqlite_contacts) AND id NOT IN(SELECT wa_account_id FROM wa_sqlite_otp_messages)`,
	} {
		if _, err := repo.queries.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	var moved []string
	for _, table := range sqliteBusinessTables {
		var exists int
		if err := repo.queries.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		if _, err := repo.queries.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s RENAME TO %s_v1`, table, table)); err != nil {
			return err
		}
		moved = append(moved, table)
	}
	if err := repo.migrate(ctx); err != nil {
		return err
	}
	for _, table := range moved {
		if _, err := repo.queries.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s SELECT * FROM %s_v1`, table, table)); err != nil {
			return err
		}
	}
	for i := len(moved) - 1; i >= 0; i-- {
		if _, err := repo.queries.ExecContext(ctx, `DROP TABLE `+moved[i]+`_v1`); err != nil {
			return err
		}
	}
	// Index names were held by the old tables until their removal.
	if err := repo.migrate(ctx); err != nil {
		return err
	}
	rows, err := repo.queries.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("legacy SQLite relationships are inconsistent")
	}
	return rows.Err()
}
