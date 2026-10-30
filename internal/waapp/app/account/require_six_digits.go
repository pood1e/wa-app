package account

import (
	"net/mail"
	"strings"
	"unicode/utf8"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	protocolwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func requireSixDigits(value string, label string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) != 6 || shared.DigitsOnly(trimmed) != trimmed {
		return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, label+" must be 6 digits", false)
	}
	return trimmed, nil
}

func requiredEmailAddress(value string, label string) (string, error) {
	trimmed, err := optionalEmailAddress(value, label)
	if err != nil {
		return "", err
	}
	if trimmed == "" {
		return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, label+" is required", false)
	}
	return trimmed, nil
}

func optionalEmailAddress(value string, label string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	address, err := mail.ParseAddress(trimmed)
	if err != nil || address.Address != trimmed {
		return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, label+" is invalid", false)
	}
	return trimmed, nil
}

func accountSettingsLocale(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func requiredAccountProfileName(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "display_name is required", false)
	}
	if utf8.RuneCountInString(trimmed) > accountProfileNameMaxRunes {
		return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "display_name is too long", false)
	}
	return trimmed, nil
}

func requiredAccountProfilePicture(image []byte, contentType string) ([]byte, error) {
	if len(image) == 0 {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "image is required", false)
	}
	if len(image) > protocolwire.ProfilePictureDownloadMaxBytes {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "WA profile picture is too large", false)
	}
	if _, err := protocolwire.ProfilePictureContentType(image, contentType); err != nil {
		return nil, err
	}
	return append([]byte(nil), image...), nil
}
