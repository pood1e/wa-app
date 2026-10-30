package store

import (
	"context"
	"errors"
)

var ErrLegacySchema = errors.New("legacy account storage requires offline wa-migrate before startup")

func (s *SQLiteStore) requireCurrentSchema(ctx context.Context) error {
	var count int
	if err := s.queries.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN
 ('wa_sqlite_native_states','wa_sqlite_registrations','wa_sqlite_runtime_state')`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrLegacySchema
	}
	return nil
}

func requireCurrentPostgresSchema(ctx context.Context, db DBTX) error {
	var legacy bool
	if err := db.QueryRow(ctx, `SELECT to_regclass('wa_client_profile_states') IS NOT NULL OR to_regclass('wa_registrations') IS NOT NULL`).Scan(&legacy); err != nil {
		return err
	}
	if legacy {
		return ErrLegacySchema
	}
	return nil
}
