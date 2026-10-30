package communication

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

func EmailSetResultFromIQ(node pwire.ChatdNode, emailAddress string) wacore.EngineAccountSettingsResult {
	status := &waappv1.TwoFactorAuthStatus{EmailConfigured: true, EmailAddress: strings.TrimSpace(emailAddress)}
	emailNode, ok := ChatdChild(node, "email")
	if !ok {
		return MalformedAccountSettingsResult("WA set email response is missing email")
	}
	doVerify, ok := ChatdNodeBoolValue(emailNode, "do_verify")
	if !ok {
		return MalformedAccountSettingsResult("WA set email response is missing do_verify")
	}
	if doVerify {
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_NEEDS_VERIFICATION, TwoFactorStatus: status}
	}
	autoVerifyStatus := ChatdNodeStatus(emailNode, "auto_verify")
	if autoVerifyStatus == "success" {
		status.EmailVerified = true
		return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED, TwoFactorStatus: status}
	}
	return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "WA set email was not accepted for verification", false)}
}

func EmailOtpRequestResultFromIQ(node pwire.ChatdNode) wacore.EngineAccountSettingsResult {
	verifyNode, ok := ChatdChild(node, "verify_email")
	if !ok {
		return MalformedAccountSettingsResult("WA email OTP request response is missing verify_email")
	}
	wait, ok := emailOTPWaitTime(verifyNode)
	if !ok {
		return MalformedAccountSettingsResult("WA email OTP request response has an invalid wait_time")
	}
	return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_WAITING, WaitTime: wait}
}

func EmailOtpVerifyResultFromIQ(node pwire.ChatdNode) wacore.EngineAccountSettingsResult {
	verifyNode, ok := ChatdChild(node, "verify_email")
	if !ok {
		return MalformedAccountSettingsResult("WA email OTP verify response is missing verify_email")
	}
	codeMatch, ok := ChatdNodeBoolValue(verifyNode, "code_match")
	if !ok {
		return MalformedAccountSettingsResult("WA email OTP verify response is missing code_match")
	}
	wait, ok := emailOTPWaitTime(verifyNode)
	if !ok {
		return MalformedAccountSettingsResult("WA email OTP verify response has an invalid wait_time")
	}
	status := waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_CODE_MISMATCH
	if codeMatch {
		status = waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED
	}
	result := wacore.EngineAccountSettingsResult{Status: status, WaitTime: wait}
	if status == waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED {
		result.TwoFactorStatus = &waappv1.TwoFactorAuthStatus{EmailConfigured: true, EmailVerified: true}
	}
	return result
}

func emailOTPWaitTime(node pwire.ChatdNode) (time.Duration, bool) {
	value, ok := ChatdNodeStringValue(node, "wait_time")
	if !ok {
		return 0, false
	}
	seconds, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

func MalformedAccountSettingsResult(message string) wacore.EngineAccountSettingsResult {
	return wacore.EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, message, false)}
}

func AccountSettingsRequestError(err error) error {
	if AccountSettingsTimeoutError(err) {
		return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, AccountSettingsRequestTimeoutMessage, true)
	}
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "native account settings request failed", IsRetryableTransportError(err))
}

func AccountSettingsTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "timed out") || strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline")
}

func IsRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{"timeout", "deadline", "proxy", "dial", "connect", "network", "tls", "no such host", "connection refused", "temporary"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
