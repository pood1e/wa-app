package store

import (
	"context"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

func (s *SQLiteStore) WithinAccountTransaction(ctx context.Context, requestID, phone string, run func(account.AccountTransaction) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := run(&SQLiteStore{queries: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) GetAccountAcceptance(ctx context.Context, id string) (*waappv1.AccountAcceptanceRecord, error) {
	var receipt []byte
	if err := s.queries.QueryRowContext(ctx, `SELECT receipt FROM wa_sqlite_account_acceptances WHERE request_id=?`, id).Scan(&receipt); err != nil {
		return nil, sqliteNotFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, "account acceptance not found")
	}
	record := &waappv1.AccountAcceptanceRecord{}
	if err := proto.Unmarshal(receipt, record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *SQLiteStore) SaveAccountAcceptance(ctx context.Context, id string, record *waappv1.AccountAcceptanceRecord) error {
	receipt, err := proto.Marshal(record)
	if err != nil {
		return err
	}
	_, err = s.queries.ExecContext(ctx, `INSERT INTO wa_sqlite_account_acceptances(request_id,receipt) VALUES (?,?)`, id, receipt)
	return err
}
