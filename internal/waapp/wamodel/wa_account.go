package wamodel

import (
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func NewWAAccount(id string, displayName string, phone *waappv1.PhoneTarget, status waappv1.WAAccountStatus, audit *waappv1.AuditStamp) *waappv1.WAAccount {
	phone = NormalizePhone(phone)
	return &waappv1.WAAccount{
		WaAccountId: strings.TrimSpace(id),
		DisplayName: strings.TrimSpace(displayName),
		Phone:       phone,
		Status:      NormalizeWAAccountStatus(status),
		Audit:       audit,
	}
}

func WAAccountID(account *waappv1.WAAccount) string {
	return strings.TrimSpace(account.GetWaAccountId())
}

func WAAccountStatus(account *waappv1.WAAccount) waappv1.WAAccountStatus {
	if account == nil {
		return waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_UNSPECIFIED
	}
	return NormalizeWAAccountStatus(account.GetStatus())
}

func NormalizeWAAccountStatus(status waappv1.WAAccountStatus) waappv1.WAAccountStatus {
	if status != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_UNSPECIFIED {
		return status
	}
	return waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_PENDING_REGISTRATION
}

func WAAccountStatusStorageValue(account *waappv1.WAAccount) string {
	return WAAccountStatus(account).String()
}

func WAAccountCreatedAt(account *waappv1.WAAccount) time.Time {
	return shared.TimeFromProto(account.GetAudit().GetCreatedAt())
}

func WAAccountUpdatedAt(account *waappv1.WAAccount) time.Time {
	return shared.TimeFromProto(account.GetAudit().GetUpdatedAt())
}
