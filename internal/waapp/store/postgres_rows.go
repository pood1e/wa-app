package store

import (
	"database/sql"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func protocolCapabilities(values []waappv1.ProtocolCapability) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != waappv1.ProtocolCapability_PROTOCOL_CAPABILITY_UNSPECIFIED {
			out = append(out, value.String())
		}
	}
	return out
}

func registrationFlows(values []waappv1.RegistrationFlowKind) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != waappv1.RegistrationFlowKind_REGISTRATION_FLOW_KIND_UNSPECIFIED {
			out = append(out, value.String())
		}
	}
	return out
}

func messageTransports(values []waappv1.MessageTransportKind) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != waappv1.MessageTransportKind_MESSAGE_TRANSPORT_KIND_UNSPECIFIED {
			out = append(out, value.String())
		}
	}
	return out
}

func deliveryMethods(values []waappv1.VerificationDeliveryMethod) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
			out = append(out, value.String())
		}
	}
	return out
}

func parseProtocolCapabilities(values []string) []waappv1.ProtocolCapability {
	out := make([]waappv1.ProtocolCapability, 0, len(values))
	for _, value := range values {
		out = append(out, waappv1.ProtocolCapability(waappv1.ProtocolCapability_value[value]))
	}
	return out
}

func parseRegistrationFlows(values []string) []waappv1.RegistrationFlowKind {
	out := make([]waappv1.RegistrationFlowKind, 0, len(values))
	for _, value := range values {
		out = append(out, waappv1.RegistrationFlowKind(waappv1.RegistrationFlowKind_value[value]))
	}
	return out
}

func parseMessageTransports(values []string) []waappv1.MessageTransportKind {
	out := make([]waappv1.MessageTransportKind, 0, len(values))
	for _, value := range values {
		out = append(out, waappv1.MessageTransportKind(waappv1.MessageTransportKind_value[value]))
	}
	return out
}

type protocolProfileRow struct {
	id                string
	artifactID        string
	displayName       string
	appVersion        string
	status            string
	capabilities      []string
	registrationFlows []string
	messageTransports []string
	discoveredAt      time.Time
	createdAt         time.Time
	updatedAt         time.Time
}

func (r protocolProfileRow) toProto() *waappv1.ProtocolProfile {
	return &waappv1.ProtocolProfile{
		ProtocolProfileId: r.id,
		AppArtifactId:     r.artifactID,
		DisplayName:       r.displayName,
		AppVersion:        r.appVersion,
		Status:            waappv1.ProtocolProfileStatus(waappv1.ProtocolProfileStatus_value[r.status]),
		Capabilities:      parseProtocolCapabilities(r.capabilities),
		RegistrationFlows: parseRegistrationFlows(r.registrationFlows),
		MessageTransports: parseMessageTransports(r.messageTransports),
		DiscoveredAt:      timestamppb.New(r.discoveredAt.UTC()),
		Audit:             shared.NewAuditStamp(r.createdAt, r.updatedAt),
	}
}

type waAccountRow struct {
	id                       string
	displayName              string
	e164                     string
	cc                       string
	national                 string
	iso2                     string
	status                   string
	twoFactorConfigured      sql.NullBool
	twoFactorEmailConfigured sql.NullBool
	twoFactorEmailAddress    sql.NullString
	twoFactorEmailVerified   sql.NullBool
	twoFactorEmailConfirmed  sql.NullBool
	createdAt                time.Time
	updatedAt                time.Time
}

func (r waAccountRow) toProto() *waappv1.WAAccount {
	account := wamodel.NewWAAccount(r.id, r.displayName, &waappv1.PhoneTarget{
		E164Number:         r.e164,
		CountryCallingCode: r.cc,
		NationalNumber:     r.national,
		CountryIso2:        r.iso2,
	}, waappv1.WAAccountStatus(waappv1.WAAccountStatus_value[r.status]), shared.NewAuditStamp(r.createdAt, r.updatedAt))
	if r.twoFactorConfigured.Valid || r.twoFactorEmailConfigured.Valid || r.twoFactorEmailAddress.Valid || r.twoFactorEmailVerified.Valid || r.twoFactorEmailConfirmed.Valid {
		account.TwoFactorAuth = &waappv1.TwoFactorAuthStatus{
			Configured:      r.twoFactorConfigured.Bool,
			EmailConfigured: r.twoFactorEmailConfigured.Bool,
			EmailAddress:    r.twoFactorEmailAddress.String,
			EmailVerified:   r.twoFactorEmailVerified.Bool,
			EmailConfirmed:  r.twoFactorEmailConfirmed.Bool,
		}
	}
	return account
}

func nullableTwoFactorConfigured(status *waappv1.TwoFactorAuthStatus) sql.NullBool {
	if status == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: status.GetConfigured(), Valid: true}
}

func nullableTwoFactorEmailConfigured(status *waappv1.TwoFactorAuthStatus) sql.NullBool {
	if status == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: status.GetEmailConfigured(), Valid: true}
}

func nullableTwoFactorEmailAddress(status *waappv1.TwoFactorAuthStatus) sql.NullString {
	if status == nil || strings.TrimSpace(status.GetEmailAddress()) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: strings.TrimSpace(status.GetEmailAddress()), Valid: true}
}

func nullableTwoFactorEmailVerified(status *waappv1.TwoFactorAuthStatus) sql.NullBool {
	if status == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: status.GetEmailVerified(), Valid: true}
}

func nullableTwoFactorEmailConfirmed(status *waappv1.TwoFactorAuthStatus) sql.NullBool {
	if status == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: status.GetEmailConfirmed(), Valid: true}
}

type clientProfileRow struct {
	id                   string
	waAccountIDValue     string
	protocolProfileID    string
	status               string
	registrationKeyState string
	messagingKeyState    string
	errCode              string
	errMessage           string
	errRetryable         bool
	lastUsedAt           sql.NullTime
	createdAt            time.Time
	updatedAt            time.Time
}

func (r clientProfileRow) toProto() *waappv1.ClientProfile {
	return &waappv1.ClientProfile{
		ClientProfileId:      r.id,
		WaAccountId:          r.waAccountIDValue,
		ProtocolProfileId:    r.protocolProfileID,
		Status:               waappv1.ClientProfileStatus(waappv1.ClientProfileStatus_value[r.status]),
		RegistrationKeyState: waappv1.KeyMaterialStatus(waappv1.KeyMaterialStatus_value[r.registrationKeyState]),
		MessagingKeyState:    waappv1.KeyMaterialStatus(waappv1.KeyMaterialStatus_value[r.messagingKeyState]),
		LastUsedAt:           sqlTime(r.lastUsedAt),
		Audit:                shared.NewAuditStamp(r.createdAt, r.updatedAt),
		LastError:            protoError(r.errCode, r.errMessage, r.errRetryable),
	}
}

type verificationRow struct {
	id                string
	waAccountIDValue  string
	clientProfileID   string
	method            string
	status            string
	length            int32
	retryAfterSeconds int64
	errCode           string
	errMessage        string
	errRetryable      bool
	requestedAt       time.Time
	expiresAt         sql.NullTime
}

func (r verificationRow) toProto() *waappv1.VerificationCodeRequestRecord {
	return &waappv1.VerificationCodeRequestRecord{
		VerificationRequestId: r.id,
		WaAccountId:           r.waAccountIDValue,
		ClientProfileId:       r.clientProfileID,
		DeliveryMethod:        waappv1.VerificationDeliveryMethod(waappv1.VerificationDeliveryMethod_value[r.method]),
		Status:                waappv1.VerificationRequestStatus(waappv1.VerificationRequestStatus_value[r.status]),
		ExpectedCodeLength:    r.length,
		RetryAfter:            shared.DurationFromSeconds(r.retryAfterSeconds),
		RequestedAt:           timestamppb.New(r.requestedAt.UTC()),
		ExpiresAt:             sqlTime(r.expiresAt),
		LastError:             protoError(r.errCode, r.errMessage, r.errRetryable),
	}
}
