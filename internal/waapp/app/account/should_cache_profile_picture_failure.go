package account

import (
	"context"
	"errors"
	"log"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	protocolcommunication "github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func shouldCacheProfilePictureFailure(err error) bool {
	var appErr *shared.AppError
	if !errors.As(err, &appErr) {
		return false
	}
	if appErr == nil {
		return false
	}
	return appErr.Code == waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND
}

func (s *serverCore) deleteWAAccountProfilePictureCache(ctx context.Context, accountID string) {
	if s == nil || s.runtime == nil || accountID == "" {
		return
	}
	key := accountProfilePictureCacheKey(accountID)
	_ = s.runtime.DeleteTransientState(ctx, key)
	_ = s.runtime.DeleteTransientState(ctx, profilePictureFailureCacheKey(key))
}

func accountProfilePictureCacheKey(accountID string) string {
	return "wa-account-profile-picture:" + strings.TrimSpace(accountID)
}

func contactProfilePictureCacheKey(contactID string, profilePictureID string) string {
	return "wa-contact-profile-picture:" + contactID + ":" + profilePictureID
}

func contactProfilePictureCacheVersion(profilePictureID string) string {
	profilePictureID = strings.TrimSpace(profilePictureID)
	if profilePictureID == "" {
		return contactProfilePictureLatestCacheVersion
	}
	return profilePictureID
}

func accountProfilePictureJID(account *waappv1.WAAccount) string {
	if account == nil {
		return ""
	}
	phone := wamodel.NormalizePhone(account.GetPhone())
	digits := shared.DigitsOnly(phone.GetE164Number())
	if digits == "" {
		digits = shared.DigitsOnly(phone.GetCountryCallingCode() + phone.GetNationalNumber())
	}
	return wacore.NormalizeWAJID(digits)
}

func IsWAProfilePictureNotFound(err error) bool {
	var appErr *shared.AppError
	return errors.As(err, &appErr) && appErr.Code == waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND
}

func IsWAContactProfilePictureNotFound(err error) bool {
	return IsWAProfilePictureNotFound(err)
}

func logWAProfilePictureError(scope string, err error) {
	var appErr *shared.AppError
	if errors.As(err, &appErr) {
		log.Printf("WA %s profile picture fetch failed code=%s retryable=%t", shared.SafeProxyLogToken(scope, "profile"), appErr.Code.String(), appErr.Retryable)
		return
	}
	log.Printf("WA %s profile picture fetch failed code=%s retryable=false reason=%s", shared.SafeProxyLogToken(scope, "profile"), waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL.String(), protocolcommunication.ContactProfilePictureFailureReason(err))
}
