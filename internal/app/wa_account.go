package app

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

var waAccountIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_-]{0,127}$`)

func newWAAccount(id string, displayName string, phone *waappv1.PhoneTarget, status waappv1.WAAccountStatus, audit *waappv1.AuditStamp) *waappv1.WAAccount {
	phone = normalizePhone(phone)
	return &waappv1.WAAccount{
		WaAccountId: strings.TrimSpace(id),
		DisplayName: strings.TrimSpace(displayName),
		Phone:       phone,
		Status:      normalizeWAAccountStatus(status),
		Audit:       audit,
	}
}

func withWAAccountStatus(account *waappv1.WAAccount, status waappv1.WAAccountStatus, updatedAt time.Time) *waappv1.WAAccount {
	createdAt := waAccountCreatedAt(account)
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	next := newWAAccount(waAccountID(account), account.GetDisplayName(), account.GetPhone(), status, audit(createdAt, updatedAt))
	next.TwoFactorAuth = cloneTwoFactorAuthStatus(account.GetTwoFactorAuth())
	return next
}

func withWAAccountDisplayName(account *waappv1.WAAccount, displayName string, updatedAt time.Time) *waappv1.WAAccount {
	createdAt := waAccountCreatedAt(account)
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	next := newWAAccount(waAccountID(account), displayName, account.GetPhone(), waAccountStatus(account), audit(createdAt, updatedAt))
	next.TwoFactorAuth = cloneTwoFactorAuthStatus(account.GetTwoFactorAuth())
	return next
}

func withWAAccountTwoFactorAuthStatus(account *waappv1.WAAccount, status *waappv1.TwoFactorAuthStatus, updatedAt time.Time) *waappv1.WAAccount {
	createdAt := waAccountCreatedAt(account)
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	next := newWAAccount(waAccountID(account), account.GetDisplayName(), account.GetPhone(), waAccountStatus(account), audit(createdAt, updatedAt))
	next.TwoFactorAuth = cloneTwoFactorAuthStatus(status)
	return next
}

func cloneTwoFactorAuthStatus(status *waappv1.TwoFactorAuthStatus) *waappv1.TwoFactorAuthStatus {
	if status == nil {
		return nil
	}
	return &waappv1.TwoFactorAuthStatus{
		Configured:      status.GetConfigured(),
		EmailConfigured: status.GetEmailConfigured(),
		EmailAddress:    strings.TrimSpace(status.GetEmailAddress()),
		EmailVerified:   status.GetEmailVerified(),
		EmailConfirmed:  status.GetEmailConfirmed(),
	}
}

func waAccountID(account *waappv1.WAAccount) string {
	return strings.TrimSpace(account.GetWaAccountId())
}

func waAccountStatus(account *waappv1.WAAccount) waappv1.WAAccountStatus {
	if account == nil {
		return waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_UNSPECIFIED
	}
	return normalizeWAAccountStatus(account.GetStatus())
}

func normalizeWAAccountStatus(status waappv1.WAAccountStatus) waappv1.WAAccountStatus {
	if status != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_UNSPECIFIED {
		return status
	}
	return waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_PENDING_REGISTRATION
}

func parseWAAccountStatus(value string) waappv1.WAAccountStatus {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_UNSPECIFIED
	}
	if !strings.HasPrefix(value, "WA_ACCOUNT_STATUS_") {
		value = "WA_ACCOUNT_STATUS_" + value
	}
	return waappv1.WAAccountStatus(waappv1.WAAccountStatus_value[value])
}

func waAccountStatusStorageValue(account *waappv1.WAAccount) string {
	return waAccountStatus(account).String()
}

func waAccountCreatedAt(account *waappv1.WAAccount) time.Time {
	return timeFromProto(account.GetAudit().GetCreatedAt())
}

func waAccountUpdatedAt(account *waappv1.WAAccount) time.Time {
	return timeFromProto(account.GetAudit().GetUpdatedAt())
}

func requireWAAccountID(value string) (string, error) {
	accountID := strings.TrimSpace(value)
	if accountID == "" {
		return "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "wa_account_id is required", false)
	}
	if !waAccountIDPattern.MatchString(accountID) {
		return "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "wa_account_id must use letters, digits, colon, underscore or dash", false)
	}
	return accountID, nil
}

func requireWAAccountIDValue(value string) (string, error) {
	accountID, err := requireWAAccountID(value)
	if err != nil {
		return "", fmt.Errorf("%w", err)
	}
	return accountID, nil
}
