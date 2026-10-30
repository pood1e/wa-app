package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *PostgresStore) ListAccountMessages(ctx context.Context, waAccountIDValue string, contactRefs []string, cursorValue string, limit int, includeSensitiveText bool) ([]*waappv1.AccountMessage, string, error) {
	cursor, err := shared.DecodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	contactRefs = shared.UniqueNonEmptyStrings(contactRefs...)
	if len(contactRefs) == 0 {
		return nil, "", nil
	}
	limit = shared.NormalizePageLimit(limit)
	rows, err := s.queryAccountMessagePage(ctx, waAccountIDValue, contactRefs, cursor, shared.KeysetLookaheadLimit(limit), false)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []*waappv1.AccountMessage{}
	for rows.Next() {
		item, err := scanAccountMessage(rows, includeSensitiveText)
		if err != nil {
			return nil, "", err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	items, nextCursor := shared.NewKeysetPage(items, limit, func(message *waappv1.AccountMessage) shared.KeysetCursor {
		return shared.KeysetCursorValue(shared.TimeFromProto(message.GetReceivedAt()), message.GetAccountMessageId())
	})
	return items, nextCursor, nil
}

func (s *PostgresStore) queryAccountMessagePage(ctx context.Context, waAccountIDValue string, contactRefs []string, cursor shared.KeysetCursor, limit int, latest bool) (pgx.Rows, error) {
	query := `WITH attempts AS (
 SELECT candidate.message_id,candidate.message_session_id,candidate.kind,candidate.encryption_state,candidate.ack_status,candidate.direction,candidate.source,candidate.contact_ref,candidate.sender_ref,candidate.payload_ref,candidate.provider_message_id,candidate.received_at,candidate.read_at,candidate.read_receipt_sent_at,candidate.delete_status,candidate.deleted_at,candidate.last_error_code,candidate.last_error_message,candidate.last_error_retryable,ROW_NUMBER() OVER (
 PARTITION BY COALESCE(NULLIF(candidate.contact_ref,''),candidate.sender_ref),candidate.sender_ref,candidate.direction,COALESCE(NULLIF(candidate.provider_message_id,''),candidate.message_id)
 ORDER BY (candidate.delete_status='MESSAGE_DELETE_STATUS_DELETED_FOR_ME') DESC,(candidate.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTED') DESC,candidate.received_at,candidate.message_id
 ) AS transport_rank
 FROM wa_inbound_messages candidate JOIN wa_message_sessions owner ON owner.message_session_id=candidate.message_session_id
 WHERE owner.wa_account_id=$1 AND candidate.kind=$2 AND COALESCE(NULLIF(candidate.contact_ref,''),candidate.sender_ref)=ANY($3)
)
SELECT m.message_id,ms.wa_account_id,m.message_session_id,m.kind,m.encryption_state,m.ack_status,m.direction,m.source,m.contact_ref,m.sender_ref,m.payload_ref,m.read_at,m.delete_status,m.deleted_at,COALESCE(d.plaintext_value,''),COALESCE(d.plaintext_redacted,''),COALESCE(d.plaintext_secret_ref,''),m.last_error_code,m.last_error_message,m.last_error_retryable,m.received_at,m.provider_message_id,m.read_receipt_sent_at,COALESCE(d.content,'{}'::jsonb)
FROM attempts m
JOIN wa_message_sessions ms ON ms.message_session_id=m.message_session_id
LEFT JOIN LATERAL (
  SELECT plaintext_value,plaintext_redacted,plaintext_secret_ref,content
  FROM wa_decrypted_messages
  WHERE message_id=m.message_id
  ORDER BY decrypted_at DESC, decrypted_message_id DESC
  LIMIT 1
) d ON true
WHERE m.transport_rank=1
  AND COALESCE(m.delete_status,'MESSAGE_DELETE_STATUS_NOT_DELETED')<>'MESSAGE_DELETE_STATUS_DELETED_FOR_ME'`
	args := []any{waAccountIDValue, waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE.String(), contactRefs}
	nextArg := 4
	if latest {
		query += ` AND NOT EXISTS (SELECT 1 FROM attempts newer
 WHERE newer.transport_rank=1 AND newer.contact_ref=m.contact_ref
 AND COALESCE(newer.delete_status,'MESSAGE_DELETE_STATUS_NOT_DELETED')<>'MESSAGE_DELETE_STATUS_DELETED_FOR_ME'
 AND (newer.received_at,newer.message_id)>(m.received_at,m.message_id))`
	}
	if shared.HasKeysetCursor(cursor) {
		query += fmt.Sprintf(" AND (m.received_at, m.message_id) < ($%d, $%d)", nextArg, nextArg+1)
		args = append(args, cursor.UpdatedAt, cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" ORDER BY m.received_at DESC, m.message_id DESC LIMIT $%d", nextArg)
	args = append(args, limit)
	return s.db.Query(ctx, query, args...)
}

func scanAccountMessage(rows pgx.Rows, includeSensitiveText bool) (*waappv1.AccountMessage, error) {
	var parts wamodel.AccountMessageParts
	var content []byte
	var kind string
	var encryptionState string
	var ackStatus string
	var errCode string
	var errMessage string
	var errRetryable bool
	var readAt sql.NullTime
	var readReceiptSentAt sql.NullTime
	var deletedAt sql.NullTime
	var deleteStatus string
	var direction string
	var source string
	if err := rows.Scan(&parts.MessageID, &parts.AccountID, &parts.SessionID, &kind, &encryptionState, &ackStatus, &direction, &source, &parts.ContactRef, &parts.SenderRef, &parts.PayloadRef, &readAt, &deleteStatus, &deletedAt, &parts.Plaintext, &parts.Redacted, &parts.SecretRef, &errCode, &errMessage, &errRetryable, &parts.ReceivedAt, &parts.ProviderID, &readReceiptSentAt, &content); err != nil {
		return nil, err
	}
	var err error
	parts.Content, err = unmarshalMessageContent(content)
	if err != nil {
		return nil, err
	}
	parts.Kind = waappv1.InboundMessageKind(waappv1.InboundMessageKind_value[kind])
	parts.EncryptionState = waappv1.MessageEncryptionState(waappv1.MessageEncryptionState_value[encryptionState])
	parts.AckStatus = waappv1.MessageAckStatus(waappv1.MessageAckStatus_value[ackStatus])
	parts.Direction = messageDirection(direction)
	parts.Source = messageSource(source)
	if readAt.Valid {
		parts.ReadAt = readAt.Time.UTC()
	}
	if readReceiptSentAt.Valid {
		parts.ReadReceiptSentAt = readReceiptSentAt.Time.UTC()
	}
	parts.DeleteStatus = messageDeleteStatus(deleteStatus)
	if deletedAt.Valid {
		parts.DeletedAt = deletedAt.Time.UTC()
	}
	parts.LastError = protoError(errCode, errMessage, errRetryable)
	return wamodel.NewAccountMessage(parts, includeSensitiveText), nil
}

func (s *PostgresStore) ListPendingReadMessagesByContactRefs(ctx context.Context, waAccountIDValue string, contactRefs []string, includePendingReceipts bool, limit int) ([]*waappv1.InboundMessage, error) {
	contactRefs = shared.UniqueNonEmptyStrings(contactRefs...)
	if len(contactRefs) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT m.message_id,m.message_session_id,m.kind,m.encryption_state,m.ack_status,m.direction,m.source,m.contact_ref,m.sender_ref,m.payload_ref,m.provider_message_id,m.provider_timestamp,m.read_at,m.delete_status,m.deleted_at,m.last_error_code,m.last_error_message,m.last_error_retryable,m.received_at,m.read_receipt_sent_at,m.delivery_receipt_sent_at
FROM wa_inbound_messages m
JOIN wa_message_sessions ms ON ms.message_session_id=m.message_session_id
WHERE ms.wa_account_id=$1
  AND m.kind=$2
  AND m.direction='ACCOUNT_MESSAGE_DIRECTION_INBOUND'
  AND (m.read_at IS NULL OR ($5 AND m.read_receipt_sent_at IS NULL AND m.provider_message_id<>''))
  AND COALESCE(NULLIF(m.contact_ref,''), m.sender_ref)=ANY($3)
  AND COALESCE(m.delete_status,'MESSAGE_DELETE_STATUS_NOT_DELETED')<>'MESSAGE_DELETE_STATUS_DELETED_FOR_ME'
ORDER BY m.received_at DESC, m.message_id DESC
LIMIT $4`, waAccountIDValue, waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE.String(), contactRefs, normalizeMessageActionLimit(limit), includePendingReceipts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := []*waappv1.InboundMessage{}
	for rows.Next() {
		msg, err := scanPostgresInboundMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func normalizeMessageActionLimit(limit int) int {
	if limit <= 0 || limit > wamodel.MaxMessageActionBatchSize {
		return wamodel.MaxMessageActionBatchSize
	}
	return limit
}

func (s *PostgresStore) ListLatestAccountMessages(ctx context.Context, accountID string, contacts []string) ([]*waappv1.AccountMessage, error) {
	if len(contacts) == 0 {
		return nil, nil
	}
	rows, err := s.queryAccountMessagePage(ctx, accountID, contacts, shared.KeysetCursor{}, len(contacts), true)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*waappv1.AccountMessage
	for rows.Next() {
		message, err := scanAccountMessage(rows, true)
		if err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	return result, rows.Err()
}
