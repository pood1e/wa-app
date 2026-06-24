package app

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"
)

//go:embed migrations/001_init.sql
var postgresStoreSchema string

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("WA_APP_PG_DSN is required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	store := &PostgresStore{pool: pool}
	if err := runPostgresMigrations(ctx, cfg); err != nil {
		pool.Close()
		return nil, err
	}
	if err := store.validate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func runPostgresMigrations(ctx context.Context, cfg *pgxpool.Config) error {
	if strings.TrimSpace(postgresStoreSchema) == "" {
		return nil
	}
	connConfig := cfg.ConnConfig.Copy()
	connConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, connConfig)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err = tx.Exec(ctx, postgresStoreSchema); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresStore) validate(ctx context.Context) error {
	for _, table := range []string{
		"wa_app_artifacts", "wa_protocol_profiles", "wa_accounts", "wa_client_profiles", "wa_account_probes",
		"wa_client_profile_states", "wa_verification_requests", "wa_registrations", "wa_login_states", "wa_message_sessions", "wa_inbound_messages",
		"wa_decrypted_messages", "wa_extracted_candidates", "wa_otp_messages", "wa_contacts",
	} {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("required wa-app table %s is missing; apply migrations first", table)
		}
	}
	return nil
}

func (s *PostgresStore) SaveAppArtifact(ctx context.Context, artifact *waappv1.AppArtifact) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO wa_app_artifacts (artifact_id, label, version_label, sha256, observed_at)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (artifact_id) DO UPDATE SET label=EXCLUDED.label, version_label=EXCLUDED.version_label, sha256=EXCLUDED.sha256, observed_at=EXCLUDED.observed_at`,
		artifact.GetArtifactId(), artifact.GetLabel(), artifact.GetVersionLabel(), artifact.GetSha256(), timeFromProto(artifact.GetObservedAt()))
	return err
}

func (s *PostgresStore) GetAppArtifact(ctx context.Context, artifactID string) (*waappv1.AppArtifact, error) {
	row := s.pool.QueryRow(ctx, `SELECT artifact_id,label,version_label,sha256,observed_at FROM wa_app_artifacts WHERE artifact_id=$1`, artifactID)
	var id, label, version, hash string
	var observed time.Time
	if err := row.Scan(&id, &label, &version, &hash, &observed); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_PROTOCOL_PROFILE_NOT_FOUND, "app artifact not found")
	}
	return &waappv1.AppArtifact{ArtifactId: id, Label: label, VersionLabel: version, Sha256: hash, ObservedAt: timestamppb.New(observed)}, nil
}

func (s *PostgresStore) SaveProtocolProfile(ctx context.Context, profile *waappv1.ProtocolProfile) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO wa_protocol_profiles (protocol_profile_id, app_artifact_id, display_name, app_version, status, capabilities, registration_flows, message_transports, discovered_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (protocol_profile_id) DO UPDATE SET display_name=EXCLUDED.display_name, app_version=EXCLUDED.app_version, status=EXCLUDED.status, capabilities=EXCLUDED.capabilities, registration_flows=EXCLUDED.registration_flows, message_transports=EXCLUDED.message_transports, updated_at=EXCLUDED.updated_at`,
		profile.GetProtocolProfileId(), profile.GetAppArtifactId(), profile.GetDisplayName(), profile.GetAppVersion(), profile.GetStatus().String(), protocolCapabilities(profile.GetCapabilities()), registrationFlows(profile.GetRegistrationFlows()), messageTransports(profile.GetMessageTransports()), timeFromProto(profile.GetDiscoveredAt()), timeFromProto(profile.GetAudit().GetCreatedAt()), timeFromProto(profile.GetAudit().GetUpdatedAt()))
	return err
}

func (s *PostgresStore) GetProtocolProfile(ctx context.Context, id string) (*waappv1.ProtocolProfile, error) {
	row := s.pool.QueryRow(ctx, `SELECT protocol_profile_id,app_artifact_id,display_name,app_version,status,capabilities,registration_flows,message_transports,discovered_at,created_at,updated_at FROM wa_protocol_profiles WHERE protocol_profile_id=$1`, id)
	var r protocolProfileRow
	if err := row.Scan(&r.id, &r.artifactID, &r.displayName, &r.appVersion, &r.status, &r.capabilities, &r.registrationFlows, &r.messageTransports, &r.discoveredAt, &r.createdAt, &r.updatedAt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_PROTOCOL_PROFILE_NOT_FOUND, "protocol profile not found")
	}
	return r.toProto(), nil
}

func (s *PostgresStore) SaveWAAccount(ctx context.Context, account *waappv1.WAAccount) error {
	createdAt := waAccountCreatedAt(account)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := waAccountUpdatedAt(account)
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO wa_accounts (wa_account_id, display_name, e164_number, country_calling_code, national_number, country_iso2, status, two_factor_auth_configured, two_factor_email_configured, two_factor_email_address, two_factor_email_verified, two_factor_email_confirmed, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (e164_number) DO UPDATE SET display_name=COALESCE(NULLIF(EXCLUDED.display_name,''), wa_accounts.display_name), country_calling_code=EXCLUDED.country_calling_code, national_number=EXCLUDED.national_number, country_iso2=EXCLUDED.country_iso2, status=EXCLUDED.status, two_factor_auth_configured=COALESCE(EXCLUDED.two_factor_auth_configured, wa_accounts.two_factor_auth_configured), two_factor_email_configured=COALESCE(EXCLUDED.two_factor_email_configured, wa_accounts.two_factor_email_configured), two_factor_email_address=COALESCE(EXCLUDED.two_factor_email_address, wa_accounts.two_factor_email_address), two_factor_email_verified=COALESCE(EXCLUDED.two_factor_email_verified, wa_accounts.two_factor_email_verified), two_factor_email_confirmed=COALESCE(EXCLUDED.two_factor_email_confirmed, wa_accounts.two_factor_email_confirmed), updated_at=EXCLUDED.updated_at`,
		waAccountID(account), account.GetDisplayName(), account.GetPhone().GetE164Number(), account.GetPhone().GetCountryCallingCode(), account.GetPhone().GetNationalNumber(), account.GetPhone().GetCountryIso2(), waAccountStatusStorageValue(account), nullableTwoFactorConfigured(account.GetTwoFactorAuth()), nullableTwoFactorEmailConfigured(account.GetTwoFactorAuth()), nullableTwoFactorEmailAddress(account.GetTwoFactorAuth()), nullableTwoFactorEmailVerified(account.GetTwoFactorAuth()), nullableTwoFactorEmailConfirmed(account.GetTwoFactorAuth()), createdAt, updatedAt)
	return err
}

func (s *PostgresStore) GetWAAccount(ctx context.Context, waAccountIDValue string) (*waappv1.WAAccount, error) {
	return s.getWAAccount(ctx, `wa_account_id=$1`, waAccountIDValue)
}

func (s *PostgresStore) FindWAAccountByPhone(ctx context.Context, e164 string) (*waappv1.WAAccount, error) {
	return s.getWAAccount(ctx, `e164_number=$1`, e164)
}

func (s *PostgresStore) ListWAAccounts(ctx context.Context, cursorValue string, limit int) ([]*waappv1.WAAccount, string, error) {
	cursor, err := decodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = normalizePageLimit(limit)
	rows, err := s.queryWAAccountPage(ctx, cursor, keysetLookaheadLimit(limit))
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	accounts := []*waappv1.WAAccount{}
	for rows.Next() {
		var r waAccountRow
		if err := rows.Scan(&r.id, &r.displayName, &r.e164, &r.cc, &r.national, &r.iso2, &r.status, &r.twoFactorConfigured, &r.twoFactorEmailConfigured, &r.twoFactorEmailAddress, &r.twoFactorEmailVerified, &r.twoFactorEmailConfirmed, &r.createdAt, &r.updatedAt); err != nil {
			return nil, "", err
		}
		accounts = append(accounts, r.toProto())
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	items, nextCursor := newKeysetPage(accounts, limit, func(account *waappv1.WAAccount) keysetCursor {
		return keysetCursorValue(waAccountUpdatedAt(account), waAccountID(account))
	})
	return items, nextCursor, nil
}

func (s *PostgresStore) queryWAAccountPage(ctx context.Context, cursor keysetCursor, limit int) (pgx.Rows, error) {
	const base = `SELECT wa_account_id,display_name,e164_number,country_calling_code,national_number,country_iso2,status,two_factor_auth_configured,two_factor_email_configured,two_factor_email_address,two_factor_email_verified,two_factor_email_confirmed,created_at,updated_at FROM wa_accounts`
	if !hasKeysetCursor(cursor) {
		return s.pool.Query(ctx, base+` ORDER BY updated_at DESC, wa_account_id DESC LIMIT $1`, limit)
	}
	return s.pool.Query(ctx, base+` WHERE (updated_at, wa_account_id) < ($1, $2) ORDER BY updated_at DESC, wa_account_id DESC LIMIT $3`, cursor.UpdatedAt, cursor.ID, limit)
}

func (s *PostgresStore) getWAAccount(ctx context.Context, where string, args ...any) (*waappv1.WAAccount, error) {
	row := s.pool.QueryRow(ctx, `SELECT wa_account_id,display_name,e164_number,country_calling_code,national_number,country_iso2,status,two_factor_auth_configured,two_factor_email_configured,two_factor_email_address,two_factor_email_verified,two_factor_email_confirmed,created_at,updated_at FROM wa_accounts WHERE `+where, args...)
	var r waAccountRow
	if err := row.Scan(&r.id, &r.displayName, &r.e164, &r.cc, &r.national, &r.iso2, &r.status, &r.twoFactorConfigured, &r.twoFactorEmailConfigured, &r.twoFactorEmailAddress, &r.twoFactorEmailVerified, &r.twoFactorEmailConfirmed, &r.createdAt, &r.updatedAt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, "WA account not found")
	}
	return r.toProto(), nil
}

func (s *PostgresStore) SaveClientProfile(ctx context.Context, profile *waappv1.ClientProfile) error {
	appErr := errorFromProto(profile.GetLastError())
	_, err := s.pool.Exec(ctx, `INSERT INTO wa_client_profiles (client_profile_id, wa_account_id, protocol_profile_id, status, registration_key_state, messaging_key_state, last_error_code, last_error_message, last_error_retryable, last_used_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (client_profile_id) DO UPDATE SET status=EXCLUDED.status, registration_key_state=EXCLUDED.registration_key_state, messaging_key_state=EXCLUDED.messaging_key_state, last_error_code=EXCLUDED.last_error_code, last_error_message=EXCLUDED.last_error_message, last_error_retryable=EXCLUDED.last_error_retryable, last_used_at=EXCLUDED.last_used_at, updated_at=EXCLUDED.updated_at`,
		profile.GetClientProfileId(), profile.GetWaAccountId(), profile.GetProtocolProfileId(), profile.GetStatus().String(), profile.GetRegistrationKeyState().String(), profile.GetMessagingKeyState().String(), errCode(appErr), errMessage(appErr), errRetryable(appErr), nullableProtoTime(profile.GetLastUsedAt()), timeFromProto(profile.GetAudit().GetCreatedAt()), timeFromProto(profile.GetAudit().GetUpdatedAt()))
	return err
}

func (s *PostgresStore) GetClientProfile(ctx context.Context, id string) (*waappv1.ClientProfile, error) {
	row := s.pool.QueryRow(ctx, `SELECT client_profile_id,wa_account_id,protocol_profile_id,status,registration_key_state,messaging_key_state,last_error_code,last_error_message,last_error_retryable,last_used_at,created_at,updated_at FROM wa_client_profiles WHERE client_profile_id=$1`, id)
	var r clientProfileRow
	if err := row.Scan(&r.id, &r.waAccountIDValue, &r.protocolProfileID, &r.status, &r.registrationKeyState, &r.messagingKeyState, &r.errCode, &r.errMessage, &r.errRetryable, &r.lastUsedAt, &r.createdAt, &r.updatedAt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_PROFILE_NOT_FOUND, "client profile not found")
	}
	return r.toProto(), nil
}

func (s *PostgresStore) ListClientProfiles(ctx context.Context, waAccountIDValue string, cursorValue string, limit int) ([]*waappv1.ClientProfile, string, error) {
	cursor, err := decodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = normalizePageLimit(limit)
	rows, err := s.queryClientProfilePage(ctx, waAccountIDValue, cursor, keysetLookaheadLimit(limit))
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
	items, nextCursor := newKeysetPage(items, limit, func(profile *waappv1.ClientProfile) keysetCursor {
		return keysetCursorValue(timeFromProto(profile.GetAudit().GetUpdatedAt()), profile.GetClientProfileId())
	})
	return items, nextCursor, nil
}

func (s *PostgresStore) queryClientProfilePage(ctx context.Context, waAccountIDValue string, cursor keysetCursor, limit int) (pgx.Rows, error) {
	const base = `SELECT client_profile_id,wa_account_id,protocol_profile_id,status,registration_key_state,messaging_key_state,last_error_code,last_error_message,last_error_retryable,last_used_at,created_at,updated_at FROM wa_client_profiles WHERE wa_account_id=$1`
	if !hasKeysetCursor(cursor) {
		return s.pool.Query(ctx, base+` ORDER BY updated_at DESC, client_profile_id DESC LIMIT $2`, waAccountIDValue, limit)
	}
	return s.pool.Query(ctx, base+` AND (updated_at, client_profile_id) < ($2, $3) ORDER BY updated_at DESC, client_profile_id DESC LIMIT $4`, waAccountIDValue, cursor.UpdatedAt, cursor.ID, limit)
}

func (s *PostgresStore) SaveNativeState(ctx context.Context, clientProfileID string, state nativeState) error {
	data, err := marshalNativeState(state)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO wa_client_profile_states (client_profile_id, state_json, created_at, updated_at)
VALUES ($1,$2::jsonb,now(),now())
ON CONFLICT (client_profile_id) DO UPDATE SET state_json=EXCLUDED.state_json, updated_at=EXCLUDED.updated_at`, clientProfileID, string(data))
	return err
}

func (s *PostgresStore) GetNativeState(ctx context.Context, clientProfileID string) (nativeState, error) {
	row := s.pool.QueryRow(ctx, `SELECT state_json FROM wa_client_profile_states WHERE client_profile_id=$1`, clientProfileID)
	var data []byte
	if err := row.Scan(&data); err != nil {
		return nativeState{}, err
	}
	return unmarshalNativeState(data)
}

func (s *PostgresStore) SaveAccountProbe(ctx context.Context, probe *waappv1.AccountProbe) error {
	appErr := errorFromProto(probe.GetLastError())
	_, err := s.pool.Exec(ctx, `INSERT INTO wa_account_probes (account_probe_id, wa_account_id, client_profile_id, status, supported_methods, last_error_code, last_error_message, last_error_retryable, probed_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (account_probe_id) DO UPDATE SET status=EXCLUDED.status, supported_methods=EXCLUDED.supported_methods, last_error_code=EXCLUDED.last_error_code, last_error_message=EXCLUDED.last_error_message, last_error_retryable=EXCLUDED.last_error_retryable, probed_at=EXCLUDED.probed_at`,
		probe.GetAccountProbeId(), probe.GetWaAccountId(), probe.GetClientProfileId(), probe.GetStatus().String(), deliveryMethods(probe.GetSupportedMethods()), errCode(appErr), errMessage(appErr), errRetryable(appErr), timeFromProto(probe.GetProbedAt()))
	return err
}

func (s *PostgresStore) SaveVerificationRequest(ctx context.Context, record *waappv1.VerificationCodeRequestRecord) error {
	appErr := errorFromProto(record.GetLastError())
	_, err := s.pool.Exec(ctx, `INSERT INTO wa_verification_requests (verification_request_id, wa_account_id, client_profile_id, delivery_method, status, expected_code_length, retry_after_seconds, last_error_code, last_error_message, last_error_retryable, requested_at, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (verification_request_id) DO UPDATE SET status=EXCLUDED.status, expected_code_length=EXCLUDED.expected_code_length, retry_after_seconds=EXCLUDED.retry_after_seconds, last_error_code=EXCLUDED.last_error_code, last_error_message=EXCLUDED.last_error_message, last_error_retryable=EXCLUDED.last_error_retryable, expires_at=EXCLUDED.expires_at`,
		record.GetVerificationRequestId(), record.GetWaAccountId(), record.GetClientProfileId(), record.GetDeliveryMethod().String(), record.GetStatus().String(), record.GetExpectedCodeLength(), durationSeconds(record.GetRetryAfter()), errCode(appErr), errMessage(appErr), errRetryable(appErr), timeFromProto(record.GetRequestedAt()), nullableProtoTime(record.GetExpiresAt()))
	return err
}

func (s *PostgresStore) GetVerificationRequest(ctx context.Context, id string) (*waappv1.VerificationCodeRequestRecord, error) {
	row := s.pool.QueryRow(ctx, `SELECT verification_request_id,wa_account_id,client_profile_id,delivery_method,status,expected_code_length,retry_after_seconds,last_error_code,last_error_message,last_error_retryable,requested_at,expires_at FROM wa_verification_requests WHERE verification_request_id=$1`, id)
	var r verificationRow
	if err := row.Scan(&r.id, &r.waAccountIDValue, &r.clientProfileID, &r.method, &r.status, &r.length, &r.retryAfterSeconds, &r.errCode, &r.errMessage, &r.errRetryable, &r.requestedAt, &r.expiresAt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND, "verification request not found")
	}
	return r.toProto(), nil
}

func (s *PostgresStore) SaveRegistration(ctx context.Context, record *waappv1.RegistrationRecord) error {
	appErr := errorFromProto(record.GetLastError())
	identity := record.GetIdentity()
	_, err := s.pool.Exec(ctx, `INSERT INTO wa_registrations (registration_id, verification_request_id, wa_account_id, client_profile_id, status, registered_identity_id, service_account_id, service_login_id, last_error_code, last_error_message, last_error_retryable, submitted_at, completed_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (registration_id) DO UPDATE SET status=EXCLUDED.status, registered_identity_id=EXCLUDED.registered_identity_id, service_account_id=EXCLUDED.service_account_id, service_login_id=EXCLUDED.service_login_id, last_error_code=EXCLUDED.last_error_code, last_error_message=EXCLUDED.last_error_message, last_error_retryable=EXCLUDED.last_error_retryable, completed_at=EXCLUDED.completed_at`,
		record.GetRegistrationId(), record.GetVerificationRequestId(), record.GetWaAccountId(), record.GetClientProfileId(), record.GetStatus().String(), identity.GetRegisteredIdentityId(), identity.GetServiceAccountId(), identity.GetServiceLoginId(), errCode(appErr), errMessage(appErr), errRetryable(appErr), timeFromProto(record.GetSubmittedAt()), nullableProtoTime(record.GetCompletedAt()))
	return err
}

func (s *PostgresStore) GetRegistration(ctx context.Context, id string) (*waappv1.RegistrationRecord, error) {
	row := s.pool.QueryRow(ctx, `SELECT registration_id,verification_request_id,wa_account_id,client_profile_id,status,registered_identity_id,service_account_id,service_login_id,last_error_code,last_error_message,last_error_retryable,submitted_at,completed_at FROM wa_registrations WHERE registration_id=$1`, id)
	var r registrationRow
	if err := row.Scan(&r.id, &r.verificationRequestID, &r.waAccountIDValue, &r.clientProfileID, &r.status, &r.identityID, &r.serviceAccountID, &r.serviceLoginID, &r.errCode, &r.errMessage, &r.errRetryable, &r.submittedAt, &r.completedAt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND, "registration not found")
	}
	return r.toProto(), nil
}

func (s *PostgresStore) SaveLoginState(ctx context.Context, state *waappv1.LoginState, stateRef string) error {
	appErr := errorFromProto(state.GetLastError())
	_, err := s.pool.Exec(ctx, `INSERT INTO wa_login_states (login_state_id, registration_id, wa_account_id, client_profile_id, registered_identity_id, service_account_id, service_login_id, status, state_ref, last_error_code, last_error_message, last_error_retryable, registered_at, last_verified_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT (login_state_id) DO UPDATE SET status=EXCLUDED.status, service_account_id=EXCLUDED.service_account_id, service_login_id=EXCLUDED.service_login_id, state_ref=EXCLUDED.state_ref, last_error_code=EXCLUDED.last_error_code, last_error_message=EXCLUDED.last_error_message, last_error_retryable=EXCLUDED.last_error_retryable, last_verified_at=EXCLUDED.last_verified_at, updated_at=EXCLUDED.updated_at`,
		state.GetLoginStateId(), state.GetRegistrationId(), state.GetWaAccountId(), state.GetClientProfileId(), state.GetRegisteredIdentityId(), state.GetServiceAccountId(), state.GetServiceLoginId(), state.GetStatus().String(), stateRef, errCode(appErr), errMessage(appErr), errRetryable(appErr), timeFromProto(state.GetRegisteredAt()), nullableProtoTime(state.GetLastVerifiedAt()), timeFromProto(state.GetAudit().GetCreatedAt()), timeFromProto(state.GetAudit().GetUpdatedAt()))
	return err
}

func (s *PostgresStore) GetLoginState(ctx context.Context, id string) (*waappv1.LoginState, error) {
	return s.getLoginState(ctx, `login_state_id=$1`, id)
}

func (s *PostgresStore) GetActiveLoginState(ctx context.Context, waAccountIDValue string, clientProfileID string) (*waappv1.LoginState, error) {
	return s.getLoginState(ctx, `wa_account_id=$1 AND client_profile_id=$2 AND status=$3`, waAccountIDValue, clientProfileID, waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE.String())
}

func (s *PostgresStore) ListActiveLoginStates(ctx context.Context) ([]LoginStateRecord, error) {
	return s.listLoginStatesByStatus(ctx, waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE)
}

func (s *PostgresStore) ListRevokedLoginStates(ctx context.Context) ([]LoginStateRecord, error) {
	return s.listLoginStatesByStatus(ctx, waappv1.LoginStateStatus_LOGIN_STATE_STATUS_REVOKED)
}

func (s *PostgresStore) listLoginStatesByStatus(ctx context.Context, status waappv1.LoginStateStatus) ([]LoginStateRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT login_state_id,registration_id,wa_account_id,client_profile_id,registered_identity_id,service_account_id,service_login_id,status,last_error_code,last_error_message,last_error_retryable,registered_at,last_verified_at,created_at,updated_at
FROM wa_login_states
WHERE status=$1
ORDER BY updated_at DESC`, status.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []LoginStateRecord{}
	for rows.Next() {
		var r loginStateRow
		if err := rows.Scan(&r.id, &r.registrationID, &r.waAccountIDValue, &r.clientProfileID, &r.registeredIdentityID, &r.serviceAccountID, &r.serviceLoginID, &r.status, &r.errCode, &r.errMessage, &r.errRetryable, &r.registeredAt, &r.lastVerifiedAt, &r.createdAt, &r.updatedAt); err != nil {
			return nil, err
		}
		records = append(records, LoginStateRecord{LoginState: r.toProto()})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *PostgresStore) GetLoginStateByRegistration(ctx context.Context, registrationID string) (*waappv1.LoginState, error) {
	return s.getLoginState(ctx, `registration_id=$1`, registrationID)
}

func (s *PostgresStore) GetLoginStateByRegisteredIdentity(ctx context.Context, registeredIdentityID string) (*waappv1.LoginState, error) {
	return s.getLoginState(ctx, `registered_identity_id=$1`, registeredIdentityID)
}

func (s *PostgresStore) getLoginState(ctx context.Context, where string, args ...any) (*waappv1.LoginState, error) {
	row := s.pool.QueryRow(ctx, `SELECT login_state_id,registration_id,wa_account_id,client_profile_id,registered_identity_id,service_account_id,service_login_id,status,last_error_code,last_error_message,last_error_retryable,registered_at,last_verified_at,created_at,updated_at FROM wa_login_states WHERE `+where, args...)
	var r loginStateRow
	if err := row.Scan(&r.id, &r.registrationID, &r.waAccountIDValue, &r.clientProfileID, &r.registeredIdentityID, &r.serviceAccountID, &r.serviceLoginID, &r.status, &r.errCode, &r.errMessage, &r.errRetryable, &r.registeredAt, &r.lastVerifiedAt, &r.createdAt, &r.updatedAt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND, "login state not found")
	}
	return r.toProto(), nil
}

func (s *PostgresStore) SaveMessageSession(ctx context.Context, session *waappv1.MessageSession) error {
	appErr := errorFromProto(session.GetLastError())
	_, err := s.pool.Exec(ctx, `INSERT INTO wa_message_sessions (message_session_id, wa_account_id, client_profile_id, registered_identity_id, protocol_profile_id, status, last_error_code, last_error_message, last_error_retryable, opened_at, last_seen_at, closed_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (message_session_id) DO UPDATE SET status=EXCLUDED.status, last_error_code=EXCLUDED.last_error_code, last_error_message=EXCLUDED.last_error_message, last_error_retryable=EXCLUDED.last_error_retryable, last_seen_at=EXCLUDED.last_seen_at, closed_at=EXCLUDED.closed_at`,
		session.GetMessageSessionId(), session.GetWaAccountId(), session.GetClientProfileId(), session.GetRegisteredIdentityId(), session.GetProtocolProfileId(), session.GetStatus().String(), errCode(appErr), errMessage(appErr), errRetryable(appErr), timeFromProto(session.GetOpenedAt()), nullableProtoTime(session.GetLastSeenAt()), nullableProtoTime(session.GetClosedAt()))
	return err
}

func (s *PostgresStore) GetMessageSession(ctx context.Context, id string) (*waappv1.MessageSession, error) {
	row := s.pool.QueryRow(ctx, `SELECT message_session_id,wa_account_id,client_profile_id,registered_identity_id,protocol_profile_id,status,last_error_code,last_error_message,last_error_retryable,opened_at,last_seen_at,closed_at FROM wa_message_sessions WHERE message_session_id=$1`, id)
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
	tag, err := s.pool.Exec(ctx, `UPDATE wa_message_sessions
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
		appErr := errorFromProto(msg.GetLastError())
		batch.Queue(`INSERT INTO wa_inbound_messages (message_id, message_session_id, kind, encryption_state, ack_status, direction, source, contact_ref, sender_ref, payload_ref, provider_message_id, provider_timestamp, read_at, delete_status, deleted_at, last_error_code, last_error_message, last_error_retryable, received_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
ON CONFLICT (message_id) DO UPDATE SET encryption_state=EXCLUDED.encryption_state, ack_status=EXCLUDED.ack_status, direction=EXCLUDED.direction, source=EXCLUDED.source, contact_ref=EXCLUDED.contact_ref, sender_ref=EXCLUDED.sender_ref, payload_ref=EXCLUDED.payload_ref, provider_message_id=COALESCE(NULLIF(EXCLUDED.provider_message_id,''), wa_inbound_messages.provider_message_id), provider_timestamp=COALESCE(EXCLUDED.provider_timestamp, wa_inbound_messages.provider_timestamp), read_at=COALESCE(EXCLUDED.read_at, wa_inbound_messages.read_at), delete_status=CASE WHEN wa_inbound_messages.delete_status='MESSAGE_DELETE_STATUS_DELETED_FOR_ME' AND EXCLUDED.delete_status='MESSAGE_DELETE_STATUS_NOT_DELETED' THEN wa_inbound_messages.delete_status ELSE EXCLUDED.delete_status END, deleted_at=COALESCE(EXCLUDED.deleted_at, wa_inbound_messages.deleted_at), last_error_code=EXCLUDED.last_error_code, last_error_message=EXCLUDED.last_error_message, last_error_retryable=EXCLUDED.last_error_retryable`,
			msg.GetMessageId(), msg.GetMessageSessionId(), msg.GetKind().String(), msg.GetEncryptionState().String(), msg.GetAckStatus().String(), inboundMessageDirectionStorageValue(msg), inboundMessageSourceStorageValue(msg), msg.GetContactRef(), msg.GetSenderRef(), msg.GetPayloadRef(), msg.GetProviderMessageId(), nullableProtoTime(msg.GetProviderTimestamp()), nullableProtoTime(msg.GetReadAt()), inboundDeleteStatusStorageValue(msg), nullableProtoTime(msg.GetDeletedAt()), errCode(appErr), errMessage(appErr), errRetryable(appErr), timeFromProto(msg.GetReceivedAt()))
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
	row := s.pool.QueryRow(ctx, `SELECT message_id,message_session_id,kind,encryption_state,ack_status,direction,source,contact_ref,sender_ref,payload_ref,provider_message_id,provider_timestamp,read_at,delete_status,deleted_at,last_error_code,last_error_message,last_error_retryable,received_at FROM wa_inbound_messages WHERE message_id=$1`, id)
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
	rows, err := s.pool.Query(ctx, `SELECT m.message_id,m.message_session_id,m.kind,m.encryption_state,m.ack_status,m.direction,m.source,m.contact_ref,m.sender_ref,m.payload_ref,m.provider_message_id,m.provider_timestamp,m.read_at,m.delete_status,m.deleted_at,m.last_error_code,m.last_error_message,m.last_error_retryable,m.received_at
FROM wa_inbound_messages m
JOIN wa_message_sessions s ON s.message_session_id=m.message_session_id
WHERE s.wa_account_id=$1 AND s.client_profile_id=$2 AND m.encryption_state='MESSAGE_ENCRYPTION_STATE_ENCRYPTED' AND m.direction='ACCOUNT_MESSAGE_DIRECTION_INBOUND' AND COALESCE(m.delete_status,'MESSAGE_DELETE_STATUS_NOT_DELETED')<>'MESSAGE_DELETE_STATUS_DELETED_FOR_ME' AND NOT EXISTS (
  SELECT 1 FROM wa_decrypted_messages d WHERE d.message_id=m.message_id
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
		if err := rows.Scan(&r.id, &r.sessionID, &r.kind, &r.encryptionState, &r.ackStatus, &r.direction, &r.source, &r.contactRef, &r.senderRef, &r.payloadRef, &r.providerMessageID, &r.providerTimestamp, &r.readAt, &r.deleteStatus, &r.deletedAt, &r.errCode, &r.errMessage, &r.errRetryable, &r.receivedAt); err != nil {
			return nil, err
		}
		messages = append(messages, r.toProto())
	}
	return messages, rows.Err()
}

func (s *PostgresStore) SaveDecryptedMessage(ctx context.Context, msg *waappv1.DecryptedMessage) error {
	appErr := errorFromProto(msg.GetLastError())
	_, err := s.pool.Exec(ctx, `INSERT INTO wa_decrypted_messages (decrypted_message_id, message_id, status, plaintext_ref, plaintext_value, plaintext_redacted, plaintext_secret_ref, last_error_code, last_error_message, last_error_retryable, decrypted_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (decrypted_message_id) DO UPDATE SET status=EXCLUDED.status, plaintext_ref=EXCLUDED.plaintext_ref, plaintext_value=EXCLUDED.plaintext_value, plaintext_redacted=EXCLUDED.plaintext_redacted, plaintext_secret_ref=EXCLUDED.plaintext_secret_ref, last_error_code=EXCLUDED.last_error_code, last_error_message=EXCLUDED.last_error_message, last_error_retryable=EXCLUDED.last_error_retryable, decrypted_at=EXCLUDED.decrypted_at`,
		msg.GetDecryptedMessageId(), msg.GetMessageId(), msg.GetStatus().String(), msg.GetPlaintextRef(), msg.GetPlaintextText().GetValue(), msg.GetPlaintextText().GetRedactedValue(), msg.GetPlaintextText().GetSecretRef(), errCode(appErr), errMessage(appErr), errRetryable(appErr), timeFromProto(msg.GetDecryptedAt()))
	return err
}

func (s *PostgresStore) GetDecryptedMessage(ctx context.Context, id string) (*waappv1.DecryptedMessage, error) {
	row := s.pool.QueryRow(ctx, `SELECT decrypted_message_id,message_id,status,plaintext_ref,plaintext_value,plaintext_redacted,plaintext_secret_ref,last_error_code,last_error_message,last_error_retryable,decrypted_at FROM wa_decrypted_messages WHERE decrypted_message_id=$1`, id)
	var r decryptedRow
	if err := row.Scan(&r.id, &r.messageID, &r.status, &r.plaintextRef, &r.plaintext, &r.redacted, &r.secretRef, &r.errCode, &r.errMessage, &r.errRetryable, &r.decryptedAt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "decrypted message not found")
	}
	return r.toProto(), nil
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
			candidate.GetCandidateId(), candidate.GetMessageId(), candidate.GetDecryptedMessageId(), candidate.GetKind().String(), candidate.GetText().GetRedactedValue(), candidate.GetText().GetSecretRef(), candidate.GetConfidence(), timeFromProto(candidate.GetExtractedAt()))
	}
	return s.pool.SendBatch(ctx, batch).Close()
}

func (s *PostgresStore) SaveOTPMessage(ctx context.Context, msg *waappv1.OtpMessage) error {
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
	source := msg.GetSource().String()
	if source == "" || source == "WA_OTP_SOURCE_UNSPECIFIED" {
		source = "WA_OTP_SOURCE_AUTO_EXTRACTION"
	}
	otpID := firstNonEmpty(msg.GetOtpMessageId(), stableOTPMessageID(msg.GetWaAccountId(), msg.GetSourceParty(), otpValue))
	redactedValue := firstNonEmpty(msg.GetOtp().GetRedactedValue(), redacted(otpValue))
	secretRef := firstNonEmpty(msg.GetOtp().GetSecretRef(), "wa-otp:"+stableID(otpID))
	_, err := s.pool.Exec(ctx, `INSERT INTO wa_otp_messages (otp_message_id, wa_account_id, client_profile_id, registered_identity_id, message_id, candidate_id, source, source_party, otp_value, otp_redacted, otp_secret_ref, received_at, expires_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now(),now())
ON CONFLICT (otp_message_id) DO UPDATE SET client_profile_id=EXCLUDED.client_profile_id, registered_identity_id=EXCLUDED.registered_identity_id, message_id=EXCLUDED.message_id, candidate_id=EXCLUDED.candidate_id, source=EXCLUDED.source, source_party=EXCLUDED.source_party, otp_value=EXCLUDED.otp_value, otp_redacted=EXCLUDED.otp_redacted, otp_secret_ref=EXCLUDED.otp_secret_ref, received_at=EXCLUDED.received_at, expires_at=EXCLUDED.expires_at, updated_at=EXCLUDED.updated_at`,
		otpID, msg.GetWaAccountId(), msg.GetClientProfileId(), msg.GetRegisteredIdentityId(), msg.GetMessageId(), msg.GetCandidateId(), source, msg.GetSourceParty(), otpValue, redactedValue, secretRef, timeFromProto(msg.GetReceivedAt()), nullableProtoTime(msg.GetExpiresAt()))
	return err
}

func (s *PostgresStore) ListAccountOTPMessages(ctx context.Context, waAccountIDValue string, cursorValue string, limit int, includeSensitiveValues bool) ([]*waappv1.OtpMessage, string, error) {
	cursor, err := decodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = normalizePageLimit(limit)
	rows, err := s.queryOTPMessagePage(ctx, waAccountIDValue, cursor, keysetLookaheadLimit(limit))
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []*waappv1.OtpMessage{}
	for rows.Next() {
		var r otpMessageRow
		if err := rows.Scan(&r.id, &r.waAccountIDValue, &r.clientProfileID, &r.registeredIdentityID, &r.messageID, &r.candidateID, &r.source, &r.sourceParty, &r.otpValue, &r.otpRedacted, &r.otpSecretRef, &r.receivedAt, &r.expiresAt, &r.createdAt, &r.updatedAt); err != nil {
			return nil, "", err
		}
		items = append(items, r.toProto(includeSensitiveValues))
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	pageItems, nextCursor := newKeysetPage(items, limit, func(msg *waappv1.OtpMessage) keysetCursor {
		return keysetCursorValue(timeFromProto(msg.GetReceivedAt()), msg.GetOtpMessageId())
	})
	return pageItems, nextCursor, nil
}

func (s *PostgresStore) queryOTPMessagePage(ctx context.Context, waAccountIDValue string, cursor keysetCursor, limit int) (pgx.Rows, error) {
	const base = `SELECT o.otp_message_id,o.wa_account_id,o.client_profile_id,o.registered_identity_id,o.message_id,o.candidate_id,o.source,o.source_party,o.otp_value,o.otp_redacted,o.otp_secret_ref,o.received_at,o.expires_at,o.created_at,o.updated_at
FROM wa_otp_messages o
WHERE o.wa_account_id=$1 AND NOT EXISTS (
  SELECT 1 FROM wa_inbound_messages m
  WHERE m.message_id=o.message_id AND COALESCE(m.delete_status,'MESSAGE_DELETE_STATUS_NOT_DELETED')='MESSAGE_DELETE_STATUS_DELETED_FOR_ME'
)`
	if !hasKeysetCursor(cursor) {
		return s.pool.Query(ctx, base+` ORDER BY o.received_at DESC, o.otp_message_id DESC LIMIT $2`, waAccountIDValue, limit)
	}
	return s.pool.Query(ctx, base+` AND (o.received_at, o.otp_message_id) < ($2, $3) ORDER BY o.received_at DESC, o.otp_message_id DESC LIMIT $4`, waAccountIDValue, cursor.UpdatedAt, cursor.ID, limit)
}

func notFound(err error, code waappv1.WaErrorCode, message string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return NewError(code, message, false)
	}
	return err
}

func timeFromProto(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Now().UTC()
	}
	return ts.AsTime().UTC()
}

func nullableProtoTime(ts *timestamppb.Timestamp) any {
	if ts == nil {
		return nil
	}
	return ts.AsTime().UTC()
}

func sqlTime(ns sql.NullTime) *timestamppb.Timestamp {
	if !ns.Valid {
		return nil
	}
	return timestamppb.New(ns.Time.UTC())
}

func audit(createdAt time.Time, updatedAt time.Time) *waappv1.AuditStamp {
	return &waappv1.AuditStamp{CreatedAt: timestamppb.New(createdAt.UTC()), UpdatedAt: timestamppb.New(updatedAt.UTC())}
}

func errCode(err *AppError) string {
	if err == nil {
		return ""
	}
	return err.Code.String()
}

func errMessage(err *AppError) string {
	if err == nil {
		return ""
	}
	return err.Message
}

func errRetryable(err *AppError) bool {
	return err != nil && err.Retryable
}

func protoError(code string, message string, retryable bool) *waappv1.WaError {
	if code == "" {
		return nil
	}
	return &waappv1.WaError{Code: waappv1.WaErrorCode(waappv1.WaErrorCode_value[code]), Message: message, Retryable: retryable}
}
