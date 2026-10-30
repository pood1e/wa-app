package store

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

func (s *SQLiteStore) SaveProtocolState(ctx context.Context, id string, credentials *protocolv1.AccountCredentials, recovery *protocolv1.AccountRecoveryState) error {
	if credentials == nil || recovery == nil {
		return fmt.Errorf("account protocol material is required")
	}
	keys, err := proto.Marshal(credentials)
	if err != nil {
		return err
	}
	snapshot, err := proto.Marshal(recovery)
	if err != nil {
		return err
	}
	save := func(db SQLiteExecutor) error {
		if _, err := db.ExecContext(ctx, `INSERT INTO wa_sqlite_account_credentials(client_profile_id,credentials) VALUES (?,?)
ON CONFLICT(client_profile_id) DO UPDATE SET credentials=excluded.credentials`, id, keys); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, `INSERT INTO wa_sqlite_protocol_states(client_profile_id,recovery) VALUES (?,?)
ON CONFLICT(client_profile_id) DO UPDATE SET recovery=excluded.recovery`, id, snapshot)
		return err
	}
	if s.db == nil {
		return save(s.queries)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := save(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) GetProtocolState(ctx context.Context, id string) (*protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState, error) {
	var keys, snapshot []byte
	if err := s.queries.QueryRowContext(ctx, `SELECT c.credentials,s.recovery FROM wa_sqlite_account_credentials c
JOIN wa_sqlite_protocol_states s USING(client_profile_id) WHERE c.client_profile_id=?`, id).Scan(&keys, &snapshot); err != nil {
		return nil, nil, err
	}
	credentials := &protocolv1.AccountCredentials{}
	recovery := &protocolv1.AccountRecoveryState{}
	if err := proto.Unmarshal(keys, credentials); err != nil {
		return nil, nil, fmt.Errorf("decode account credentials")
	}
	if err := proto.Unmarshal(snapshot, recovery); err != nil {
		return nil, nil, fmt.Errorf("decode account recovery state")
	}
	return credentials, recovery, nil
}
