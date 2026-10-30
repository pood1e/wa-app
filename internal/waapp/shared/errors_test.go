package shared

import (
	"errors"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/proto"
)

func TestErrorFromProtoPreservesNilAndErrorFields(t *testing.T) {
	for _, input := range []*waappv1.WaError{nil, {}} {
		var err error = ErrorFromProto(input)
		if err != nil || ToProtoError(err) != nil {
			t.Fatal("successful response became a non-nil error")
		}
	}
	input := &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, Message: "retry later", Retryable: true}
	err := ErrorFromProto(input)
	var app *AppError
	if !errors.As(err, &app) || app == nil || !proto.Equal(ToProtoError(err), input) {
		t.Fatal("RPC error lost its identity or fields")
	}
}

func TestToProtoErrorAcceptsNilAppError(t *testing.T) {
	var app *AppError
	if ToProtoError(app) != nil {
		t.Fatal("nil application error became a failure")
	}
}
