package account

import (
	"context"
	"log"
	"strings"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *serverCore) publishOTPCandidates(ctx context.Context, msg *waappv1.InboundMessage, session *waappv1.MessageSession, candidates []*waappv1.ExtractedCandidate, source waappv1.WaOtpSource) {
	if s == nil || len(candidates) == 0 {
		return
	}
	if source == waappv1.WaOtpSource_WA_OTP_SOURCE_UNSPECIFIED {
		source = waappv1.WaOtpSource_WA_OTP_SOURCE_AUTO_EXTRACTION
	}
	sourceParty := strings.TrimSpace(msg.GetSenderRef())
	for _, candidate := range candidates {
		if candidate.GetKind() != waappv1.CandidateKind_CANDIDATE_KIND_OTP {
			continue
		}
		otp := strings.TrimSpace(candidate.GetText().GetValue())
		if otp == "" {
			continue
		}
		receivedAtTS := firstTimestamp(candidate.GetExtractedAt(), msg.GetReceivedAt())
		otpMessage := &waappv1.OtpMessage{
			OtpMessageId:         wamodel.StableOTPMessageID(session.GetWaAccountId(), sourceParty, otp),
			WaAccountId:          session.GetWaAccountId(),
			ClientProfileId:      session.GetClientProfileId(),
			RegisteredIdentityId: session.GetRegisteredIdentityId(),
			MessageId:            msg.GetMessageId(),
			CandidateId:          candidate.GetCandidateId(),
			Source:               source,
			SourceParty:          sourceParty,
			Otp:                  &waappv1.SensitiveText{Value: otp, RedactedValue: shared.Redacted(otp), SecretRef: "candidate:" + candidate.GetCandidateId()},
			ReceivedAt:           receivedAtTS,
		}
		if err := s.store.SaveOTPMessage(ctx, otpMessage); err != nil && ctx.Err() == nil {
			log.Printf("save WA OTP history failed: %v", shared.SanitizeLogError(err))
		}
	}
}

func firstTimestamp(values ...*timestamppb.Timestamp) *timestamppb.Timestamp {
	for _, value := range values {
		if value != nil && value.IsValid() {
			return value
		}
	}
	return nil
}
