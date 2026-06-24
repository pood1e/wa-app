package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	_ "modernc.org/sqlite"
)

const sqliteStoreFileName = "wa-app.sqlite3"

type SQLiteStore struct{ db *sql.DB }

func NewSQLiteStore(ctx context.Context, dataDir string) (*SQLiteStore, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		dataDir = defaultWAAppDataDir
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	return NewSQLiteStoreFile(ctx, filepath.Join(dataDir, sqliteStoreFileName))
}

func NewSQLiteStoreFile(ctx context.Context, path string) (*SQLiteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("sqlite store path is required")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteStore{db: db}
	if err := store.configure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() {
	if s != nil && s.db != nil {
		_ = s.db.Close()
	}
}

func (s *SQLiteStore) configure(ctx context.Context) error {
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA busy_timeout=5000`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, sqliteStoreSchema)
	return err
}

func (s *SQLiteStore) SaveAppArtifact(ctx context.Context, artifact *waappv1.AppArtifact) error {
	return s.upsertPayload(ctx, "wa_sqlite_artifacts", artifact.GetArtifactId(), sqliteTimeValue(timeFromProto(artifact.GetObservedAt())), artifact)
}

func (s *SQLiteStore) GetAppArtifact(ctx context.Context, artifactID string) (*waappv1.AppArtifact, error) {
	artifact := &waappv1.AppArtifact{}
	return artifact, s.loadPayload(ctx, "wa_sqlite_artifacts", artifactID, artifact, waappv1.WaErrorCode_WA_ERROR_CODE_PROTOCOL_PROFILE_NOT_FOUND, "app artifact not found")
}

func (s *SQLiteStore) SaveProtocolProfile(ctx context.Context, profile *waappv1.ProtocolProfile) error {
	return s.upsertPayload(ctx, "wa_sqlite_protocol_profiles", profile.GetProtocolProfileId(), sqliteTimeValue(timeFromProto(profile.GetAudit().GetUpdatedAt())), profile)
}

func (s *SQLiteStore) GetProtocolProfile(ctx context.Context, id string) (*waappv1.ProtocolProfile, error) {
	profile := &waappv1.ProtocolProfile{}
	return profile, s.loadPayload(ctx, "wa_sqlite_protocol_profiles", id, profile, waappv1.WaErrorCode_WA_ERROR_CODE_PROTOCOL_PROFILE_NOT_FOUND, "protocol profile not found")
}

func (s *SQLiteStore) SaveWAAccount(ctx context.Context, account *waappv1.WAAccount) error {
	payload, err := sqliteMarshal(account)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_accounts (id,e164,updated_at,payload) VALUES (?,?,?,?)
ON CONFLICT(id) DO UPDATE SET e164=excluded.e164, updated_at=excluded.updated_at, payload=excluded.payload`, waAccountID(account), account.GetPhone().GetE164Number(), sqliteTimeValue(waAccountUpdatedAt(account)), payload)
	return err
}

func (s *SQLiteStore) GetWAAccount(ctx context.Context, waAccountIDValue string) (*waappv1.WAAccount, error) {
	return s.getWAAccountBy(ctx, `id=?`, waAccountIDValue)
}

func (s *SQLiteStore) FindWAAccountByPhone(ctx context.Context, e164 string) (*waappv1.WAAccount, error) {
	return s.getWAAccountBy(ctx, `e164=?`, e164)
}

func (s *SQLiteStore) ListWAAccounts(ctx context.Context, cursorValue string, limit int) ([]*waappv1.WAAccount, string, error) {
	cursor, err := decodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = normalizePageLimit(limit)
	lookahead := keysetLookaheadLimit(limit)
	query := `SELECT payload FROM wa_sqlite_accounts`
	args := []any{}
	if hasKeysetCursor(cursor) {
		query += ` WHERE (updated_at < ? OR (updated_at = ? AND id < ?))`
		value := sqliteTimeValue(cursor.UpdatedAt)
		args = append(args, value, value, cursor.ID)
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, lookahead)
	items, err := sqliteListPayloads(ctx, s.db, func() *waappv1.WAAccount { return &waappv1.WAAccount{} }, query, args...)
	if err != nil {
		return nil, "", err
	}
	items, nextCursor := newKeysetPage(items, limit, func(account *waappv1.WAAccount) keysetCursor {
		return keysetCursorValue(waAccountUpdatedAt(account), waAccountID(account))
	})
	return items, nextCursor, nil
}

func (s *SQLiteStore) DeleteWAAccount(ctx context.Context, waAccountIDValue string) error {
	if _, err := s.GetWAAccount(ctx, waAccountIDValue); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range sqliteDeleteWAAccountStatements {
		if _, err := tx.ExecContext(ctx, statement, waAccountIDValue); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) SaveClientProfile(ctx context.Context, profile *waappv1.ClientProfile) error {
	payload, err := sqliteMarshal(profile)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_client_profiles (id,wa_account_id,protocol_profile_id,status,updated_at,payload) VALUES (?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET wa_account_id=excluded.wa_account_id, protocol_profile_id=excluded.protocol_profile_id, status=excluded.status, updated_at=excluded.updated_at, payload=excluded.payload`, profile.GetClientProfileId(), profile.GetWaAccountId(), profile.GetProtocolProfileId(), profile.GetStatus().String(), sqliteTimeValue(timeFromProto(profile.GetAudit().GetUpdatedAt())), payload)
	return err
}

func (s *SQLiteStore) GetClientProfile(ctx context.Context, id string) (*waappv1.ClientProfile, error) {
	profile := &waappv1.ClientProfile{}
	return profile, s.loadPayload(ctx, "wa_sqlite_client_profiles", id, profile, waappv1.WaErrorCode_WA_ERROR_CODE_PROFILE_NOT_FOUND, "client profile not found")
}

func (s *SQLiteStore) ListClientProfiles(ctx context.Context, waAccountIDValue string, cursorValue string, limit int) ([]*waappv1.ClientProfile, string, error) {
	cursor, err := decodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = normalizePageLimit(limit)
	lookahead := keysetLookaheadLimit(limit)
	query := `SELECT payload FROM wa_sqlite_client_profiles WHERE wa_account_id=?`
	args := []any{waAccountIDValue}
	if hasKeysetCursor(cursor) {
		value := sqliteTimeValue(cursor.UpdatedAt)
		query += ` AND (updated_at < ? OR (updated_at = ? AND id < ?))`
		args = append(args, value, value, cursor.ID)
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, lookahead)
	items, err := sqliteListPayloads(ctx, s.db, func() *waappv1.ClientProfile { return &waappv1.ClientProfile{} }, query, args...)
	if err != nil {
		return nil, "", err
	}
	items, nextCursor := newKeysetPage(items, limit, func(profile *waappv1.ClientProfile) keysetCursor {
		return keysetCursorValue(timeFromProto(profile.GetAudit().GetUpdatedAt()), profile.GetClientProfileId())
	})
	return items, nextCursor, nil
}

func (s *SQLiteStore) SaveNativeState(ctx context.Context, clientProfileID string, state nativeState) error {
	data, err := marshalNativeState(state)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_native_states (client_profile_id,state_json,updated_at) VALUES (?,?,?)
ON CONFLICT(client_profile_id) DO UPDATE SET state_json=excluded.state_json, updated_at=excluded.updated_at`, clientProfileID, string(data), sqliteTimeValue(time.Now().UTC()))
	return err
}

func (s *SQLiteStore) GetNativeState(ctx context.Context, clientProfileID string) (nativeState, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT state_json FROM wa_sqlite_native_states WHERE client_profile_id=?`, clientProfileID).Scan(&data)
	if err != nil {
		return nativeState{}, sqliteNotFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_PROFILE_NOT_FOUND, "native state not found")
	}
	return unmarshalNativeState([]byte(data))
}

func (s *SQLiteStore) SaveAccountProbe(ctx context.Context, probe *waappv1.AccountProbe) error {
	payload, err := sqliteMarshal(probe)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_account_probes (id,wa_account_id,client_profile_id,updated_at,payload) VALUES (?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET wa_account_id=excluded.wa_account_id, client_profile_id=excluded.client_profile_id, updated_at=excluded.updated_at, payload=excluded.payload`, probe.GetAccountProbeId(), probe.GetWaAccountId(), probe.GetClientProfileId(), sqliteTimeValue(timeFromProto(probe.GetProbedAt())), payload)
	return err
}

func (s *SQLiteStore) SaveVerificationRequest(ctx context.Context, record *waappv1.VerificationCodeRequestRecord) error {
	payload, err := sqliteMarshal(record)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_verification_requests (id,wa_account_id,client_profile_id,requested_at,payload) VALUES (?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET wa_account_id=excluded.wa_account_id, client_profile_id=excluded.client_profile_id, requested_at=excluded.requested_at, payload=excluded.payload`, record.GetVerificationRequestId(), record.GetWaAccountId(), record.GetClientProfileId(), sqliteTimeValue(timeFromProto(record.GetRequestedAt())), payload)
	return err
}

func (s *SQLiteStore) GetVerificationRequest(ctx context.Context, id string) (*waappv1.VerificationCodeRequestRecord, error) {
	record := &waappv1.VerificationCodeRequestRecord{}
	return record, s.loadPayload(ctx, "wa_sqlite_verification_requests", id, record, waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND, "verification request not found")
}

func (s *SQLiteStore) SaveRegistration(ctx context.Context, record *waappv1.RegistrationRecord) error {
	payload, err := sqliteMarshal(record)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_registrations (id,verification_request_id,wa_account_id,client_profile_id,submitted_at,payload) VALUES (?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET verification_request_id=excluded.verification_request_id, wa_account_id=excluded.wa_account_id, client_profile_id=excluded.client_profile_id, submitted_at=excluded.submitted_at, payload=excluded.payload`, record.GetRegistrationId(), record.GetVerificationRequestId(), record.GetWaAccountId(), record.GetClientProfileId(), sqliteTimeValue(timeFromProto(record.GetSubmittedAt())), payload)
	return err
}

func (s *SQLiteStore) GetRegistration(ctx context.Context, id string) (*waappv1.RegistrationRecord, error) {
	record := &waappv1.RegistrationRecord{}
	return record, s.loadPayload(ctx, "wa_sqlite_registrations", id, record, waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND, "registration not found")
}

func (s *SQLiteStore) SaveLoginState(ctx context.Context, state *waappv1.LoginState, _ string) error {
	payload, err := sqliteMarshal(state)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_login_states (id,registration_id,wa_account_id,client_profile_id,registered_identity_id,status,updated_at,payload) VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET registration_id=excluded.registration_id, wa_account_id=excluded.wa_account_id, client_profile_id=excluded.client_profile_id, registered_identity_id=excluded.registered_identity_id, status=excluded.status, updated_at=excluded.updated_at, payload=excluded.payload`, state.GetLoginStateId(), state.GetRegistrationId(), state.GetWaAccountId(), state.GetClientProfileId(), state.GetRegisteredIdentityId(), state.GetStatus().String(), sqliteTimeValue(timeFromProto(state.GetAudit().GetUpdatedAt())), payload)
	return err
}

func (s *SQLiteStore) GetLoginState(ctx context.Context, id string) (*waappv1.LoginState, error) {
	return s.getLoginStateBy(ctx, `id=?`, id)
}

func (s *SQLiteStore) GetActiveLoginState(ctx context.Context, waAccountIDValue string, clientProfileID string) (*waappv1.LoginState, error) {
	return s.getLoginStateBy(ctx, `wa_account_id=? AND client_profile_id=? AND status=?`, waAccountIDValue, clientProfileID, waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE.String())
}

func (s *SQLiteStore) ListActiveLoginStates(ctx context.Context) ([]LoginStateRecord, error) {
	return s.listLoginStatesByStatus(ctx, waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE)
}

func (s *SQLiteStore) ListRevokedLoginStates(ctx context.Context) ([]LoginStateRecord, error) {
	return s.listLoginStatesByStatus(ctx, waappv1.LoginStateStatus_LOGIN_STATE_STATUS_REVOKED)
}

func (s *SQLiteStore) listLoginStatesByStatus(ctx context.Context, status waappv1.LoginStateStatus) ([]LoginStateRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM wa_sqlite_login_states WHERE status=? ORDER BY updated_at DESC`, status.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LoginStateRecord{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		state := &waappv1.LoginState{}
		if err := sqliteUnmarshal([]byte(payload), state); err != nil {
			return nil, err
		}
		out = append(out, LoginStateRecord{LoginState: state})
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetLoginStateByRegistration(ctx context.Context, registrationID string) (*waappv1.LoginState, error) {
	return s.getLoginStateBy(ctx, `registration_id=?`, registrationID)
}

func (s *SQLiteStore) GetLoginStateByRegisteredIdentity(ctx context.Context, registeredIdentityID string) (*waappv1.LoginState, error) {
	return s.getLoginStateBy(ctx, `registered_identity_id=?`, registeredIdentityID)
}

func (s *SQLiteStore) SaveMessageSession(ctx context.Context, session *waappv1.MessageSession) error {
	payload, err := sqliteMarshal(session)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_message_sessions (id,wa_account_id,client_profile_id,registered_identity_id,protocol_profile_id,status,updated_at,payload) VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET status=excluded.status, updated_at=excluded.updated_at, payload=excluded.payload`, session.GetMessageSessionId(), session.GetWaAccountId(), session.GetClientProfileId(), session.GetRegisteredIdentityId(), session.GetProtocolProfileId(), session.GetStatus().String(), sqliteTimeValue(timeFromProto(firstProtoTime(session.GetLastSeenAt(), session.GetOpenedAt(), session.GetClosedAt()))), payload)
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
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM wa_sqlite_message_sessions WHERE status=? AND updated_at<?`, waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_OPEN.String(), sqliteTimeValue(before.UTC()))
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
ON CONFLICT(id) DO UPDATE SET encryption_state=excluded.encryption_state, payload=excluded.payload`, msg.GetMessageId(), msg.GetMessageSessionId(), msg.GetEncryptionState().String(), sqliteTimeValue(timeFromProto(msg.GetReceivedAt())), payload); err != nil {
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

func mergeInboundMessageState(existing *waappv1.InboundMessage, next *waappv1.InboundMessage) {
	if existing == nil || next == nil {
		return
	}
	if next.GetProviderMessageId() == "" {
		next.ProviderMessageId = existing.GetProviderMessageId()
	}
	if next.GetProviderTimestamp() == nil {
		next.ProviderTimestamp = existing.GetProviderTimestamp()
	}
	if next.GetReadAt() == nil {
		next.ReadAt = existing.GetReadAt()
	}
	if next.GetDeleteStatus() == waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_UNSPECIFIED {
		next.DeleteStatus = existing.GetDeleteStatus()
	}
	if next.GetDeleteStatus() == waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_UNSPECIFIED {
		next.DeleteStatus = waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_NOT_DELETED
	}
	if next.GetDeletedAt() == nil {
		next.DeletedAt = existing.GetDeletedAt()
	}
}

func (s *SQLiteStore) GetInboundMessage(ctx context.Context, id string) (*waappv1.InboundMessage, error) {
	msg := &waappv1.InboundMessage{}
	return msg, s.loadPayload(ctx, "wa_sqlite_inbound_messages", id, msg, waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "message not found")
}

func (s *SQLiteStore) ListPendingEncryptedInboundMessages(ctx context.Context, waAccountIDValue string, clientProfileID string, limit int) ([]*waappv1.InboundMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	return sqliteListPayloads(ctx, s.db, func() *waappv1.InboundMessage { return &waappv1.InboundMessage{} }, `SELECT m.payload FROM wa_sqlite_inbound_messages m
JOIN wa_sqlite_message_sessions s ON s.id=m.message_session_id
LEFT JOIN wa_sqlite_decrypted_messages d ON d.message_id=m.id
WHERE s.wa_account_id=? AND s.client_profile_id=? AND m.encryption_state=? AND COALESCE(json_extract(m.payload, '$.direction'), 'ACCOUNT_MESSAGE_DIRECTION_INBOUND')='ACCOUNT_MESSAGE_DIRECTION_INBOUND' AND COALESCE(json_extract(m.payload, '$.delete_status'), 'MESSAGE_DELETE_STATUS_NOT_DELETED')<>'MESSAGE_DELETE_STATUS_DELETED_FOR_ME' AND d.id IS NULL
ORDER BY m.received_at ASC, m.id ASC LIMIT ?`, waAccountIDValue, clientProfileID, waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_ENCRYPTED.String(), limit)
}

func (s *SQLiteStore) SaveDecryptedMessage(ctx context.Context, msg *waappv1.DecryptedMessage) error {
	payload, err := sqliteMarshal(msg)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_decrypted_messages (id,message_id,decrypted_at,payload) VALUES (?,?,?,?)
ON CONFLICT(id) DO UPDATE SET message_id=excluded.message_id, decrypted_at=excluded.decrypted_at, payload=excluded.payload`, msg.GetDecryptedMessageId(), msg.GetMessageId(), sqliteTimeValue(timeFromProto(msg.GetDecryptedAt())), payload)
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
ON CONFLICT(id) DO UPDATE SET decrypted_message_id=excluded.decrypted_message_id, extracted_at=excluded.extracted_at, payload=excluded.payload`, candidate.GetCandidateId(), candidate.GetMessageId(), candidate.GetDecryptedMessageId(), sqliteTimeValue(timeFromProto(candidate.GetExtractedAt())), payload); err != nil {
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
	stored.OtpMessageId = firstNonEmpty(stored.GetOtpMessageId(), stableOTPMessageID(stored.GetWaAccountId(), stored.GetSourceParty(), otpValue))
	if stored.Otp == nil {
		stored.Otp = &waappv1.SensitiveText{}
	}
	stored.Otp.Value = otpValue
	stored.Otp.RedactedValue = firstNonEmpty(stored.GetOtp().GetRedactedValue(), redacted(otpValue))
	stored.Otp.SecretRef = firstNonEmpty(stored.GetOtp().GetSecretRef(), "wa-otp:"+stableID(stored.GetOtpMessageId()))
	if stored.ReceivedAt == nil {
		stored.ReceivedAt = timestamppb.New(time.Now().UTC())
	}
	payload, err := sqliteMarshal(stored)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_otp_messages (id,wa_account_id,received_at,payload) VALUES (?,?,?,?)
ON CONFLICT(id) DO UPDATE SET wa_account_id=excluded.wa_account_id, received_at=excluded.received_at, payload=excluded.payload`, stored.GetOtpMessageId(), stored.GetWaAccountId(), sqliteTimeValue(timeFromProto(stored.GetReceivedAt())), payload)
	return err
}

func (s *SQLiteStore) ListAccountOTPMessages(ctx context.Context, waAccountIDValue string, cursorValue string, limit int, includeSensitiveValues bool) ([]*waappv1.OtpMessage, string, error) {
	cursor, err := decodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = normalizePageLimit(limit)
	lookahead := keysetLookaheadLimit(limit)
	query := `SELECT o.payload FROM wa_sqlite_otp_messages o
WHERE o.wa_account_id=? AND NOT EXISTS (
  SELECT 1 FROM wa_sqlite_inbound_messages m
  WHERE m.id=json_extract(o.payload, '$.message_id') AND COALESCE(json_extract(m.payload, '$.delete_status'), 'MESSAGE_DELETE_STATUS_NOT_DELETED')='MESSAGE_DELETE_STATUS_DELETED_FOR_ME'
)`
	args := []any{waAccountIDValue}
	if hasKeysetCursor(cursor) {
		value := sqliteTimeValue(cursor.UpdatedAt)
		query += ` AND (o.received_at < ? OR (o.received_at = ? AND o.id < ?))`
		args = append(args, value, value, cursor.ID)
	}
	query += ` ORDER BY o.received_at DESC, o.id DESC LIMIT ?`
	args = append(args, lookahead)
	items, err := sqliteListPayloads(ctx, s.db, func() *waappv1.OtpMessage { return &waappv1.OtpMessage{} }, query, args...)
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
	items, nextCursor := newKeysetPage(items, limit, func(msg *waappv1.OtpMessage) keysetCursor {
		return keysetCursorValue(timeFromProto(msg.GetReceivedAt()), msg.GetOtpMessageId())
	})
	return items, nextCursor, nil
}

func (s *SQLiteStore) getWAAccountBy(ctx context.Context, where string, args ...any) (*waappv1.WAAccount, error) {
	account := &waappv1.WAAccount{}
	return account, s.loadPayloadWhere(ctx, "wa_sqlite_accounts", where, args, account, waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, "WA account not found")
}

func (s *SQLiteStore) getLoginStateBy(ctx context.Context, where string, args ...any) (*waappv1.LoginState, error) {
	state := &waappv1.LoginState{}
	return state, s.loadPayloadWhere(ctx, "wa_sqlite_login_states", where, args, state, waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND, "login state not found")
}

func (s *SQLiteStore) upsertPayload(ctx context.Context, table string, id string, updatedAt int64, msg proto.Message) error {
	payload, err := sqliteMarshal(msg)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id,updated_at,payload) VALUES (?,?,?)
ON CONFLICT(id) DO UPDATE SET updated_at=excluded.updated_at, payload=excluded.payload`, table), id, updatedAt, payload)
	return err
}

func (s *SQLiteStore) loadPayload(ctx context.Context, table string, id string, msg proto.Message, code waappv1.WaErrorCode, message string) error {
	return s.loadPayloadWhere(ctx, table, `id=?`, []any{id}, msg, code, message)
}

func (s *SQLiteStore) loadPayloadWhere(ctx context.Context, table string, where string, args []any, msg proto.Message, code waappv1.WaErrorCode, message string) error {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM `+table+` WHERE `+where+` LIMIT 1`, args...).Scan(&payload)
	if err != nil {
		return sqliteNotFound(err, code, message)
	}
	return sqliteUnmarshal([]byte(payload), msg)
}

func sqliteListPayloads[T proto.Message](ctx context.Context, db *sql.DB, factory func() T, query string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		item := factory()
		if err := sqliteUnmarshal([]byte(payload), item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func sqliteMarshal(msg proto.Message) (string, error) {
	data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func sqliteUnmarshal(data []byte, msg proto.Message) error {
	return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(data, msg)
}

func sqliteNotFound(err error, code waappv1.WaErrorCode, message string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return NewError(code, message, false)
	}
	return err
}

func sqliteTimeValue(value time.Time) int64 {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return value.UTC().UnixNano()
}

func firstProtoTime(values ...*timestamppb.Timestamp) *timestamppb.Timestamp {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
