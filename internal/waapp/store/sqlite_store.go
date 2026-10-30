package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

type SQLiteExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type SQLiteStore struct {
	db      *sql.DB
	queries SQLiteExecutor
}

func NewSQLiteStoreFile(ctx context.Context, path string) (*SQLiteStore, error) {
	store, err := openSQLite(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := store.requireCurrentSchema(ctx); err != nil {
		store.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

func openSQLite(ctx context.Context, path string) (*SQLiteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("sqlite store path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err := file.Chmod(0600); err != nil {
			_ = file.Close()
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "_txlock=immediate&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"}).String()
	if path == ":memory:" {
		dsn = "file::memory:?_txlock=immediate&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteStore{db: db, queries: db}
	if err := store.configure(ctx); err != nil {
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
		if _, err := s.queries.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	_, err := s.queries.ExecContext(ctx, SQLiteStoreSchema)
	return err
}

func (s *SQLiteStore) SaveAppArtifact(ctx context.Context, artifact *waappv1.AppArtifact) error {
	return s.upsertPayload(ctx, "wa_sqlite_artifacts", artifact.GetArtifactId(), SQLiteTimeValue(shared.TimeFromProto(artifact.GetObservedAt())), artifact)
}

func (s *SQLiteStore) GetAppArtifact(ctx context.Context, artifactID string) (*waappv1.AppArtifact, error) {
	artifact := &waappv1.AppArtifact{}
	return artifact, s.loadPayload(ctx, "wa_sqlite_artifacts", artifactID, artifact, waappv1.WaErrorCode_WA_ERROR_CODE_PROTOCOL_PROFILE_NOT_FOUND, "app artifact not found")
}

func (s *SQLiteStore) SaveProtocolProfile(ctx context.Context, profile *waappv1.ProtocolProfile) error {
	return s.upsertPayload(ctx, "wa_sqlite_protocol_profiles", profile.GetProtocolProfileId(), SQLiteTimeValue(shared.TimeFromProto(profile.GetAudit().GetUpdatedAt())), profile)
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
	_, err = s.queries.ExecContext(ctx, `INSERT INTO wa_sqlite_accounts (id,e164,updated_at,payload) VALUES (?,?,?,?)
ON CONFLICT(id) DO UPDATE SET e164=excluded.e164, updated_at=excluded.updated_at, payload=excluded.payload`, wamodel.WAAccountID(account), account.GetPhone().GetE164Number(), SQLiteTimeValue(wamodel.WAAccountUpdatedAt(account)), payload)
	return err
}

func (s *SQLiteStore) GetWAAccount(ctx context.Context, waAccountIDValue string) (*waappv1.WAAccount, error) {
	return s.getWAAccountBy(ctx, `id=?`, waAccountIDValue)
}

func (s *SQLiteStore) FindWAAccountByPhone(ctx context.Context, e164 string) (*waappv1.WAAccount, error) {
	return s.getWAAccountBy(ctx, `e164=?`, e164)
}

func (s *SQLiteStore) ListWAAccounts(ctx context.Context, cursorValue string, limit int) ([]*waappv1.WAAccount, string, error) {
	cursor, err := shared.DecodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = shared.NormalizePageLimit(limit)
	lookahead := shared.KeysetLookaheadLimit(limit)
	query := `SELECT payload FROM wa_sqlite_accounts`
	args := []any{}
	if shared.HasKeysetCursor(cursor) {
		query += ` WHERE (updated_at < ? OR (updated_at = ? AND id < ?))`
		value := SQLiteTimeValue(cursor.UpdatedAt)
		args = append(args, value, value, cursor.ID)
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, lookahead)
	items, err := sqliteListPayloads(ctx, s.queries, func() *waappv1.WAAccount { return &waappv1.WAAccount{} }, query, args...)
	if err != nil {
		return nil, "", err
	}
	items, nextCursor := shared.NewKeysetPage(items, limit, func(account *waappv1.WAAccount) shared.KeysetCursor {
		return shared.KeysetCursorValue(wamodel.WAAccountUpdatedAt(account), wamodel.WAAccountID(account))
	})
	return items, nextCursor, nil
}

func (s *SQLiteStore) DeleteWAAccount(ctx context.Context, id string) error {
	if s.db == nil {
		return s.deleteWAAccount(ctx, id)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	scoped := &SQLiteStore{queries: tx}
	if err := scoped.deleteWAAccount(ctx, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *SQLiteStore) deleteWAAccount(ctx context.Context, id string) error {
	if _, err := s.GetWAAccount(ctx, id); err != nil {
		return err
	}
	for _, statement := range sqliteDeleteWAAccountStatements {
		if _, err := s.queries.ExecContext(ctx, statement, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) SaveClientProfile(ctx context.Context, profile *waappv1.ClientProfile) error {
	payload, err := sqliteMarshal(profile)
	if err != nil {
		return err
	}
	_, err = s.queries.ExecContext(ctx, `INSERT INTO wa_sqlite_client_profiles (id,wa_account_id,protocol_profile_id,status,updated_at,payload) VALUES (?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET wa_account_id=excluded.wa_account_id, protocol_profile_id=excluded.protocol_profile_id, status=excluded.status, updated_at=excluded.updated_at, payload=excluded.payload`, profile.GetClientProfileId(), profile.GetWaAccountId(), profile.GetProtocolProfileId(), profile.GetStatus().String(), SQLiteTimeValue(shared.TimeFromProto(profile.GetAudit().GetUpdatedAt())), payload)
	return err
}

func (s *SQLiteStore) GetClientProfile(ctx context.Context, id string) (*waappv1.ClientProfile, error) {
	profile := &waappv1.ClientProfile{}
	return profile, s.loadPayload(ctx, "wa_sqlite_client_profiles", id, profile, waappv1.WaErrorCode_WA_ERROR_CODE_PROFILE_NOT_FOUND, "client profile not found")
}

func (s *SQLiteStore) ListClientProfiles(ctx context.Context, waAccountIDValue string, cursorValue string, limit int) ([]*waappv1.ClientProfile, string, error) {
	cursor, err := shared.DecodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = shared.NormalizePageLimit(limit)
	lookahead := shared.KeysetLookaheadLimit(limit)
	query := `SELECT payload FROM wa_sqlite_client_profiles WHERE wa_account_id=?`
	args := []any{waAccountIDValue}
	if shared.HasKeysetCursor(cursor) {
		value := SQLiteTimeValue(cursor.UpdatedAt)
		query += ` AND (updated_at < ? OR (updated_at = ? AND id < ?))`
		args = append(args, value, value, cursor.ID)
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, lookahead)
	items, err := sqliteListPayloads(ctx, s.queries, func() *waappv1.ClientProfile { return &waappv1.ClientProfile{} }, query, args...)
	if err != nil {
		return nil, "", err
	}
	items, nextCursor := shared.NewKeysetPage(items, limit, func(profile *waappv1.ClientProfile) shared.KeysetCursor {
		return shared.KeysetCursorValue(shared.TimeFromProto(profile.GetAudit().GetUpdatedAt()), profile.GetClientProfileId())
	})
	return items, nextCursor, nil
}

func (s *SQLiteStore) SaveLoginState(ctx context.Context, state *waappv1.LoginState) error {
	payload, err := sqliteMarshal(state)
	if err != nil {
		return err
	}
	result, err := s.queries.ExecContext(ctx, `INSERT INTO wa_sqlite_login_states (id,registration_id,wa_account_id,client_profile_id,registered_identity_id,status,updated_at,payload) VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET registration_id=excluded.registration_id, wa_account_id=excluded.wa_account_id, client_profile_id=excluded.client_profile_id, registered_identity_id=excluded.registered_identity_id, status=excluded.status, updated_at=excluded.updated_at, payload=excluded.payload
WHERE (wa_sqlite_login_states.status<>'LOGIN_STATE_STATUS_REVOKED' OR excluded.status='LOGIN_STATE_STATUS_REVOKED')
  AND (wa_sqlite_login_states.status<>'LOGIN_STATE_STATUS_INVALID' OR excluded.status IN ('LOGIN_STATE_STATUS_INVALID','LOGIN_STATE_STATUS_REVOKED'))`, state.GetLoginStateId(), state.GetRegistrationId(), state.GetWaAccountId(), state.GetClientProfileId(), state.GetRegisteredIdentityId(), state.GetStatus().String(), SQLiteTimeValue(shared.TimeFromProto(state.GetAudit().GetUpdatedAt())), payload)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "login identity is no longer active", false)
	}
	return nil
}

func (s *SQLiteStore) GetLoginState(ctx context.Context, id string) (*waappv1.LoginState, error) {
	return s.getLoginStateBy(ctx, `id=?`, id)
}

func (s *SQLiteStore) GetActiveLoginState(ctx context.Context, waAccountIDValue string, clientProfileID string) (*waappv1.LoginState, error) {
	return s.getLoginStateBy(ctx, `wa_account_id=? AND (?='' OR client_profile_id=?) AND status=?`, waAccountIDValue, clientProfileID, clientProfileID, waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE.String())
}

func (s *SQLiteStore) ListActiveLoginStates(ctx context.Context) ([]*waappv1.LoginState, error) {
	return s.listLoginStatesByStatus(ctx, waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE)
}

func (s *SQLiteStore) ListStoppedLoginStates(ctx context.Context) ([]*waappv1.LoginState, error) {
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
