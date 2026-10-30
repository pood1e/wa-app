package store

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

//go:embed migrations/001_init.sql
var postgresStoreSchema string

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresStore struct {
	pool *pgxpool.Pool
	db   DBTX
}

func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("WA_APP_PG_DSN is required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid WA_APP_PG_DSN")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	store := &PostgresStore{pool: pool, db: pool}
	if err := requireCurrentPostgresSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
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
		"wa_app_artifacts", "wa_protocol_profiles", "wa_accounts", "wa_account_proxies", "wa_client_profiles",
		"wa_account_credentials", "wa_protocol_states", "wa_account_acceptances", "wa_login_states", "wa_message_sessions", "wa_inbound_messages",
		"wa_decrypted_messages", "wa_extracted_candidates", "wa_otp_messages", "wa_contacts",
	} {
		var exists bool
		if err := s.db.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("required wa-app table %s is missing; apply migrations first", table)
		}
	}
	return nil
}

func (s *PostgresStore) SaveAppArtifact(ctx context.Context, artifact *waappv1.AppArtifact) error {
	_, err := s.db.Exec(ctx, `INSERT INTO wa_app_artifacts (artifact_id, label, version_label, sha256, observed_at)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (artifact_id) DO UPDATE SET label=EXCLUDED.label, version_label=EXCLUDED.version_label, sha256=EXCLUDED.sha256, observed_at=EXCLUDED.observed_at`,
		artifact.GetArtifactId(), artifact.GetLabel(), artifact.GetVersionLabel(), artifact.GetSha256(), shared.TimeFromProto(artifact.GetObservedAt()))
	return err
}

func (s *PostgresStore) GetAppArtifact(ctx context.Context, artifactID string) (*waappv1.AppArtifact, error) {
	row := s.db.QueryRow(ctx, `SELECT artifact_id,label,version_label,sha256,observed_at FROM wa_app_artifacts WHERE artifact_id=$1`, artifactID)
	var id, label, version, hash string
	var observed time.Time
	if err := row.Scan(&id, &label, &version, &hash, &observed); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_PROTOCOL_PROFILE_NOT_FOUND, "app artifact not found")
	}
	return &waappv1.AppArtifact{ArtifactId: id, Label: label, VersionLabel: version, Sha256: hash, ObservedAt: timestamppb.New(observed)}, nil
}

func (s *PostgresStore) SaveProtocolProfile(ctx context.Context, profile *waappv1.ProtocolProfile) error {
	_, err := s.db.Exec(ctx, `INSERT INTO wa_protocol_profiles (protocol_profile_id, app_artifact_id, display_name, app_version, status, capabilities, registration_flows, message_transports, discovered_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (protocol_profile_id) DO UPDATE SET display_name=EXCLUDED.display_name, app_version=EXCLUDED.app_version, status=EXCLUDED.status, capabilities=EXCLUDED.capabilities, registration_flows=EXCLUDED.registration_flows, message_transports=EXCLUDED.message_transports, updated_at=EXCLUDED.updated_at`,
		profile.GetProtocolProfileId(), profile.GetAppArtifactId(), profile.GetDisplayName(), profile.GetAppVersion(), profile.GetStatus().String(), protocolCapabilities(profile.GetCapabilities()), registrationFlows(profile.GetRegistrationFlows()), messageTransports(profile.GetMessageTransports()), shared.TimeFromProto(profile.GetDiscoveredAt()), shared.TimeFromProto(profile.GetAudit().GetCreatedAt()), shared.TimeFromProto(profile.GetAudit().GetUpdatedAt()))
	return err
}

func (s *PostgresStore) GetProtocolProfile(ctx context.Context, id string) (*waappv1.ProtocolProfile, error) {
	row := s.db.QueryRow(ctx, `SELECT protocol_profile_id,app_artifact_id,display_name,app_version,status,capabilities,registration_flows,message_transports,discovered_at,created_at,updated_at FROM wa_protocol_profiles WHERE protocol_profile_id=$1`, id)
	var r protocolProfileRow
	if err := row.Scan(&r.id, &r.artifactID, &r.displayName, &r.appVersion, &r.status, &r.capabilities, &r.registrationFlows, &r.messageTransports, &r.discoveredAt, &r.createdAt, &r.updatedAt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_PROTOCOL_PROFILE_NOT_FOUND, "protocol profile not found")
	}
	return r.toProto(), nil
}

func (s *PostgresStore) SaveWAAccount(ctx context.Context, account *waappv1.WAAccount) error {
	createdAt := wamodel.WAAccountCreatedAt(account)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := wamodel.WAAccountUpdatedAt(account)
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	_, err := s.db.Exec(ctx, `INSERT INTO wa_accounts (wa_account_id, display_name, e164_number, country_calling_code, national_number, country_iso2, status, two_factor_auth_configured, two_factor_email_configured, two_factor_email_address, two_factor_email_verified, two_factor_email_confirmed, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (e164_number) DO UPDATE SET display_name=COALESCE(NULLIF(EXCLUDED.display_name,''), wa_accounts.display_name), country_calling_code=EXCLUDED.country_calling_code, national_number=EXCLUDED.national_number, country_iso2=EXCLUDED.country_iso2, status=EXCLUDED.status, two_factor_auth_configured=COALESCE(EXCLUDED.two_factor_auth_configured, wa_accounts.two_factor_auth_configured), two_factor_email_configured=COALESCE(EXCLUDED.two_factor_email_configured, wa_accounts.two_factor_email_configured), two_factor_email_address=COALESCE(EXCLUDED.two_factor_email_address, wa_accounts.two_factor_email_address), two_factor_email_verified=COALESCE(EXCLUDED.two_factor_email_verified, wa_accounts.two_factor_email_verified), two_factor_email_confirmed=COALESCE(EXCLUDED.two_factor_email_confirmed, wa_accounts.two_factor_email_confirmed), updated_at=EXCLUDED.updated_at`,
		wamodel.WAAccountID(account), account.GetDisplayName(), account.GetPhone().GetE164Number(), account.GetPhone().GetCountryCallingCode(), account.GetPhone().GetNationalNumber(), account.GetPhone().GetCountryIso2(), wamodel.WAAccountStatusStorageValue(account), nullableTwoFactorConfigured(account.GetTwoFactorAuth()), nullableTwoFactorEmailConfigured(account.GetTwoFactorAuth()), nullableTwoFactorEmailAddress(account.GetTwoFactorAuth()), nullableTwoFactorEmailVerified(account.GetTwoFactorAuth()), nullableTwoFactorEmailConfirmed(account.GetTwoFactorAuth()), createdAt, updatedAt)
	return err
}

func (s *PostgresStore) GetWAAccount(ctx context.Context, waAccountIDValue string) (*waappv1.WAAccount, error) {
	return s.getWAAccount(ctx, `wa_account_id=$1`, waAccountIDValue)
}

func (s *PostgresStore) FindWAAccountByPhone(ctx context.Context, e164 string) (*waappv1.WAAccount, error) {
	return s.getWAAccount(ctx, `e164_number=$1`, e164)
}

func (s *PostgresStore) ListWAAccounts(ctx context.Context, cursorValue string, limit int) ([]*waappv1.WAAccount, string, error) {
	cursor, err := shared.DecodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = shared.NormalizePageLimit(limit)
	rows, err := s.queryWAAccountPage(ctx, cursor, shared.KeysetLookaheadLimit(limit))
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
	items, nextCursor := shared.NewKeysetPage(accounts, limit, func(account *waappv1.WAAccount) shared.KeysetCursor {
		return shared.KeysetCursorValue(wamodel.WAAccountUpdatedAt(account), wamodel.WAAccountID(account))
	})
	return items, nextCursor, nil
}

func (s *PostgresStore) queryWAAccountPage(ctx context.Context, cursor shared.KeysetCursor, limit int) (pgx.Rows, error) {
	const base = `SELECT wa_account_id,display_name,e164_number,country_calling_code,national_number,country_iso2,status,two_factor_auth_configured,two_factor_email_configured,two_factor_email_address,two_factor_email_verified,two_factor_email_confirmed,created_at,updated_at FROM wa_accounts`
	if !shared.HasKeysetCursor(cursor) {
		return s.db.Query(ctx, base+` ORDER BY updated_at DESC, wa_account_id DESC LIMIT $1`, limit)
	}
	return s.db.Query(ctx, base+` WHERE (updated_at, wa_account_id) < ($1, $2) ORDER BY updated_at DESC, wa_account_id DESC LIMIT $3`, cursor.UpdatedAt, cursor.ID, limit)
}

func (s *PostgresStore) getWAAccount(ctx context.Context, where string, args ...any) (*waappv1.WAAccount, error) {
	row := s.db.QueryRow(ctx, `SELECT wa_account_id,display_name,e164_number,country_calling_code,national_number,country_iso2,status,two_factor_auth_configured,two_factor_email_configured,two_factor_email_address,two_factor_email_verified,two_factor_email_confirmed,created_at,updated_at FROM wa_accounts WHERE `+where, args...)
	var r waAccountRow
	if err := row.Scan(&r.id, &r.displayName, &r.e164, &r.cc, &r.national, &r.iso2, &r.status, &r.twoFactorConfigured, &r.twoFactorEmailConfigured, &r.twoFactorEmailAddress, &r.twoFactorEmailVerified, &r.twoFactorEmailConfirmed, &r.createdAt, &r.updatedAt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, "WA account not found")
	}
	return r.toProto(), nil
}

func (s *PostgresStore) SaveClientProfile(ctx context.Context, profile *waappv1.ClientProfile) error {
	appErr := profile.GetLastError()
	_, err := s.db.Exec(ctx, `INSERT INTO wa_client_profiles (client_profile_id, wa_account_id, protocol_profile_id, status, registration_key_state, messaging_key_state, last_error_code, last_error_message, last_error_retryable, last_used_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (client_profile_id) DO UPDATE SET status=EXCLUDED.status, registration_key_state=EXCLUDED.registration_key_state, messaging_key_state=EXCLUDED.messaging_key_state, last_error_code=EXCLUDED.last_error_code, last_error_message=EXCLUDED.last_error_message, last_error_retryable=EXCLUDED.last_error_retryable, last_used_at=EXCLUDED.last_used_at, updated_at=EXCLUDED.updated_at`,
		profile.GetClientProfileId(), profile.GetWaAccountId(), profile.GetProtocolProfileId(), profile.GetStatus().String(), profile.GetRegistrationKeyState().String(), profile.GetMessagingKeyState().String(), errCode(appErr), errMessage(appErr), errRetryable(appErr), nullableProtoTime(profile.GetLastUsedAt()), shared.TimeFromProto(profile.GetAudit().GetCreatedAt()), shared.TimeFromProto(profile.GetAudit().GetUpdatedAt()))
	return err
}

func (s *PostgresStore) GetClientProfile(ctx context.Context, id string) (*waappv1.ClientProfile, error) {
	row := s.db.QueryRow(ctx, `SELECT client_profile_id,wa_account_id,protocol_profile_id,status,registration_key_state,messaging_key_state,last_error_code,last_error_message,last_error_retryable,last_used_at,created_at,updated_at FROM wa_client_profiles WHERE client_profile_id=$1`, id)
	var r clientProfileRow
	if err := row.Scan(&r.id, &r.waAccountIDValue, &r.protocolProfileID, &r.status, &r.registrationKeyState, &r.messagingKeyState, &r.errCode, &r.errMessage, &r.errRetryable, &r.lastUsedAt, &r.createdAt, &r.updatedAt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_PROFILE_NOT_FOUND, "client profile not found")
	}
	return r.toProto(), nil
}
