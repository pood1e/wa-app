package app

import (
	"database/sql"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
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

func parseDeliveryMethods(values []string) []waappv1.VerificationDeliveryMethod {
	out := make([]waappv1.VerificationDeliveryMethod, 0, len(values))
	for _, value := range values {
		out = append(out, waappv1.VerificationDeliveryMethod(waappv1.VerificationDeliveryMethod_value[value]))
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
		Audit:             audit(r.createdAt, r.updatedAt),
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
	account := newWAAccount(r.id, r.displayName, &waappv1.PhoneTarget{
		E164Number:         r.e164,
		CountryCallingCode: r.cc,
		NationalNumber:     r.national,
		CountryIso2:        r.iso2,
	}, waappv1.WAAccountStatus(waappv1.WAAccountStatus_value[r.status]), audit(r.createdAt, r.updatedAt))
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
		Audit:                audit(r.createdAt, r.updatedAt),
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
		RetryAfter:            durationFromSeconds(r.retryAfterSeconds),
		RequestedAt:           timestamppb.New(r.requestedAt.UTC()),
		ExpiresAt:             sqlTime(r.expiresAt),
		LastError:             protoError(r.errCode, r.errMessage, r.errRetryable),
	}
}

type registrationRow struct {
	id                    string
	verificationRequestID string
	waAccountIDValue      string
	clientProfileID       string
	status                string
	identityID            string
	serviceAccountID      string
	serviceLoginID        string
	errCode               string
	errMessage            string
	errRetryable          bool
	submittedAt           time.Time
	completedAt           sql.NullTime
}

func (r registrationRow) toProto() *waappv1.RegistrationRecord {
	var identity *waappv1.RegisteredIdentity
	if r.identityID != "" {
		identity = &waappv1.RegisteredIdentity{
			RegisteredIdentityId: r.identityID,
			WaAccountId:          r.waAccountIDValue,
			ClientProfileId:      r.clientProfileID,
			ServiceAccountId:     r.serviceAccountID,
			ServiceLoginId:       r.serviceLoginID,
			RegisteredAt:         sqlTime(r.completedAt),
		}
	}
	return &waappv1.RegistrationRecord{
		RegistrationId:        r.id,
		VerificationRequestId: r.verificationRequestID,
		WaAccountId:           r.waAccountIDValue,
		ClientProfileId:       r.clientProfileID,
		Status:                waappv1.RegistrationStatus(waappv1.RegistrationStatus_value[r.status]),
		Identity:              identity,
		SubmittedAt:           timestamppb.New(r.submittedAt.UTC()),
		CompletedAt:           sqlTime(r.completedAt),
		LastError:             protoError(r.errCode, r.errMessage, r.errRetryable),
	}
}

type loginStateRow struct {
	id                   string
	registrationID       string
	waAccountIDValue     string
	clientProfileID      string
	registeredIdentityID string
	serviceAccountID     string
	serviceLoginID       string
	status               string
	errCode              string
	errMessage           string
	errRetryable         bool
	registeredAt         time.Time
	lastVerifiedAt       sql.NullTime
	createdAt            time.Time
	updatedAt            time.Time
}

func (r loginStateRow) toProto() *waappv1.LoginState {
	return &waappv1.LoginState{
		LoginStateId:         r.id,
		RegistrationId:       r.registrationID,
		WaAccountId:          r.waAccountIDValue,
		ClientProfileId:      r.clientProfileID,
		RegisteredIdentityId: r.registeredIdentityID,
		ServiceAccountId:     r.serviceAccountID,
		ServiceLoginId:       r.serviceLoginID,
		Status:               waappv1.LoginStateStatus(waappv1.LoginStateStatus_value[r.status]),
		RegisteredAt:         timestamppb.New(r.registeredAt.UTC()),
		LastVerifiedAt:       sqlTime(r.lastVerifiedAt),
		Audit:                audit(r.createdAt, r.updatedAt),
		LastError:            protoError(r.errCode, r.errMessage, r.errRetryable),
	}
}

type sessionRow struct {
	id                string
	waAccountIDValue  string
	clientProfileID   string
	identityID        string
	protocolProfileID string
	status            string
	errCode           string
	errMessage        string
	errRetryable      bool
	openedAt          time.Time
	lastSeenAt        sql.NullTime
	closedAt          sql.NullTime
}

func (r sessionRow) toProto() *waappv1.MessageSession {
	return &waappv1.MessageSession{
		MessageSessionId:     r.id,
		WaAccountId:          r.waAccountIDValue,
		ClientProfileId:      r.clientProfileID,
		RegisteredIdentityId: r.identityID,
		ProtocolProfileId:    r.protocolProfileID,
		Status:               waappv1.MessageSessionStatus(waappv1.MessageSessionStatus_value[r.status]),
		OpenedAt:             timestamppb.New(r.openedAt.UTC()),
		LastSeenAt:           sqlTime(r.lastSeenAt),
		ClosedAt:             sqlTime(r.closedAt),
		LastError:            protoError(r.errCode, r.errMessage, r.errRetryable),
	}
}

type messageRow struct {
	id                string
	sessionID         string
	kind              string
	encryptionState   string
	ackStatus         string
	direction         string
	source            string
	contactRef        string
	senderRef         string
	payloadRef        string
	providerMessageID string
	providerTimestamp sql.NullTime
	readAt            sql.NullTime
	deleteStatus      string
	deletedAt         sql.NullTime
	errCode           string
	errMessage        string
	errRetryable      bool
	receivedAt        time.Time
}

func (r messageRow) toProto() *waappv1.InboundMessage {
	return &waappv1.InboundMessage{
		MessageId:         r.id,
		MessageSessionId:  r.sessionID,
		Kind:              waappv1.InboundMessageKind(waappv1.InboundMessageKind_value[r.kind]),
		EncryptionState:   waappv1.MessageEncryptionState(waappv1.MessageEncryptionState_value[r.encryptionState]),
		AckStatus:         waappv1.MessageAckStatus(waappv1.MessageAckStatus_value[r.ackStatus]),
		Direction:         messageDirection(r.direction),
		Source:            messageSource(r.source),
		ContactRef:        r.contactRef,
		SenderRef:         r.senderRef,
		PayloadRef:        r.payloadRef,
		ProviderMessageId: r.providerMessageID,
		ProviderTimestamp: sqlTime(r.providerTimestamp),
		ReadAt:            sqlTime(r.readAt),
		DeleteStatus:      messageDeleteStatus(r.deleteStatus),
		DeletedAt:         sqlTime(r.deletedAt),
		ReceivedAt:        timestamppb.New(r.receivedAt.UTC()),
		LastError:         protoError(r.errCode, r.errMessage, r.errRetryable),
	}
}

func scanPostgresInboundMessage(scanner interface{ Scan(...any) error }) (*waappv1.InboundMessage, error) {
	var r messageRow
	if err := scanner.Scan(&r.id, &r.sessionID, &r.kind, &r.encryptionState, &r.ackStatus, &r.direction, &r.source, &r.contactRef, &r.senderRef, &r.payloadRef, &r.providerMessageID, &r.providerTimestamp, &r.readAt, &r.deleteStatus, &r.deletedAt, &r.errCode, &r.errMessage, &r.errRetryable, &r.receivedAt); err != nil {
		return nil, err
	}
	return r.toProto(), nil
}

func messageDirection(value string) waappv1.AccountMessageDirection {
	if value == "" {
		return waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND
	}
	status, ok := waappv1.AccountMessageDirection_value[value]
	if !ok {
		return waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_UNSPECIFIED
	}
	return waappv1.AccountMessageDirection(status)
}

func messageSource(value string) waappv1.AccountMessageSource {
	if value == "" {
		return waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_LONG_CONNECTION
	}
	status, ok := waappv1.AccountMessageSource_value[value]
	if !ok {
		return waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_UNSPECIFIED
	}
	return waappv1.AccountMessageSource(status)
}

func messageDeleteStatus(value string) waappv1.MessageDeleteStatus {
	if value == "" {
		return waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_NOT_DELETED
	}
	status, ok := waappv1.MessageDeleteStatus_value[value]
	if !ok {
		return waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_UNSPECIFIED
	}
	return waappv1.MessageDeleteStatus(status)
}

type decryptedRow struct {
	id           string
	messageID    string
	status       string
	plaintextRef string
	plaintext    string
	redacted     string
	secretRef    string
	errCode      string
	errMessage   string
	errRetryable bool
	decryptedAt  time.Time
}

func (r decryptedRow) toProto() *waappv1.DecryptedMessage {
	return &waappv1.DecryptedMessage{
		DecryptedMessageId: r.id,
		MessageId:          r.messageID,
		Status:             waappv1.DecryptionStatus(waappv1.DecryptionStatus_value[r.status]),
		PlaintextRef:       r.plaintextRef,
		PlaintextText:      &waappv1.SensitiveText{Value: r.plaintext, RedactedValue: r.redacted, SecretRef: r.secretRef},
		DecryptedAt:        timestamppb.New(r.decryptedAt.UTC()),
		LastError:          protoError(r.errCode, r.errMessage, r.errRetryable),
	}
}

type otpMessageRow struct {
	id                   string
	waAccountIDValue     string
	clientProfileID      string
	registeredIdentityID string
	messageID            string
	candidateID          string
	source               string
	sourceParty          string
	otpValue             string
	otpRedacted          string
	otpSecretRef         string
	receivedAt           time.Time
	expiresAt            sql.NullTime
	createdAt            time.Time
	updatedAt            time.Time
}

func (r otpMessageRow) toProto(includeSensitiveValue bool) *waappv1.OtpMessage {
	text := &waappv1.SensitiveText{
		RedactedValue: r.otpRedacted,
		SecretRef:     r.otpSecretRef,
	}
	if includeSensitiveValue {
		text.Value = r.otpValue
	}
	return &waappv1.OtpMessage{
		OtpMessageId:         r.id,
		WaAccountId:          r.waAccountIDValue,
		ClientProfileId:      r.clientProfileID,
		RegisteredIdentityId: r.registeredIdentityID,
		MessageId:            r.messageID,
		CandidateId:          r.candidateID,
		Source:               waappv1.WaOtpSource(waappv1.WaOtpSource_value[r.source]),
		SourceParty:          r.sourceParty,
		Otp:                  text,
		ReceivedAt:           timestamppb.New(r.receivedAt.UTC()),
		ExpiresAt:            sqlTime(r.expiresAt),
		Audit:                audit(r.createdAt, r.updatedAt),
	}
}

type contactRow struct {
	id               string
	waAccountIDValue string
	jid              string
	number           string
	displayName      string
	waName           string
	verifiedName     string
	profilePictureID string
	kind             string
	isWhatsAppUser   bool
	isReachable      bool
	createdAt        time.Time
	updatedAt        time.Time
	messageCount     int64
	unreadCount      int64
	lastMessageAt    sql.NullTime
	lastPlaintext    string
	lastRedacted     string
	lastPayloadRef   string
	lastEncryption   string
}

func (r contactRow) toProto() *waappv1.WAContact {
	kind := waappv1.WAContactKind(waappv1.WAContactKind_value[r.kind])
	contact := &waappv1.WAContact{
		ContactId:        r.id,
		WaAccountId:      r.waAccountIDValue,
		Jid:              r.jid,
		Number:           r.number,
		DisplayName:      r.displayName,
		WaName:           r.waName,
		VerifiedName:     r.verifiedName,
		ProfilePictureId: r.profilePictureID,
		Kind:             kind,
		IsWhatsappUser:   r.isWhatsAppUser,
		IsReachable:      r.isReachable,
		Audit:            audit(r.createdAt, r.updatedAt),
		MessageCount:     int32(r.messageCount),
		UnreadCount:      int32(r.unreadCount),
		LastMessageAt:    sqlTime(r.lastMessageAt),
		LastMessagePreview: contactMessagePreview(
			r.lastPlaintext,
			r.lastRedacted,
			r.lastPayloadRef,
			waappv1.MessageEncryptionState(waappv1.MessageEncryptionState_value[r.lastEncryption]),
		),
	}
	enrichWAContactFallback(contact)
	return contact
}
