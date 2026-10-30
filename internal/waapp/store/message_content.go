package store

import (
	"context"
	"google.golang.org/protobuf/encoding/protojson"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *SQLiteStore) GetLatestDecryptedMessage(ctx context.Context, messageID string) (*waappv1.DecryptedMessage, error) {
	message := new(waappv1.DecryptedMessage)
	err := s.loadPayloadWhere(ctx, "wa_sqlite_decrypted_messages", "message_id=? ORDER BY decrypted_at DESC,id DESC", []any{messageID}, message, waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "decrypted message not found")
	return message, err
}

func (s *PostgresStore) GetLatestDecryptedMessage(ctx context.Context, messageID string) (*waappv1.DecryptedMessage, error) {
	var id string
	err := s.db.QueryRow(ctx, `SELECT decrypted_message_id FROM wa_decrypted_messages WHERE message_id=$1 ORDER BY decrypted_at DESC,decrypted_message_id DESC LIMIT 1`, messageID).Scan(&id)
	if err != nil {
		return nil, notFound(err, waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "decrypted message not found")
	}
	return s.GetDecryptedMessage(ctx, id)
}

func marshalMessageContent(content *waappv1.MessageContent) ([]byte, error) {
	if content == nil {
		return []byte("{}"), nil
	}
	return (protojson.MarshalOptions{UseProtoNames: true}).Marshal(content)
}

func unmarshalMessageContent(data []byte) (*waappv1.MessageContent, error) {
	if len(data) == 0 {
		return nil, nil
	}
	content := new(waappv1.MessageContent)
	if err := protojson.Unmarshal(data, content); err != nil {
		return nil, err
	}
	return content, nil
}
