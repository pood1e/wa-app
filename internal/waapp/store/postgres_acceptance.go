package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

func (s *PostgresStore) WithinAccountTransaction(ctx context.Context, requestID, phone string, run func(account.AccountTransaction) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, requestID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,1))`, phone); err != nil {
			return err
		}
		return run(&PostgresStore{db: tx})
	})
}
func (s *PostgresStore) GetAccountAcceptance(ctx context.Context, id string) (*waappv1.AccountAcceptanceRecord, error) {
	var receipt []byte
	if err := s.db.QueryRow(ctx, `SELECT receipt FROM wa_account_acceptances WHERE request_id=$1`, id).Scan(&receipt); err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, "account acceptance not found")
	}
	record := &waappv1.AccountAcceptanceRecord{}
	if err := proto.Unmarshal(receipt, record); err != nil {
		return nil, err
	}
	return record, nil
}
func (s *PostgresStore) SaveAccountAcceptance(ctx context.Context, id string, record *waappv1.AccountAcceptanceRecord) error {
	data, err := proto.Marshal(record)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO wa_account_acceptances(request_id,wa_account_id,receipt) VALUES ($1,$2,$3)`, id, record.GetAccount().GetWaAccountId(), data)
	return err
}
