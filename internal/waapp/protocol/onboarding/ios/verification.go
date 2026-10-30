package ios

import (
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type verificationChannel struct {
	available  bool
	nextCodeAt time.Time
}

var verificationMethods = [...]waappv1.VerificationDeliveryMethod{
	waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS,
	waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD,
	waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP,
}

// VerificationMethods reports availability and remaining cooldown for each channel.
func (f *Flow) VerificationMethods() []*waappv1.VerificationMethodStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	statuses := make([]*waappv1.VerificationMethodStatus, 0, len(verificationMethods))
	for _, method := range verificationMethods {
		channel := f.channels[method]
		remaining := time.Duration(0)
		if channel.available {
			remaining = max(0, time.Until(channel.nextCodeAt))
			if remaining%time.Second != 0 {
				remaining = (remaining/time.Second + 1) * time.Second
			}
		}
		statuses = append(statuses, &waappv1.VerificationMethodStatus{
			DeliveryMethod: method, Available: channel.available && remaining == 0, Cooldown: durationpb.New(remaining),
		})
	}
	return statuses
}

func (f *Flow) updateChannels(response *protocolv1.IOSRegistrationResponse, requested waappv1.VerificationDeliveryMethod) {
	probe := requested == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED
	if probe {
		f.channels = map[waappv1.VerificationDeliveryMethod]verificationChannel{
			waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS: {available: true},
		}
	}
	now := time.Now()
	retryAfter := response.GetRetryAfter()
	if response.GetReason() == "challenge" && response.GetNextMethod() == "recaptcha" {
		retryAfter = max(retryAfter, response.GetRecaptchaWait())
	}
	for _, method := range verificationMethods {
		channel := f.channels[method]
		wait, eligible := verificationAvailability(response, method)
		if method != waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS {
			if eligible != nil {
				channel.available = *eligible == 1
			}
			if probe && wait == nil {
				channel.available = false
			}
		}
		if wait != nil {
			channel.available = *wait >= 0 && (channel.available || method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS)
			channel.nextCodeAt = now.Add(verificationDelay(*wait))
		}
		if channel.available && retryAfter > 0 {
			if next := now.Add(verificationDelay(retryAfter)); next.After(channel.nextCodeAt) {
				channel.nextCodeAt = next
			}
		}
		if method == requested && response.GetStatus() == "fail" && !channel.nextCodeAt.After(now) {
			channel.available = false
		}
		f.channels[method] = channel
	}
}

func verificationAvailability(response *protocolv1.IOSRegistrationResponse, method waappv1.VerificationDeliveryMethod) (*int64, *int32) {
	switch method {
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD:
		return response.WaOldWait, response.WaOldEligible
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP:
		return response.EmailOtpWait, response.EmailOtpEligible
	default:
		return response.SmsWait, nil
	}
}

func verificationDelay(seconds int64) time.Duration {
	return time.Duration(min(max(seconds, 0), int64((1<<63-1)/time.Second))) * time.Second
}

func verificationMethodName(method waappv1.VerificationDeliveryMethod) (string, error) {
	switch method {
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS:
		return "sms", nil
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD:
		return "wa_old", nil
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP:
		return "email_otp", nil
	default:
		return "", errors.New("unsupported iOS verification channel")
	}
}

// VerificationCodeLength resolves the selected channel's reported code length.
func VerificationCodeLength(response *protocolv1.IOSRegistrationResponse, method waappv1.VerificationDeliveryMethod) int {
	length := response.GetLength()
	if length == 0 {
		switch method {
		case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD:
			length = response.GetWaOldLength()
		case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP:
			length = response.GetEmailOtpLength()
		default:
			length = response.GetSmsLength()
		}
	}
	if length <= 0 || length > 32 {
		return 6
	}
	return int(length)
}
