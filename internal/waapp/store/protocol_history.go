package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (s *SQLiteStore) ListRetiredProtocolProfiles(ctx context.Context, profileID string) ([]string, error) {
	rows, err := s.queries.QueryContext(ctx, `SELECT previous.client_profile_id
FROM wa_sqlite_login_states previous
JOIN wa_sqlite_client_profiles owner ON owner.wa_account_id=previous.wa_account_id
WHERE owner.id=? AND previous.client_profile_id<>owner.id
  AND previous.status IN ('LOGIN_STATE_STATUS_INVALID','LOGIN_STATE_STATUS_REVOKED')
ORDER BY previous.updated_at DESC, previous.id DESC`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var profiles []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		profiles = append(profiles, id)
	}
	return profiles, rows.Err()
}

func (s *PostgresStore) ListRetiredProtocolProfiles(ctx context.Context, profileID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT previous.client_profile_id
FROM wa_login_states previous
JOIN wa_client_profiles owner ON owner.wa_account_id=previous.wa_account_id
WHERE owner.client_profile_id=$1 AND previous.client_profile_id<>owner.client_profile_id
  AND previous.status IN ('LOGIN_STATE_STATUS_INVALID','LOGIN_STATE_STATUS_REVOKED')
ORDER BY previous.updated_at DESC, previous.login_state_id DESC`, profileID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
