package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (s *PostgresStore) DeleteWAAccount(ctx context.Context, id string) error {
	if s.pool == nil {
		return deletePostgresAccount(ctx, s.db, id)
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return deletePostgresAccount(ctx, tx, id) })
}
func deletePostgresAccount(ctx context.Context, db DBTX, id string) error {
	if err := lockWAAccount(ctx, db, id); err != nil {
		return err
	}
	for _, statement := range waAccountDeleteStatements {
		if _, err := db.Exec(ctx, statement, id); err != nil {
			return err
		}
	}
	_, err := db.Exec(ctx, `DELETE FROM wa_accounts WHERE wa_account_id=$1`, id)
	return err
}

func lockWAAccount(ctx context.Context, tx DBTX, waAccountIDValue string) error {
	var accountID string
	err := tx.QueryRow(ctx, `SELECT wa_account_id FROM wa_accounts WHERE wa_account_id=$1 FOR UPDATE`, waAccountIDValue).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, "WA account not found", false)
	}
	return err
}

var waAccountDeleteStatements = []string{
	`DELETE FROM wa_contacts WHERE wa_account_id=$1`,
	`DELETE FROM wa_extracted_candidates WHERE message_id IN (
		SELECT m.message_id FROM wa_inbound_messages m
		JOIN wa_message_sessions s ON s.message_session_id=m.message_session_id
		WHERE s.wa_account_id=$1
	)`,
	`DELETE FROM wa_decrypted_messages WHERE message_id IN (
		SELECT m.message_id FROM wa_inbound_messages m
		JOIN wa_message_sessions s ON s.message_session_id=m.message_session_id
		WHERE s.wa_account_id=$1
	)`,
	`DELETE FROM wa_inbound_messages WHERE message_session_id IN (
		SELECT message_session_id FROM wa_message_sessions WHERE wa_account_id=$1
	)`,
	`DELETE FROM wa_message_sessions WHERE wa_account_id=$1`,
	`DELETE FROM wa_otp_messages WHERE wa_account_id=$1`,
	`DELETE FROM wa_login_states WHERE wa_account_id=$1`,
	`DELETE FROM wa_client_profiles WHERE wa_account_id=$1`,
}
