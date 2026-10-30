package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	pgx "github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *PostgresStore) SaveOTPMessage(ctx context.Context, msg *waappv1.OtpMessage) error {
	if msg == nil {
		return nil
	}
	otpValue := strings.TrimSpace(msg.GetOtp().GetValue())
	if otpValue == "" {
		otpValue = strings.TrimSpace(msg.GetOtp().GetRedactedValue())
	}
	if otpValue == "" {
		return nil
	}
	source := msg.GetSource().String()
	if source == "" || source == "WA_OTP_SOURCE_UNSPECIFIED" {
		source = "WA_OTP_SOURCE_AUTO_EXTRACTION"
	}
	otpID := shared.FirstNonEmpty(msg.GetOtpMessageId(), wamodel.StableOTPMessageID(msg.GetWaAccountId(), msg.GetSourceParty(), otpValue))
	redactedValue := shared.FirstNonEmpty(msg.GetOtp().GetRedactedValue(), shared.Redacted(otpValue))
	secretRef := shared.FirstNonEmpty(msg.GetOtp().GetSecretRef(), "wa-otp:"+shared.StableID(otpID))
	_, err := s.db.Exec(ctx, `INSERT INTO wa_otp_messages (otp_message_id, wa_account_id, client_profile_id, registered_identity_id, message_id, candidate_id, source, source_party, otp_value, otp_redacted, otp_secret_ref, received_at, expires_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now(),now())
ON CONFLICT (otp_message_id) DO UPDATE SET client_profile_id=EXCLUDED.client_profile_id, registered_identity_id=EXCLUDED.registered_identity_id, message_id=EXCLUDED.message_id, candidate_id=EXCLUDED.candidate_id, source=EXCLUDED.source, source_party=EXCLUDED.source_party, otp_value=EXCLUDED.otp_value, otp_redacted=EXCLUDED.otp_redacted, otp_secret_ref=EXCLUDED.otp_secret_ref, received_at=EXCLUDED.received_at, expires_at=EXCLUDED.expires_at, updated_at=EXCLUDED.updated_at`,
		otpID, msg.GetWaAccountId(), msg.GetClientProfileId(), msg.GetRegisteredIdentityId(), msg.GetMessageId(), msg.GetCandidateId(), source, msg.GetSourceParty(), otpValue, redactedValue, secretRef, shared.TimeFromProto(msg.GetReceivedAt()), nullableProtoTime(msg.GetExpiresAt()))
	return err
}

func (s *PostgresStore) ListAccountOTPMessages(ctx context.Context, waAccountIDValue string, cursorValue string, limit int, includeSensitiveValues bool) ([]*waappv1.OtpMessage, string, error) {
	cursor, err := shared.DecodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = shared.NormalizePageLimit(limit)
	rows, err := s.queryOTPMessagePage(ctx, waAccountIDValue, cursor, shared.KeysetLookaheadLimit(limit))
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []*waappv1.OtpMessage{}
	for rows.Next() {
		var r otpMessageRow
		if err := rows.Scan(&r.id, &r.waAccountIDValue, &r.clientProfileID, &r.registeredIdentityID, &r.messageID, &r.candidateID, &r.source, &r.sourceParty, &r.otpValue, &r.otpRedacted, &r.otpSecretRef, &r.receivedAt, &r.expiresAt, &r.createdAt, &r.updatedAt); err != nil {
			return nil, "", err
		}
		items = append(items, r.toProto(includeSensitiveValues))
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	pageItems, nextCursor := shared.NewKeysetPage(items, limit, func(msg *waappv1.OtpMessage) shared.KeysetCursor {
		return shared.KeysetCursorValue(shared.TimeFromProto(msg.GetReceivedAt()), msg.GetOtpMessageId())
	})
	return pageItems, nextCursor, nil
}

func (s *PostgresStore) queryOTPMessagePage(ctx context.Context, waAccountIDValue string, cursor shared.KeysetCursor, limit int) (pgx.Rows, error) {
	const base = `SELECT o.otp_message_id,o.wa_account_id,o.client_profile_id,o.registered_identity_id,o.message_id,o.candidate_id,o.source,o.source_party,o.otp_value,o.otp_redacted,o.otp_secret_ref,o.received_at,o.expires_at,o.created_at,o.updated_at
FROM wa_otp_messages o
WHERE o.wa_account_id=$1 AND NOT EXISTS (
  SELECT 1 FROM wa_inbound_messages m
  WHERE m.message_id=o.message_id AND COALESCE(m.delete_status,'MESSAGE_DELETE_STATUS_NOT_DELETED')='MESSAGE_DELETE_STATUS_DELETED_FOR_ME'
)`
	if !shared.HasKeysetCursor(cursor) {
		return s.db.Query(ctx, base+` ORDER BY o.received_at DESC, o.otp_message_id DESC LIMIT $2`, waAccountIDValue, limit)
	}
	return s.db.Query(ctx, base+` AND (o.received_at, o.otp_message_id) < ($2, $3) ORDER BY o.received_at DESC, o.otp_message_id DESC LIMIT $4`, waAccountIDValue, cursor.UpdatedAt, cursor.ID, limit)
}

func notFound(err error, code waappv1.WaErrorCode, message string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return shared.NewError(code, message, false)
	}
	return err
}

func nullableProtoTime(ts *timestamppb.Timestamp) any {
	if ts == nil {
		return nil
	}
	return ts.AsTime().UTC()
}

func sqlTime(ns sql.NullTime) *timestamppb.Timestamp {
	if !ns.Valid {
		return nil
	}
	return timestamppb.New(ns.Time.UTC())
}

func errCode(err *waappv1.WaError) string {
	if err.GetCode() == waappv1.WaErrorCode_WA_ERROR_CODE_UNSPECIFIED {
		return ""
	}
	return err.GetCode().String()
}

func errMessage(err *waappv1.WaError) string {
	if errCode(err) == "" {
		return ""
	}
	return err.GetMessage()
}

func errRetryable(err *waappv1.WaError) bool {
	return errCode(err) != "" && err.GetRetryable()
}

func protoError(code string, message string, retryable bool) *waappv1.WaError {
	if code == "" {
		return nil
	}
	return &waappv1.WaError{Code: waappv1.WaErrorCode(waappv1.WaErrorCode_value[code]), Message: message, Retryable: retryable}
}
