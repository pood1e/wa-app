package account

import (
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func withWAAccountStatus(account *waappv1.WAAccount, status waappv1.WAAccountStatus, updatedAt time.Time) *waappv1.WAAccount {
	createdAt := wamodel.WAAccountCreatedAt(account)
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	next := wamodel.NewWAAccount(wamodel.WAAccountID(account), account.GetDisplayName(), account.GetPhone(), status, shared.NewAuditStamp(createdAt, updatedAt))
	next.TwoFactorAuth = cloneTwoFactorAuthStatus(account.GetTwoFactorAuth())
	return next
}

func withWAAccountDisplayName(account *waappv1.WAAccount, displayName string, updatedAt time.Time) *waappv1.WAAccount {
	createdAt := wamodel.WAAccountCreatedAt(account)
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	next := wamodel.NewWAAccount(wamodel.WAAccountID(account), displayName, account.GetPhone(), wamodel.WAAccountStatus(account), shared.NewAuditStamp(createdAt, updatedAt))
	next.TwoFactorAuth = cloneTwoFactorAuthStatus(account.GetTwoFactorAuth())
	return next
}

func withWAAccountTwoFactorAuthStatus(account *waappv1.WAAccount, status *waappv1.TwoFactorAuthStatus, updatedAt time.Time) *waappv1.WAAccount {
	createdAt := wamodel.WAAccountCreatedAt(account)
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	next := wamodel.NewWAAccount(wamodel.WAAccountID(account), account.GetDisplayName(), account.GetPhone(), wamodel.WAAccountStatus(account), shared.NewAuditStamp(createdAt, updatedAt))
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
