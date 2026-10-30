package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func validateMessageReceipts(accountID string, receipts []*waappv1.MessageReceipt) error {
	for _, receipt := range receipts {
		if accountID == "" || receipt.GetProviderMessageId() == "" || receipt.GetContactRef() == "" || receipt.GetRecipientRef() == "" ||
			receipt.GetType() < waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_DELIVERED || receipt.GetType() > waappv1.MessageReceiptType_MESSAGE_RECEIPT_TYPE_PLAYED ||
			receipt.GetTimestamp() == nil || receipt.GetTimestamp().CheckValid() != nil {
			return fmt.Errorf("invalid message receipt")
		}
	}
	return nil
}

func (s *SQLiteStore) SaveMessageReceipts(ctx context.Context, accountID string, receipts []*waappv1.MessageReceipt) error {
	if err := validateMessageReceipts(accountID, receipts); err != nil {
		return err
	}
	for _, receipt := range receipts {
		_, err := s.queries.ExecContext(ctx, `INSERT INTO wa_sqlite_message_receipts
 (wa_account_id,provider_message_id,contact_ref,recipient_ref,type,timestamp) VALUES (?,?,?,?,?,?)
 ON CONFLICT(wa_account_id,provider_message_id,contact_ref,recipient_ref,type)
 DO UPDATE SET timestamp=MIN(timestamp,excluded.timestamp)`,
			accountID, receipt.GetProviderMessageId(), receipt.GetContactRef(), receipt.GetRecipientRef(), receipt.GetType(), SQLiteTimeValue(receipt.GetTimestamp().AsTime()))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) ListMessageReceipts(ctx context.Context, accountID string, ids []string) ([]*waappv1.MessageReceipt, error) {
	clause, args := sqliteInClause("provider_message_id", ids)
	args = append([]any{accountID}, args...)
	rows, err := s.queries.QueryContext(ctx, `SELECT provider_message_id,contact_ref,recipient_ref,type,timestamp
 FROM wa_sqlite_message_receipts WHERE wa_account_id=? AND `+clause+` ORDER BY timestamp,recipient_ref,type`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var receipts []*waappv1.MessageReceipt
	for rows.Next() {
		receipt := new(waappv1.MessageReceipt)
		var timestamp int64
		if err := rows.Scan(&receipt.ProviderMessageId, &receipt.ContactRef, &receipt.RecipientRef, &receipt.Type, &timestamp); err != nil {
			return nil, err
		}
		receipt.Timestamp = timestamppb.New(time.Unix(0, timestamp))
		receipts = append(receipts, receipt)
	}
	return receipts, rows.Err()
}

func (s *PostgresStore) SaveMessageReceipts(ctx context.Context, accountID string, receipts []*waappv1.MessageReceipt) error {
	if err := validateMessageReceipts(accountID, receipts); err != nil || len(receipts) == 0 {
		return err
	}
	batch := new(pgx.Batch)
	for _, receipt := range receipts {
		batch.Queue(`INSERT INTO wa_message_receipts
 (wa_account_id,provider_message_id,contact_ref,recipient_ref,type,timestamp) VALUES ($1,$2,$3,$4,$5,$6)
 ON CONFLICT(wa_account_id,provider_message_id,contact_ref,recipient_ref,type)
 DO UPDATE SET timestamp=LEAST(wa_message_receipts.timestamp,EXCLUDED.timestamp)`,
			accountID, receipt.GetProviderMessageId(), receipt.GetContactRef(), receipt.GetRecipientRef(), int32(receipt.GetType()), receipt.GetTimestamp().AsTime())
	}
	return s.pool.SendBatch(ctx, batch).Close()
}

func (s *PostgresStore) ListMessageReceipts(ctx context.Context, accountID string, ids []string) ([]*waappv1.MessageReceipt, error) {
	ids = shared.UniqueNonEmptyStrings(ids...)
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT provider_message_id,contact_ref,recipient_ref,type,timestamp
 FROM wa_message_receipts WHERE wa_account_id=$1 AND provider_message_id=ANY($2) ORDER BY timestamp,recipient_ref,type`, accountID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var receipts []*waappv1.MessageReceipt
	for rows.Next() {
		receipt := new(waappv1.MessageReceipt)
		var timestamp time.Time
		if err := rows.Scan(&receipt.ProviderMessageId, &receipt.ContactRef, &receipt.RecipientRef, &receipt.Type, &timestamp); err != nil {
			return nil, err
		}
		receipt.Timestamp = timestamppb.New(timestamp)
		receipts = append(receipts, receipt)
	}
	return receipts, rows.Err()
}

func (s *SQLiteStore) ListPendingDeliveryReceipts(ctx context.Context, accountID string, limit int) ([]*waappv1.InboundMessage, error) {
	return sqliteListPayloads(ctx, s.queries, func() *waappv1.InboundMessage { return new(waappv1.InboundMessage) }, `SELECT m.payload
 FROM wa_sqlite_inbound_messages m JOIN wa_sqlite_message_sessions s ON s.id=m.message_session_id
 WHERE s.wa_account_id=? AND m.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTED'
 AND json_extract(m.payload,'$.kind') IN ('INBOUND_MESSAGE_KIND_MESSAGE','INBOUND_MESSAGE_KIND_SYSTEM')
 AND COALESCE(json_extract(m.payload,'$.direction'),'ACCOUNT_MESSAGE_DIRECTION_INBOUND')='ACCOUNT_MESSAGE_DIRECTION_INBOUND'
 AND COALESCE(json_extract(m.payload,'$.source'),'')<>'ACCOUNT_MESSAGE_SOURCE_IMPORTED_HISTORY'
 AND COALESCE(json_extract(m.payload,'$.provider_message_id'),'')<>''
 AND json_extract(m.payload,'$.delivery_receipt_sent_at') IS NULL AND json_extract(m.payload,'$.read_receipt_sent_at') IS NULL
 AND COALESCE(json_extract(m.payload,'$.delete_status'),'')<>'MESSAGE_DELETE_STATUS_DELETED_FOR_ME'
 ORDER BY m.received_at,m.id LIMIT ?`, accountID, normalizeMessageActionLimit(limit))
}

func (s *PostgresStore) ListPendingDeliveryReceipts(ctx context.Context, accountID string, limit int) ([]*waappv1.InboundMessage, error) {
	rows, err := s.db.Query(ctx, `SELECT m.message_id,m.message_session_id,m.kind,m.encryption_state,m.ack_status,m.direction,m.source,m.contact_ref,m.sender_ref,m.payload_ref,m.provider_message_id,m.provider_timestamp,m.read_at,m.delete_status,m.deleted_at,m.last_error_code,m.last_error_message,m.last_error_retryable,m.received_at,m.read_receipt_sent_at,m.delivery_receipt_sent_at
 FROM wa_inbound_messages m JOIN wa_message_sessions s ON s.message_session_id=m.message_session_id
 WHERE s.wa_account_id=$1 AND m.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTED'
 AND m.kind IN ('INBOUND_MESSAGE_KIND_MESSAGE','INBOUND_MESSAGE_KIND_SYSTEM') AND m.direction='ACCOUNT_MESSAGE_DIRECTION_INBOUND'
 AND m.source<>'ACCOUNT_MESSAGE_SOURCE_IMPORTED_HISTORY' AND m.provider_message_id<>''
 AND m.delivery_receipt_sent_at IS NULL AND m.read_receipt_sent_at IS NULL
 AND COALESCE(m.delete_status,'')<>'MESSAGE_DELETE_STATUS_DELETED_FOR_ME'
 ORDER BY m.received_at,m.message_id LIMIT $2`, accountID, normalizeMessageActionLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []*waappv1.InboundMessage
	for rows.Next() {
		message, err := scanPostgresInboundMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}
