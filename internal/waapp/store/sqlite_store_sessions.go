package store

import (
	"context"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	_ "modernc.org/sqlite"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *SQLiteStore) listLoginStatesByStatus(ctx context.Context, status waappv1.LoginStateStatus) ([]*waappv1.LoginState, error) {
	rows, err := s.queries.QueryContext(ctx, `SELECT payload FROM wa_sqlite_login_states WHERE status=? ORDER BY updated_at DESC`, status.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*waappv1.LoginState{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		state := &waappv1.LoginState{}
		if err := sqliteUnmarshal([]byte(payload), state); err != nil {
			return nil, err
		}
		out = append(out, state)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetLoginStateByRegisteredIdentity(ctx context.Context, registeredIdentityID string) (*waappv1.LoginState, error) {
	return s.getLoginStateBy(ctx, `registered_identity_id=?`, registeredIdentityID)
}

func (s *SQLiteStore) SaveMessageSession(ctx context.Context, session *waappv1.MessageSession) error {
	payload, err := sqliteMarshal(session)
	if err != nil {
		return err
	}
	_, err = s.queries.ExecContext(ctx, `INSERT INTO wa_sqlite_message_sessions (id,wa_account_id,client_profile_id,registered_identity_id,protocol_profile_id,status,updated_at,payload) VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET status=excluded.status, updated_at=excluded.updated_at, payload=excluded.payload`, session.GetMessageSessionId(), session.GetWaAccountId(), session.GetClientProfileId(), session.GetRegisteredIdentityId(), session.GetProtocolProfileId(), session.GetStatus().String(), SQLiteTimeValue(shared.TimeFromProto(firstProtoTime(session.GetLastSeenAt(), session.GetOpenedAt(), session.GetClosedAt()))), payload)
	return err
}

func (s *SQLiteStore) GetMessageSession(ctx context.Context, id string) (*waappv1.MessageSession, error) {
	session := &waappv1.MessageSession{}
	return session, s.loadPayload(ctx, "wa_sqlite_message_sessions", id, session, waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_SESSION_NOT_FOUND, "message session not found")
}

func (s *SQLiteStore) CloseStaleOpenMessageSessions(ctx context.Context, before time.Time) (int64, error) {
	if before.IsZero() {
		return 0, nil
	}
	rows, err := s.queries.QueryContext(ctx, `SELECT payload FROM wa_sqlite_message_sessions WHERE status=? AND updated_at<?`, waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_OPEN.String(), SQLiteTimeValue(before.UTC()))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	sessions := []*waappv1.MessageSession{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return 0, err
		}
		session := &waappv1.MessageSession{}
		if err := sqliteUnmarshal([]byte(payload), session); err != nil {
			return 0, err
		}
		session.Status = waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_CLOSED
		session.ClosedAt = timestamppb.New(time.Now().UTC())
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, session := range sessions {
		if err := s.SaveMessageSession(ctx, session); err != nil {
			return 0, err
		}
	}
	return int64(len(sessions)), nil
}

func (s *SQLiteStore) SaveInboundMessages(ctx context.Context, messages []*waappv1.InboundMessage) error {
	if len(messages) == 0 {
		return nil
	}
	for _, msg := range messages {
		if existing := s.existingInboundMessage(ctx, msg.GetMessageId()); existing != nil {
			mergeInboundMessageState(existing, msg)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, msg := range messages {
		payload, err := sqliteMarshal(msg)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO wa_sqlite_inbound_messages (id,message_session_id,encryption_state,received_at,payload) VALUES (?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET encryption_state=excluded.encryption_state, payload=excluded.payload`, msg.GetMessageId(), msg.GetMessageSessionId(), msg.GetEncryptionState().String(), SQLiteTimeValue(shared.TimeFromProto(msg.GetReceivedAt())), payload); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) existingInboundMessage(ctx context.Context, id string) *waappv1.InboundMessage {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	msg := &waappv1.InboundMessage{}
	if err := s.loadPayload(ctx, "wa_sqlite_inbound_messages", id, msg, waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "message not found"); err != nil {
		return nil
	}
	return msg
}

func (s *SQLiteStore) GetInboundMessage(ctx context.Context, id string) (*waappv1.InboundMessage, error) {
	msg := &waappv1.InboundMessage{}
	return msg, s.loadPayload(ctx, "wa_sqlite_inbound_messages", id, msg, waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "message not found")
}

func (s *SQLiteStore) ListPendingEncryptedInboundMessages(ctx context.Context, waAccountIDValue string, clientProfileID string, limit int) ([]*waappv1.InboundMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	return sqliteListPayloads(ctx, s.queries, func() *waappv1.InboundMessage { return &waappv1.InboundMessage{} }, `SELECT m.payload FROM wa_sqlite_inbound_messages m
JOIN wa_sqlite_message_sessions s ON s.id=m.message_session_id
LEFT JOIN wa_sqlite_decrypted_messages d ON d.message_id=m.id
WHERE s.wa_account_id=? AND s.client_profile_id=?
  AND (m.encryption_state=? OR (m.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTION_FAILED' AND json_extract(m.payload, '$.last_error.retryable')=1))
  AND COALESCE(json_extract(m.payload, '$.direction'), 'ACCOUNT_MESSAGE_DIRECTION_INBOUND')='ACCOUNT_MESSAGE_DIRECTION_INBOUND' AND COALESCE(json_extract(m.payload, '$.delete_status'), 'MESSAGE_DELETE_STATUS_NOT_DELETED')<>'MESSAGE_DELETE_STATUS_DELETED_FOR_ME' AND d.id IS NULL
AND NOT EXISTS (
 SELECT 1 FROM wa_sqlite_inbound_messages recovered JOIN wa_sqlite_message_sessions owner ON owner.id=recovered.message_session_id
 JOIN wa_sqlite_decrypted_messages projected ON projected.message_id=recovered.id
 WHERE COALESCE(json_extract(m.payload,'$.provider_message_id'),'')<>''
 AND json_extract(recovered.payload,'$.provider_message_id')=json_extract(m.payload,'$.provider_message_id') AND owner.wa_account_id=s.wa_account_id
 AND json_extract(recovered.payload,'$.contact_ref')=json_extract(m.payload,'$.contact_ref')
 AND COALESCE(json_extract(recovered.payload,'$.sender_ref'),'')=COALESCE(json_extract(m.payload,'$.sender_ref'),'')
 AND COALESCE(json_extract(recovered.payload,'$.direction'),'ACCOUNT_MESSAGE_DIRECTION_INBOUND')=COALESCE(json_extract(m.payload,'$.direction'),'ACCOUNT_MESSAGE_DIRECTION_INBOUND')
 AND recovered.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTED'
 AND (json_extract(recovered.payload,'$.kind')='INBOUND_MESSAGE_KIND_MESSAGE' OR json_extract(projected.payload,'$.content.change.target_provider_message_id') IS NOT NULL)
)
ORDER BY m.received_at ASC, m.id ASC LIMIT ?`, waAccountIDValue, clientProfileID, waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_ENCRYPTED.String(), limit)
}

func (s *SQLiteStore) SaveDecryptedMessage(ctx context.Context, msg *waappv1.DecryptedMessage) error {
	payload, err := sqliteMarshal(msg)
	if err != nil {
		return err
	}
	_, err = s.queries.ExecContext(ctx, `INSERT INTO wa_sqlite_decrypted_messages (id,message_id,decrypted_at,payload) VALUES (?,?,?,?)
ON CONFLICT(id) DO UPDATE SET message_id=excluded.message_id, decrypted_at=excluded.decrypted_at, payload=excluded.payload`, msg.GetDecryptedMessageId(), msg.GetMessageId(), SQLiteTimeValue(shared.TimeFromProto(msg.GetDecryptedAt())), payload)
	return err
}

func (s *SQLiteStore) GetDecryptedMessage(ctx context.Context, id string) (*waappv1.DecryptedMessage, error) {
	msg := &waappv1.DecryptedMessage{}
	return msg, s.loadPayload(ctx, "wa_sqlite_decrypted_messages", id, msg, waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "decrypted message not found")
}

func (s *SQLiteStore) SaveCandidates(ctx context.Context, candidates []*waappv1.ExtractedCandidate) error {
	if len(candidates) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, candidate := range candidates {
		payload, err := sqliteMarshal(candidate)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO wa_sqlite_candidates (id,message_id,decrypted_message_id,extracted_at,payload) VALUES (?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET decrypted_message_id=excluded.decrypted_message_id, extracted_at=excluded.extracted_at, payload=excluded.payload`, candidate.GetCandidateId(), candidate.GetMessageId(), candidate.GetDecryptedMessageId(), SQLiteTimeValue(shared.TimeFromProto(candidate.GetExtractedAt())), payload); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) SaveOTPMessage(ctx context.Context, msg *waappv1.OtpMessage) error {
	if msg == nil {
		return nil
	}
	otpValue := strings.TrimSpace(msg.GetOtp().GetValue())
	if otpValue == "" {
		otpValue = strings.TrimSpace(msg.GetOtp().GetRedactedValue())
	}
	if otpValue == "" {
		return nil
	}
	stored := proto.Clone(msg).(*waappv1.OtpMessage)
	stored.OtpMessageId = shared.FirstNonEmpty(stored.GetOtpMessageId(), wamodel.StableOTPMessageID(stored.GetWaAccountId(), stored.GetSourceParty(), otpValue))
	if stored.Otp == nil {
		stored.Otp = &waappv1.SensitiveText{}
	}
	stored.Otp.Value = otpValue
	stored.Otp.RedactedValue = shared.FirstNonEmpty(stored.GetOtp().GetRedactedValue(), shared.Redacted(otpValue))
	stored.Otp.SecretRef = shared.FirstNonEmpty(stored.GetOtp().GetSecretRef(), "wa-otp:"+shared.StableID(stored.GetOtpMessageId()))
	if stored.ReceivedAt == nil {
		stored.ReceivedAt = timestamppb.New(time.Now().UTC())
	}
	payload, err := sqliteMarshal(stored)
	if err != nil {
		return err
	}
	_, err = s.queries.ExecContext(ctx, `INSERT INTO wa_sqlite_otp_messages (id,wa_account_id,received_at,payload) VALUES (?,?,?,?)
ON CONFLICT(id) DO UPDATE SET wa_account_id=excluded.wa_account_id, received_at=excluded.received_at, payload=excluded.payload`, stored.GetOtpMessageId(), stored.GetWaAccountId(), SQLiteTimeValue(shared.TimeFromProto(stored.GetReceivedAt())), payload)
	return err
}

func (s *SQLiteStore) ListAccountOTPMessages(ctx context.Context, waAccountIDValue string, cursorValue string, limit int, includeSensitiveValues bool) ([]*waappv1.OtpMessage, string, error) {
	cursor, err := shared.DecodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = shared.NormalizePageLimit(limit)
	lookahead := shared.KeysetLookaheadLimit(limit)
	query := `SELECT o.payload FROM wa_sqlite_otp_messages o
WHERE o.wa_account_id=? AND NOT EXISTS (
  SELECT 1 FROM wa_sqlite_inbound_messages m
  WHERE m.id=json_extract(o.payload, '$.message_id') AND COALESCE(json_extract(m.payload, '$.delete_status'), 'MESSAGE_DELETE_STATUS_NOT_DELETED')='MESSAGE_DELETE_STATUS_DELETED_FOR_ME'
)`
	args := []any{waAccountIDValue}
	if shared.HasKeysetCursor(cursor) {
		value := SQLiteTimeValue(cursor.UpdatedAt)
		query += ` AND (o.received_at < ? OR (o.received_at = ? AND o.id < ?))`
		args = append(args, value, value, cursor.ID)
	}
	query += ` ORDER BY o.received_at DESC, o.id DESC LIMIT ?`
	args = append(args, lookahead)
	items, err := sqliteListPayloads(ctx, s.queries, func() *waappv1.OtpMessage { return &waappv1.OtpMessage{} }, query, args...)
	if err != nil {
		return nil, "", err
	}
	if !includeSensitiveValues {
		for _, item := range items {
			if item.Otp != nil {
				item.Otp.Value = ""
			}
		}
	}
	items, nextCursor := shared.NewKeysetPage(items, limit, func(msg *waappv1.OtpMessage) shared.KeysetCursor {
		return shared.KeysetCursorValue(shared.TimeFromProto(msg.GetReceivedAt()), msg.GetOtpMessageId())
	})
	return items, nextCursor, nil
}

func (s *SQLiteStore) getWAAccountBy(ctx context.Context, where string, args ...any) (*waappv1.WAAccount, error) {
	account := &waappv1.WAAccount{}
	return account, s.loadPayloadWhere(ctx, "wa_sqlite_accounts", where, args, account, waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, "WA account not found")
}
