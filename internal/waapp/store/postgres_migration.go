package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// MigrateLegacyPostgres preserves account IDs and business data in one transaction.
func MigrateLegacyPostgres(ctx context.Context, dsn string) error {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("invalid PostgreSQL migration configuration")
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if err := requireCurrentPostgresSchema(ctx, tx); !errors.Is(err, ErrLegacySchema) {
			if err != nil {
				return err
			}
			return fmt.Errorf("database is not a legacy PostgreSQL store")
		}
		if _, err := tx.Exec(ctx, postgresStoreSchema); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT p.client_profile_id FROM wa_client_profiles p
 WHERE EXISTS(SELECT 1 FROM wa_login_states l WHERE l.client_profile_id=p.client_profile_id)
 OR EXISTS(SELECT 1 FROM wa_message_sessions m WHERE m.client_profile_id=p.client_profile_id)`)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		repo := &PostgresStore{db: tx}
		for _, id := range ids {
			var data []byte
			if err := tx.QueryRow(ctx, `SELECT state_json FROM wa_client_profile_states WHERE client_profile_id=$1`, id).Scan(&data); err != nil {
				return err
			}
			if err := migrateLegacyMaterial(ctx, repo, id, data); err != nil {
				return err
			}
		}

		for _, statement := range []string{
			`ALTER TABLE wa_login_states DROP CONSTRAINT IF EXISTS wa_login_states_registration_id_fkey`,
			`DROP TABLE IF EXISTS wa_registrations`,
			`DROP TABLE IF EXISTS wa_verification_requests`,
			`DROP TABLE IF EXISTS wa_account_probes`,
			`DROP TABLE wa_client_profile_states`,
			`ALTER TABLE wa_client_profiles DROP COLUMN IF EXISTS state_ref`,
			`ALTER TABLE wa_login_states DROP COLUMN IF EXISTS state_ref`,
			`DELETE FROM wa_client_profiles p WHERE NOT EXISTS(SELECT 1 FROM wa_login_states l WHERE l.client_profile_id=p.client_profile_id) AND NOT EXISTS(SELECT 1 FROM wa_message_sessions m WHERE m.client_profile_id=p.client_profile_id)`,
			`DELETE FROM wa_accounts a WHERE NOT EXISTS(SELECT 1 FROM wa_login_states l WHERE l.wa_account_id=a.wa_account_id) AND NOT EXISTS(SELECT 1 FROM wa_message_sessions m WHERE m.wa_account_id=a.wa_account_id) AND NOT EXISTS(SELECT 1 FROM wa_contacts c WHERE c.wa_account_id=a.wa_account_id) AND NOT EXISTS(SELECT 1 FROM wa_otp_messages o WHERE o.wa_account_id=a.wa_account_id)`,
		} {
			if _, err := tx.Exec(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	})
}
