// wa-ios-onboarding exercises the host-owned iOS registration protocol in memory.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/attestation"
	"github.com/pood1e/wa-app/internal/waapp/protocol/onboarding/ios"
)

type command struct {
	Operation string `json:"operation"`
	OTPFile   string `json:"otp_file"`
}

type observation struct {
	Operation  string `json:"operation"`
	Status     string `json:"status,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Method     string `json:"method,omitempty"`
	CodeLength int32  `json:"code_length,omitempty"`
	Error      string `json:"error,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	RetryAfter *int64 `json:"retry_after_seconds,omitempty"`
	SMSWait    *int64 `json:"sms_wait_seconds,omitempty"`
}

func main() {
	target := flag.String("target", "", "private App Attest socket or loopback tunnel")
	phoneFile := flag.String("phone", "", "private PhoneTarget Proto JSON")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if err := run(ctx, *target, *phoneFile); err != nil {
		fmt.Fprintln(os.Stderr, "iOS onboarding failed")
		os.Exit(1)
	}
}

func run(ctx context.Context, target, phoneFile string) (err error) {
	input, err := os.Open("/dev/stdin")
	if err != nil {
		return errors.New("command input is unavailable")
	}
	defer input.Close()
	profile, err := ios.DefaultProfile()
	if err != nil {
		return err
	}
	phone := new(waappv1.PhoneTarget)
	if err := readProto(phoneFile, phone); err != nil {
		return err
	}
	connection, err := attestation.Dial(target)
	if err != nil {
		return err
	}
	defer connection.Close()
	flow, err := ios.Begin(ctx, phone, profile, nil, ios.Dependencies{
		Attestation: waappv1.NewAppAttestServiceClient(connection),
		Recaptcha:   waappv1.NewRecaptchaServiceClient(connection),
	})
	if err != nil {
		emit("begin", nil, err)
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		err = errors.Join(err, flow.Close(cleanup))
	}()
	emit("ready", nil, nil)
	readerContext, cancelReader := context.WithCancel(ctx)
	lines := make(chan string)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(lines)
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4096), 4096)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-readerContext.Done():
				return
			}
		}
	}()
	defer func() { cancelReader(); _ = input.Close(); <-done }()
	var requestedAt time.Time
	var lastResponse *protocolv1.IOSRegistrationResponse
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, open := <-lines:
			if !open {
				return nil
			}
			var input command
			if json.Unmarshal([]byte(line), &input) != nil {
				emit("command", nil, errors.New("invalid command"))
				continue
			}
			var response *protocolv1.IOSRegistrationResponse
			switch input.Operation {
			case "stop":
				return nil
			case "probe":
				response, err = flow.Probe(ctx)
			case "status":
				response, err = lastResponse, nil
			case "request_code":
				started := time.Now()
				response, err = flow.RequestCode(ctx, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS)
				if err == nil && response.GetStatus() == "sent" {
					requestedAt = started
				}
			case "submit_code":
				var code string
				code, err = readOTP(input.OTPFile, requestedAt)
				if err == nil {
					response, err = flow.SubmitCode(ctx, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, code)
				}
			default:
				input.Operation = "command"
				err = errors.New("unsupported command")
			}
			if response != nil {
				lastResponse = response
			}
			emit(input.Operation, response, err)
		}
	}
}

func readProto(path string, value proto.Message) error {
	data, err := readPrivate(path)
	if err != nil {
		return err
	}
	if err := protojson.Unmarshal(data, value); err != nil {
		return errors.New("invalid private Proto JSON")
	}
	return nil
}

func readPrivate(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 32768 {
		return nil, errors.New("private input file is unavailable")
	}
	return os.ReadFile(path)
}

func readOTP(path string, requestedAt time.Time) (string, error) {
	data, err := readPrivate(path)
	if err != nil {
		return "", err
	}
	var input struct {
		Code          string    `json:"code"`
		ReceivedAt    time.Time `json:"received_at"`
		Fresh         bool      `json:"fresh"`
		NumberMatches bool      `json:"number_matches"`
	}
	if json.Unmarshal(data, &input) != nil || requestedAt.IsZero() || !input.Fresh || !input.NumberMatches || input.ReceivedAt.Before(requestedAt.Add(-2*time.Second)) {
		return "", errors.New("fresh matching verification code is required")
	}
	return input.Code, nil
}

func emit(operation string, response *protocolv1.IOSRegistrationResponse, err error) {
	result := observation{Operation: operation, Status: publicState(response.GetStatus()), Reason: publicState(response.GetReason()),
		Method: publicState(response.GetMethod()), CodeLength: response.GetLength()}
	if response != nil {
		result.RetryAfter, result.SMSWait = response.RetryAfter, response.SmsWait
	}
	if err != nil {
		result.Error = "operation_failed"
		var httpError *ios.HTTPStatusError
		if errors.Is(err, ios.ErrCodeCooldown) {
			result.Error = "sms_cooldown"
		} else if errors.As(err, &httpError) {
			result.Error, result.HTTPStatus = "http_status", httpError.StatusCode
		} else if rpc, ok := status.FromError(err); ok {
			result.Error = rpc.Code().String()
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
}

func publicState(value string) string {
	switch value {
	case "", "ok", "fail", "incorrect", "sent", "sms", "voice", "blocked", "too_recent", "too_many", "too_many_all_methods", "temporarily_unavailable", "no_routes", "bad_token", "bad_param", "missing", "expired", "old_version", "security_code", "captcha", "wait":
		return value
	default:
		return "other"
	}
}
