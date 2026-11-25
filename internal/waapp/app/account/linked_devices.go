package account

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"time"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type linkedDeviceHandler struct{ *serverCore }

type linkedDeviceOperations interface {
	ListLinkedDevices(context.Context) ([]*waappv1.LinkedDevice, error)
	LinkDevice(context.Context, string) (*waappv1.LinkDeviceResponse, error)
	UnlinkDevice(context.Context, string) error
}

func (s *linkedDeviceHandler) ListLinkedDevices(ctx context.Context, request *waappv1.ListLinkedDevicesRequest) (*waappv1.ListLinkedDevicesResponse, error) {
	runner, err := s.linkedDeviceRunner(ctx, request.GetWaAccountId())
	if err != nil {
		return &waappv1.ListLinkedDevicesResponse{Error: shared.ToProtoError(err)}, nil
	}
	devices, err := runner.ListLinkedDevices(ctx)
	return &waappv1.ListLinkedDevicesResponse{Devices: devices, Error: shared.ToProtoError(err)}, nil
}

func (s *linkedDeviceHandler) LinkDevice(ctx context.Context, request *waappv1.LinkDeviceRequest) (*waappv1.LinkDeviceResponse, error) {
	qr, err := wire.ParseDevicePairingQR(request.GetQrCode().GetValue())
	if err != nil {
		return &waappv1.LinkDeviceResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false))}, nil
	}
	defer clear(qr.Secret)
	runner, err := s.linkedDeviceRunner(ctx, request.GetWaAccountId())
	if err != nil {
		return &waappv1.LinkDeviceResponse{Error: shared.ToProtoError(err)}, nil
	}
	fingerprint := sha256.New()
	for _, value := range [][]byte{[]byte(qr.Reference), qr.NoisePublic, qr.IdentityPublic, qr.Secret} {
		_, _ = fingerprint.Write(value)
	}
	key := "device-link/" + request.GetWaAccountId() + "/" + hex.EncodeToString(fingerprint.Sum(nil))
	if response := s.cachedDeviceLink(ctx, key); response != nil {
		return response, nil
	}
	claimed, err := s.runtime.ClaimLease(ctx, key, s.ids.NewID("wa_link_"), 5*time.Minute)
	if err != nil {
		return &waappv1.LinkDeviceResponse{Error: shared.ToProtoError(err)}, nil
	}
	if !claimed {
		if response := s.cachedDeviceLink(ctx, key); response != nil {
			return response, nil
		}
		return &waappv1.LinkDeviceResponse{OutcomeUnknown: true, Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT,
			"this QR authorization was already submitted; inspect the companion and refresh linked devices", false))}, nil
	}
	response, err := runner.LinkDevice(ctx, request.GetQrCode().GetValue())
	if response == nil {
		response = &waappv1.LinkDeviceResponse{}
	}
	response.Error = shared.ToProtoError(err)
	raw, encodeErr := proto.Marshal(response)
	if encodeErr == nil {
		checkpoint, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if err := s.runtime.SaveTransientState(checkpoint, key, raw, 5*time.Minute); err != nil {
			log.Printf("WA linked-device result cache: %v", shared.SanitizeLogError(err))
		}
	}
	return response, nil
}

func (s *linkedDeviceHandler) cachedDeviceLink(ctx context.Context, key string) *waappv1.LinkDeviceResponse {
	raw, err := s.runtime.GetTransientState(ctx, key)
	if err != nil {
		return nil
	}
	response := new(waappv1.LinkDeviceResponse)
	if proto.Unmarshal(raw, response) != nil {
		return nil
	}
	return response
}

func (s *linkedDeviceHandler) UnlinkDevice(ctx context.Context, request *waappv1.UnlinkDeviceRequest) (*waappv1.UnlinkDeviceResponse, error) {
	runner, err := s.linkedDeviceRunner(ctx, request.GetWaAccountId())
	if err == nil {
		err = runner.UnlinkDevice(ctx, request.GetDeviceRef())
	}
	return &waappv1.UnlinkDeviceResponse{Error: shared.ToProtoError(err)}, nil
}

func (s *linkedDeviceHandler) linkedDeviceRunner(ctx context.Context, id string) (linkedDeviceOperations, error) {
	runner, err := s.accountConnection(ctx, id)
	if err != nil {
		return nil, err
	}
	devices, ok := runner.(linkedDeviceOperations)
	if !ok {
		return nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "linked-device operations are unavailable", false)
	}
	return devices, nil
}
