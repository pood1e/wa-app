package store

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *SQLiteStore) ListMessageChanges(ctx context.Context, accountID string, contacts, targets []string, sensitive bool) ([]*waappv1.AccountMessage, error) {
	if len(contacts) == 0 || len(targets) == 0 {
		return nil, nil
	}
	contactClause, contactArgs := sqliteInClause("json_extract(m.payload, '$.contact_ref')", contacts)
	targetClause, targetArgs := sqliteInClause("json_extract(d.payload, '$.content.change.target_provider_message_id')", targets)
	query := `SELECT m.payload,d.payload FROM wa_sqlite_decrypted_messages d
JOIN wa_sqlite_inbound_messages m ON m.id=d.message_id
JOIN wa_sqlite_message_sessions s ON s.id=m.message_session_id
WHERE s.wa_account_id=? AND m.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTED' AND ` + contactClause + ` AND ` + targetClause + ` ORDER BY m.received_at,m.id,d.decrypted_at,d.id`
	args := append([]any{accountID}, contactArgs...)
	args = append(args, targetArgs...)
	rows, err := s.queries.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*waappv1.AccountMessage
	for rows.Next() {
		message, decrypted, err := scanSQLiteAccountMessage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, wamodel.NewAccountMessageFromInbound(accountID, message, decrypted, sensitive))
	}
	return result, rows.Err()
}

func (s *PostgresStore) ListMessageChanges(ctx context.Context, accountID string, contacts, targets []string, sensitive bool) ([]*waappv1.AccountMessage, error) {
	if len(contacts) == 0 || len(targets) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT m.message_id,s.wa_account_id,m.message_session_id,m.kind,m.encryption_state,m.ack_status,m.direction,m.source,m.contact_ref,m.sender_ref,m.payload_ref,m.read_at,m.delete_status,m.deleted_at,d.plaintext_value,d.plaintext_redacted,d.plaintext_secret_ref,m.last_error_code,m.last_error_message,m.last_error_retryable,m.received_at,m.provider_message_id,m.read_receipt_sent_at,d.content
FROM wa_decrypted_messages d
JOIN wa_inbound_messages m ON m.message_id=d.message_id
JOIN wa_message_sessions s ON s.message_session_id=m.message_session_id
WHERE s.wa_account_id=$1 AND m.contact_ref=ANY($2)
AND m.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTED'
AND d.content->'change'->>'target_provider_message_id'=ANY($3)
ORDER BY m.received_at,m.message_id,d.decrypted_at,d.decrypted_message_id`, accountID, contacts, targets)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*waappv1.AccountMessage
	for rows.Next() {
		message, err := scanAccountMessage(rows, sensitive)
		if err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	return result, rows.Err()
}
