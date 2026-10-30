package account

import (
	"unicode"
	"unicode/utf16"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func requireAccountPassword(value string) error {
	length := 0
	letter, digit := false, false
	for _, r := range utf16.Encode([]rune(value)) {
		length++
		letter = letter || unicode.IsLetter(rune(r))
		digit = digit || unicode.IsDigit(rune(r))
	}
	if length < 8 || length > 20 || !letter || !digit {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "两步验证密码需为 8–20 个字符，并包含字母和数字", false)
	}
	return nil
}
