package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type linkedDeviceServiceServer struct {
	waappv1.UnimplementedWaLinkedDeviceServiceServer
	application *account.Application
}

func (s *linkedDeviceServiceServer) ListLinkedDevices(ctx context.Context, request *waappv1.ListLinkedDevicesRequest) (*waappv1.ListLinkedDevicesResponse, error) {
	return s.application.ListLinkedDevices(ctx, request)
}

func (s *linkedDeviceServiceServer) LinkDevice(ctx context.Context, request *waappv1.LinkDeviceRequest) (*waappv1.LinkDeviceResponse, error) {
	return s.application.LinkDevice(ctx, request)
}

func (s *linkedDeviceServiceServer) UnlinkDevice(ctx context.Context, request *waappv1.UnlinkDeviceRequest) (*waappv1.UnlinkDeviceResponse, error) {
	return s.application.UnlinkDevice(ctx, request)
}
