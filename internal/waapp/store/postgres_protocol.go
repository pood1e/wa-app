package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

func (s *PostgresStore) SaveProtocolState(ctx context.Context, id string, credentials *protocolv1.AccountCredentials, recovery *protocolv1.AccountRecoveryState) error {
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
	save := func(tx DBTX) error {
		if _, err := tx.Exec(ctx, `INSERT INTO wa_account_credentials (client_profile_id,credentials) VALUES ($1,$2)
ON CONFLICT (client_profile_id) DO UPDATE SET credentials=EXCLUDED.credentials,updated_at=now()`, id, keys); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO wa_protocol_states (client_profile_id,recovery) VALUES ($1,$2)
ON CONFLICT (client_profile_id) DO UPDATE SET recovery=EXCLUDED.recovery,updated_at=now()`, id, snapshot)
		return err
	}
	if s.pool == nil {
		return save(s.db)
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return save(tx) })
}

func (s *PostgresStore) GetProtocolState(ctx context.Context, id string) (*protocolv1.AccountCredentials, *protocolv1.AccountRecoveryState, error) {
	var keys, snapshot []byte
	err := s.db.QueryRow(ctx, `SELECT c.credentials,s.recovery FROM wa_account_credentials c
JOIN wa_protocol_states s USING(client_profile_id) WHERE c.client_profile_id=$1`, id).Scan(&keys, &snapshot)
	if err != nil {
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
