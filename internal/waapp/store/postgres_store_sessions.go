package store

import (
	"context"
	"time"

	pgx "github.com/jackc/pgx/v5"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (s *PostgresStore) ListClientProfiles(ctx context.Context, waAccountIDValue string, cursorValue string, limit int) ([]*waappv1.ClientProfile, string, error) {
	cursor, err := shared.DecodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = shared.NormalizePageLimit(limit)
	rows, err := s.queryClientProfilePage(ctx, waAccountIDValue, cursor, shared.KeysetLookaheadLimit(limit))
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []*waappv1.ClientProfile{}
	for rows.Next() {
		var r clientProfileRow
		if err := rows.Scan(&r.id, &r.waAccountIDValue, &r.protocolProfileID, &r.status, &r.registrationKeyState, &r.messagingKeyState, &r.errCode, &r.errMessage, &r.errRetryable, &r.lastUsedAt, &r.createdAt, &r.updatedAt); err != nil {
			return nil, "", err
		}
		items = append(items, r.toProto())
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	items, nextCursor := shared.NewKeysetPage(items, limit, func(profile *waappv1.ClientProfile) shared.KeysetCursor {
		return shared.KeysetCursorValue(shared.TimeFromProto(profile.GetAudit().GetUpdatedAt()), profile.GetClientProfileId())
	})
	return items, nextCursor, nil
}

func (s *PostgresStore) queryClientProfilePage(ctx context.Context, waAccountIDValue string, cursor shared.KeysetCursor, limit int) (pgx.Rows, error) {
	const base = `SELECT client_profile_id,wa_account_id,protocol_profile_id,status,registration_key_state,messaging_key_state,last_error_code,last_error_message,last_error_retryable,last_used_at,created_at,updated_at FROM wa_client_profiles WHERE wa_account_id=$1`
	if !shared.HasKeysetCursor(cursor) {
		return s.db.Query(ctx, base+` ORDER BY updated_at DESC, client_profile_id DESC LIMIT $2`, waAccountIDValue, limit)
	}
	return s.db.Query(ctx, base+` AND (updated_at, client_profile_id) < ($2, $3) ORDER BY updated_at DESC, client_profile_id DESC LIMIT $4`, waAccountIDValue, cursor.UpdatedAt, cursor.ID, limit)
}

func (s *PostgresStore) SaveLoginState(ctx context.Context, state *waappv1.LoginState) error {
	appErr := state.GetLastError()
	tag, err := s.db.Exec(ctx, `INSERT INTO wa_login_states (login_state_id, registration_id, wa_account_id, client_profile_id, registered_identity_id, service_account_id, service_login_id, status, last_error_code, last_error_message, last_error_retryable, registered_at, last_verified_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (login_state_id) DO UPDATE SET status=EXCLUDED.status, service_account_id=EXCLUDED.service_account_id, service_login_id=EXCLUDED.service_login_id, last_error_code=EXCLUDED.last_error_code, last_error_message=EXCLUDED.last_error_message, last_error_retryable=EXCLUDED.last_error_retryable, last_verified_at=EXCLUDED.last_verified_at, updated_at=EXCLUDED.updated_at
WHERE (wa_login_states.status<>'LOGIN_STATE_STATUS_REVOKED' OR EXCLUDED.status='LOGIN_STATE_STATUS_REVOKED')
  AND (wa_login_states.status<>'LOGIN_STATE_STATUS_INVALID' OR EXCLUDED.status IN ('LOGIN_STATE_STATUS_INVALID','LOGIN_STATE_STATUS_REVOKED'))`,
		state.GetLoginStateId(), state.GetRegistrationId(), state.GetWaAccountId(), state.GetClientProfileId(), state.GetRegisteredIdentityId(), state.GetServiceAccountId(), state.GetServiceLoginId(), state.GetStatus().String(), errCode(appErr), errMessage(appErr), errRetryable(appErr), shared.TimeFromProto(state.GetRegisteredAt()), nullableProtoTime(state.GetLastVerifiedAt()), shared.TimeFromProto(state.GetAudit().GetCreatedAt()), shared.TimeFromProto(state.GetAudit().GetUpdatedAt()))
	if err == nil && tag.RowsAffected() == 0 {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "login identity is no longer active", false)
	}
	return err
}

func (s *PostgresStore) GetLoginState(ctx context.Context, id string) (*waappv1.LoginState, error) {
	return s.getLoginState(ctx, `login_state_id=$1`, id)
}

func (s *PostgresStore) GetActiveLoginState(ctx context.Context, waAccountIDValue string, clientProfileID string) (*waappv1.LoginState, error) {
	return s.getLoginState(ctx, `wa_account_id=$1 AND ($2='' OR client_profile_id=$2) AND status=$3`, waAccountIDValue, clientProfileID, waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE.String())
}

func (s *PostgresStore) ListActiveLoginStates(ctx context.Context) ([]*waappv1.LoginState, error) {
	return s.listLoginStatesByStatus(ctx, waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE)
}

func (s *PostgresStore) ListStoppedLoginStates(ctx context.Context) ([]*waappv1.LoginState, error) {
	revoked, err := s.listLoginStatesByStatus(ctx, waappv1.LoginStateStatus_LOGIN_STATE_STATUS_REVOKED)
	if err != nil {
		return nil, err
	}
	invalid, err := s.listLoginStatesByStatus(ctx, waappv1.LoginStateStatus_LOGIN_STATE_STATUS_INVALID)
	if err != nil {
		return nil, err
	}
	return append(revoked, invalid...), nil
}

func (s *PostgresStore) listLoginStatesByStatus(ctx context.Context, status waappv1.LoginStateStatus) ([]*waappv1.LoginState, error) {
	rows, err := s.db.Query(ctx, `SELECT login_state_id,registration_id,wa_account_id,client_profile_id,registered_identity_id,service_account_id,service_login_id,status,last_error_code,last_error_message,last_error_retryable,registered_at,last_verified_at,created_at,updated_at
FROM wa_login_states
WHERE status=$1
ORDER BY updated_at DESC`, status.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []*waappv1.LoginState{}
	for rows.Next() {
		var r loginStateRow
		if err := rows.Scan(&r.id, &r.registrationID, &r.waAccountIDValue, &r.clientProfileID, &r.registeredIdentityID, &r.serviceAccountID, &r.serviceLoginID, &r.status, &r.errCode, &r.errMessage, &r.errRetryable, &r.registeredAt, &r.lastVerifiedAt, &r.createdAt, &r.updatedAt); err != nil {
			return nil, err
		}
		records = append(records, r.toProto())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *PostgresStore) GetLoginStateByRegisteredIdentity(ctx context.Context, registeredIdentityID string) (*waappv1.LoginState, error) {
	return s.getLoginState(ctx, `registered_identity_id=$1`, registeredIdentityID)
}

func (s *PostgresStore) getLoginState(ctx context.Context, where string, args ...any) (*waappv1.LoginState, error) {
	row := s.db.QueryRow(ctx, `SELECT login_state_id,registration_id,wa_account_id,client_profile_id,registered_identity_id,service_account_id,service_login_id,status,last_error_code,last_error_message,last_error_retryable,registered_at,last_verified_at,created_at,updated_at FROM wa_login_states WHERE `+where, args...)
	var r loginStateRow
	if err := row.Scan(&r.id, &r.registrationID, &r.waAccountIDValue, &r.clientProfileID, &r.registeredIdentityID, &r.serviceAccountID, &r.serviceLoginID, &r.status, &r.errCode, &r.errMessage, &r.errRetryable, &r.registeredAt, &r.lastVerifiedAt, &r.createdAt, &r.updatedAt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND, "login state not found")
	}
	return r.toProto(), nil
}

func (s *PostgresStore) SaveMessageSession(ctx context.Context, session *waappv1.MessageSession) error {
	appErr := session.GetLastError()
	_, err := s.db.Exec(ctx, `INSERT INTO wa_message_sessions (message_session_id, wa_account_id, client_profile_id, registered_identity_id, protocol_profile_id, status, last_error_code, last_error_message, last_error_retryable, opened_at, last_seen_at, closed_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (message_session_id) DO UPDATE SET status=EXCLUDED.status, last_error_code=EXCLUDED.last_error_code, last_error_message=EXCLUDED.last_error_message, last_error_retryable=EXCLUDED.last_error_retryable, last_seen_at=EXCLUDED.last_seen_at, closed_at=EXCLUDED.closed_at`,
		session.GetMessageSessionId(), session.GetWaAccountId(), session.GetClientProfileId(), session.GetRegisteredIdentityId(), session.GetProtocolProfileId(), session.GetStatus().String(), errCode(appErr), errMessage(appErr), errRetryable(appErr), shared.TimeFromProto(session.GetOpenedAt()), nullableProtoTime(session.GetLastSeenAt()), nullableProtoTime(session.GetClosedAt()))
	return err
}

func (s *PostgresStore) GetMessageSession(ctx context.Context, id string) (*waappv1.MessageSession, error) {
	row := s.db.QueryRow(ctx, `SELECT message_session_id,wa_account_id,client_profile_id,registered_identity_id,protocol_profile_id,status,last_error_code,last_error_message,last_error_retryable,opened_at,last_seen_at,closed_at FROM wa_message_sessions WHERE message_session_id=$1`, id)
	var r sessionRow
	if err := row.Scan(&r.id, &r.waAccountIDValue, &r.clientProfileID, &r.identityID, &r.protocolProfileID, &r.status, &r.errCode, &r.errMessage, &r.errRetryable, &r.openedAt, &r.lastSeenAt, &r.closedAt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_SESSION_NOT_FOUND, "message session not found")
	}
	return r.toProto(), nil
}

func (s *PostgresStore) CloseStaleOpenMessageSessions(ctx context.Context, before time.Time) (int64, error) {
	if before.IsZero() {
		return 0, nil
	}
	tag, err := s.db.Exec(ctx, `UPDATE wa_message_sessions
SET status=$1, closed_at=now()
WHERE status=$2 AND COALESCE(last_seen_at, opened_at) < $3`,
		waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_CLOSED.String(),
		waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_OPEN.String(),
		before.UTC())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *PostgresStore) SaveInboundMessages(ctx context.Context, messages []*waappv1.InboundMessage) error {
	if len(messages) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, msg := range messages {
		appErr := msg.GetLastError()
		batch.Queue(`INSERT INTO wa_inbound_messages (message_id, message_session_id, kind, encryption_state, ack_status, direction, source, contact_ref, sender_ref, payload_ref, provider_message_id, provider_timestamp, read_at, delete_status, deleted_at, last_error_code, last_error_message, last_error_retryable, received_at, read_receipt_sent_at, delivery_receipt_sent_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
ON CONFLICT (message_id) DO UPDATE SET kind=CASE WHEN wa_inbound_messages.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTED' AND EXCLUDED.encryption_state<>'MESSAGE_ENCRYPTION_STATE_DECRYPTED' THEN wa_inbound_messages.kind ELSE EXCLUDED.kind END, encryption_state=CASE WHEN wa_inbound_messages.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTED' THEN wa_inbound_messages.encryption_state ELSE EXCLUDED.encryption_state END, ack_status=EXCLUDED.ack_status, direction=EXCLUDED.direction, source=EXCLUDED.source, contact_ref=EXCLUDED.contact_ref, sender_ref=EXCLUDED.sender_ref, payload_ref=EXCLUDED.payload_ref, provider_message_id=COALESCE(NULLIF(EXCLUDED.provider_message_id,''), wa_inbound_messages.provider_message_id), provider_timestamp=COALESCE(EXCLUDED.provider_timestamp, wa_inbound_messages.provider_timestamp), read_at=COALESCE(EXCLUDED.read_at, wa_inbound_messages.read_at), read_receipt_sent_at=COALESCE(EXCLUDED.read_receipt_sent_at, wa_inbound_messages.read_receipt_sent_at), delivery_receipt_sent_at=COALESCE(EXCLUDED.delivery_receipt_sent_at, wa_inbound_messages.delivery_receipt_sent_at), delete_status=CASE WHEN wa_inbound_messages.delete_status='MESSAGE_DELETE_STATUS_DELETED_FOR_ME' AND EXCLUDED.delete_status='MESSAGE_DELETE_STATUS_NOT_DELETED' THEN wa_inbound_messages.delete_status ELSE EXCLUDED.delete_status END, deleted_at=COALESCE(EXCLUDED.deleted_at, wa_inbound_messages.deleted_at), last_error_code=CASE WHEN wa_inbound_messages.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTED' THEN wa_inbound_messages.last_error_code ELSE EXCLUDED.last_error_code END, last_error_message=CASE WHEN wa_inbound_messages.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTED' THEN wa_inbound_messages.last_error_message ELSE EXCLUDED.last_error_message END, last_error_retryable=CASE WHEN wa_inbound_messages.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTED' THEN wa_inbound_messages.last_error_retryable ELSE EXCLUDED.last_error_retryable END`,
			msg.GetMessageId(), msg.GetMessageSessionId(), msg.GetKind().String(), msg.GetEncryptionState().String(), msg.GetAckStatus().String(), inboundMessageDirectionStorageValue(msg), inboundMessageSourceStorageValue(msg), msg.GetContactRef(), msg.GetSenderRef(), msg.GetPayloadRef(), msg.GetProviderMessageId(), nullableProtoTime(msg.GetProviderTimestamp()), nullableProtoTime(msg.GetReadAt()), inboundDeleteStatusStorageValue(msg), nullableProtoTime(msg.GetDeletedAt()), errCode(appErr), errMessage(appErr), errRetryable(appErr), shared.TimeFromProto(msg.GetReceivedAt()), nullableProtoTime(msg.GetReadReceiptSentAt()), nullableProtoTime(msg.GetDeliveryReceiptSentAt()))
	}
	return s.pool.SendBatch(ctx, batch).Close()
}

func inboundMessageDirectionStorageValue(msg *waappv1.InboundMessage) string {
	if msg.GetDirection() == waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_UNSPECIFIED {
		return waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND.String()
	}
	return msg.GetDirection().String()
}

func inboundMessageSourceStorageValue(msg *waappv1.InboundMessage) string {
	if msg.GetSource() == waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_UNSPECIFIED {
		return waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_LONG_CONNECTION.String()
	}
	return msg.GetSource().String()
}

func inboundDeleteStatusStorageValue(msg *waappv1.InboundMessage) string {
	if msg.GetDeleteStatus() == waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_UNSPECIFIED {
		return waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_NOT_DELETED.String()
	}
	return msg.GetDeleteStatus().String()
}

func (s *PostgresStore) GetInboundMessage(ctx context.Context, id string) (*waappv1.InboundMessage, error) {
	row := s.db.QueryRow(ctx, `SELECT message_id,message_session_id,kind,encryption_state,ack_status,direction,source,contact_ref,sender_ref,payload_ref,provider_message_id,provider_timestamp,read_at,delete_status,deleted_at,last_error_code,last_error_message,last_error_retryable,received_at,read_receipt_sent_at,delivery_receipt_sent_at FROM wa_inbound_messages WHERE message_id=$1`, id)
	msg, err := scanPostgresInboundMessage(row)
	if err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "message not found")
	}
	return msg, nil
}

func (s *PostgresStore) ListPendingEncryptedInboundMessages(ctx context.Context, waAccountIDValue string, clientProfileID string, limit int) ([]*waappv1.InboundMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.db.Query(ctx, `SELECT m.message_id,m.message_session_id,m.kind,m.encryption_state,m.ack_status,m.direction,m.source,m.contact_ref,m.sender_ref,m.payload_ref,m.provider_message_id,m.provider_timestamp,m.read_at,m.delete_status,m.deleted_at,m.last_error_code,m.last_error_message,m.last_error_retryable,m.received_at,m.read_receipt_sent_at,m.delivery_receipt_sent_at
FROM wa_inbound_messages m
JOIN wa_message_sessions s ON s.message_session_id=m.message_session_id
WHERE s.wa_account_id=$1 AND s.client_profile_id=$2
  AND (m.encryption_state='MESSAGE_ENCRYPTION_STATE_ENCRYPTED' OR (m.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTION_FAILED' AND m.last_error_retryable))
  AND m.direction='ACCOUNT_MESSAGE_DIRECTION_INBOUND' AND COALESCE(m.delete_status,'MESSAGE_DELETE_STATUS_NOT_DELETED')<>'MESSAGE_DELETE_STATUS_DELETED_FOR_ME' AND NOT EXISTS (
  SELECT 1 FROM wa_decrypted_messages d WHERE d.message_id=m.message_id
)
AND NOT EXISTS (
 SELECT 1 FROM wa_inbound_messages recovered JOIN wa_message_sessions owner ON owner.message_session_id=recovered.message_session_id
 JOIN wa_decrypted_messages d ON d.message_id=recovered.message_id
 WHERE m.provider_message_id<>'' AND recovered.provider_message_id=m.provider_message_id AND owner.wa_account_id=s.wa_account_id
 AND recovered.contact_ref=m.contact_ref AND recovered.sender_ref=m.sender_ref AND recovered.direction=m.direction
 AND recovered.encryption_state='MESSAGE_ENCRYPTION_STATE_DECRYPTED'
 AND (recovered.kind='INBOUND_MESSAGE_KIND_MESSAGE' OR d.content->'change'->>'target_provider_message_id' IS NOT NULL)
)
ORDER BY m.received_at ASC, m.message_id ASC
LIMIT $3`, waAccountIDValue, clientProfileID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := []*waappv1.InboundMessage{}
	for rows.Next() {
		var r messageRow
		if err := rows.Scan(&r.id, &r.sessionID, &r.kind, &r.encryptionState, &r.ackStatus, &r.direction, &r.source, &r.contactRef, &r.senderRef, &r.payloadRef, &r.providerMessageID, &r.providerTimestamp, &r.readAt, &r.deleteStatus, &r.deletedAt, &r.errCode, &r.errMessage, &r.errRetryable, &r.receivedAt, &r.readReceiptSentAt, &r.deliveryReceiptSentAt); err != nil {
			return nil, err
		}
		messages = append(messages, r.toProto())
	}
	return messages, rows.Err()
}

func (s *PostgresStore) SaveDecryptedMessage(ctx context.Context, msg *waappv1.DecryptedMessage) error {
	appErr := msg.GetLastError()
	content, err := marshalMessageContent(msg.GetContent())
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO wa_decrypted_messages (decrypted_message_id, message_id, status, plaintext_ref, plaintext_value, plaintext_redacted, plaintext_secret_ref, last_error_code, last_error_message, last_error_retryable, decrypted_at, content)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (decrypted_message_id) DO UPDATE SET status=EXCLUDED.status, plaintext_ref=EXCLUDED.plaintext_ref, plaintext_value=EXCLUDED.plaintext_value, plaintext_redacted=EXCLUDED.plaintext_redacted, plaintext_secret_ref=EXCLUDED.plaintext_secret_ref, last_error_code=EXCLUDED.last_error_code, last_error_message=EXCLUDED.last_error_message, last_error_retryable=EXCLUDED.last_error_retryable, decrypted_at=EXCLUDED.decrypted_at, content=EXCLUDED.content`,
		msg.GetDecryptedMessageId(), msg.GetMessageId(), msg.GetStatus().String(), msg.GetPlaintextRef(), msg.GetPlaintextText().GetValue(), msg.GetPlaintextText().GetRedactedValue(), msg.GetPlaintextText().GetSecretRef(), errCode(appErr), errMessage(appErr), errRetryable(appErr), shared.TimeFromProto(msg.GetDecryptedAt()), content)
	return err
}

func (s *PostgresStore) GetDecryptedMessage(ctx context.Context, id string) (*waappv1.DecryptedMessage, error) {
	row := s.db.QueryRow(ctx, `SELECT decrypted_message_id,message_id,status,plaintext_ref,plaintext_value,plaintext_redacted,plaintext_secret_ref,last_error_code,last_error_message,last_error_retryable,decrypted_at,content FROM wa_decrypted_messages WHERE decrypted_message_id=$1`, id)
	var r decryptedRow
	var content []byte
	if err := row.Scan(&r.id, &r.messageID, &r.status, &r.plaintextRef, &r.plaintext, &r.redacted, &r.secretRef, &r.errCode, &r.errMessage, &r.errRetryable, &r.decryptedAt, &content); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "decrypted message not found")
	}
	message := r.toProto()
	var err error
	message.Content, err = unmarshalMessageContent(content)
	return message, err
}

func (s *PostgresStore) SaveCandidates(ctx context.Context, candidates []*waappv1.ExtractedCandidate) error {
	if len(candidates) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, candidate := range candidates {
		batch.Queue(`INSERT INTO wa_extracted_candidates (candidate_id, message_id, decrypted_message_id, kind, redacted_value, secret_ref, confidence, extracted_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (candidate_id) DO UPDATE SET confidence=EXCLUDED.confidence, redacted_value=EXCLUDED.redacted_value, secret_ref=EXCLUDED.secret_ref`,
			candidate.GetCandidateId(), candidate.GetMessageId(), candidate.GetDecryptedMessageId(), candidate.GetKind().String(), candidate.GetText().GetRedactedValue(), candidate.GetText().GetSecretRef(), candidate.GetConfidence(), shared.TimeFromProto(candidate.GetExtractedAt()))
	}
	return s.pool.SendBatch(ctx, batch).Close()
}
