package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/proto"
)

func decodeAccountProxy(data []byte, err error) (*waappv1.AccountProxy, error) {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value := new(waappv1.AccountProxy)
	if err := proto.Unmarshal(data, value); err != nil {
		return nil, errors.New("account proxy configuration is invalid")
	}
	return value, nil
}

func (s *SQLiteStore) GetAccountProxy(ctx context.Context, id string) (*waappv1.AccountProxy, error) {
	var data []byte
	err := s.queries.QueryRowContext(ctx, `SELECT configuration FROM wa_sqlite_account_proxies WHERE wa_account_id=?`, id).Scan(&data)
	return decodeAccountProxy(data, err)
}

func (s *SQLiteStore) GetProfileProxy(ctx context.Context, id string) (*waappv1.AccountProxy, error) {
	var data []byte
	err := s.queries.QueryRowContext(ctx, `SELECT p.configuration FROM wa_sqlite_account_proxies p JOIN wa_sqlite_client_profiles c ON c.wa_account_id=p.wa_account_id WHERE c.id=?`, id).Scan(&data)
	return decodeAccountProxy(data, err)
}

func (s *SQLiteStore) SaveAccountProxy(ctx context.Context, id string, value *waappv1.AccountProxy) error {
	if value == nil {
		_, err := s.queries.ExecContext(ctx, `DELETE FROM wa_sqlite_account_proxies WHERE wa_account_id=?`, id)
		return err
	}
	data, err := proto.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.queries.ExecContext(ctx, `INSERT INTO wa_sqlite_account_proxies(wa_account_id,configuration) VALUES(?,?) ON CONFLICT(wa_account_id) DO UPDATE SET configuration=excluded.configuration`, id, data)
	return err
}

func (s *PostgresStore) GetAccountProxy(ctx context.Context, id string) (*waappv1.AccountProxy, error) {
	var data []byte
	err := s.db.QueryRow(ctx, `SELECT configuration FROM wa_account_proxies WHERE wa_account_id=$1`, id).Scan(&data)
	return decodeAccountProxy(data, err)
}

func (s *PostgresStore) GetProfileProxy(ctx context.Context, id string) (*waappv1.AccountProxy, error) {
	var data []byte
	err := s.db.QueryRow(ctx, `SELECT p.configuration FROM wa_account_proxies p JOIN wa_client_profiles c USING(wa_account_id) WHERE c.client_profile_id=$1`, id).Scan(&data)
	return decodeAccountProxy(data, err)
}

func (s *PostgresStore) SaveAccountProxy(ctx context.Context, id string, value *waappv1.AccountProxy) error {
	if value == nil {
		_, err := s.db.Exec(ctx, `DELETE FROM wa_account_proxies WHERE wa_account_id=$1`, id)
		return err
	}
	data, err := proto.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO wa_account_proxies(wa_account_id,configuration) VALUES($1,$2) ON CONFLICT(wa_account_id) DO UPDATE SET configuration=excluded.configuration`, id, data)
	return err
}
