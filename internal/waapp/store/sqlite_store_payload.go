package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func (s *SQLiteStore) getLoginStateBy(ctx context.Context, where string, args ...any) (*waappv1.LoginState, error) {
	state := &waappv1.LoginState{}
	return state, s.loadPayloadWhere(ctx, "wa_sqlite_login_states", where, args, state, waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND, "login state not found")
}

func (s *SQLiteStore) upsertPayload(ctx context.Context, table string, id string, updatedAt int64, msg proto.Message) error {
	payload, err := sqliteMarshal(msg)
	if err != nil {
		return err
	}
	_, err = s.queries.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id,updated_at,payload) VALUES (?,?,?)
ON CONFLICT(id) DO UPDATE SET updated_at=excluded.updated_at, payload=excluded.payload`, table), id, updatedAt, payload)
	return err
}

func (s *SQLiteStore) loadPayload(ctx context.Context, table string, id string, msg proto.Message, code waappv1.WaErrorCode, message string) error {
	return s.loadPayloadWhere(ctx, table, `id=?`, []any{id}, msg, code, message)
}

func (s *SQLiteStore) loadPayloadWhere(ctx context.Context, table string, where string, args []any, msg proto.Message, code waappv1.WaErrorCode, message string) error {
	var payload string
	err := s.queries.QueryRowContext(ctx, `SELECT payload FROM `+table+` WHERE `+where+` LIMIT 1`, args...).Scan(&payload)
	if err != nil {
		return sqliteNotFound(err, code, message)
	}
	return sqliteUnmarshal([]byte(payload), msg)
}

func sqliteListPayloads[T proto.Message](ctx context.Context, db SQLiteExecutor, factory func() T, query string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		item := factory()
		if err := sqliteUnmarshal([]byte(payload), item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func sqliteMarshal(msg proto.Message) (string, error) {
	data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func sqliteUnmarshal(data []byte, msg proto.Message) error {
	return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(data, msg)
}

func sqliteNotFound(err error, code waappv1.WaErrorCode, message string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return shared.NewError(code, message, false)
	}
	return err
}

func SQLiteTimeValue(value time.Time) int64 {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return value.UTC().UnixNano()
}
