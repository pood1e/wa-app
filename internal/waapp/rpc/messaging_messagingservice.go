package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type waMessagingServiceServer struct {
	waappv1.UnimplementedWaMessagingServiceServer
	application *account.Application
}

func (s *waMessagingServiceServer) OpenMessageSession(ctx context.Context, req *waappv1.OpenMessageSessionRequest) (*waappv1.OpenMessageSessionResponse, error) {
	return s.application.OpenMessageSession(ctx, req)
}
func (s *waMessagingServiceServer) GetMessageSession(ctx context.Context, req *waappv1.GetMessageSessionRequest) (*waappv1.GetMessageSessionResponse, error) {
	return s.application.GetMessageSession(ctx, req)
}
func (s *waMessagingServiceServer) ReceiveMessageBatch(ctx context.Context, req *waappv1.ReceiveMessageBatchRequest) (*waappv1.ReceiveMessageBatchResponse, error) {
	return s.application.ReceiveMessageBatch(ctx, req)
}
func (s *waMessagingServiceServer) ListAccountMessages(ctx context.Context, req *waappv1.ListAccountMessagesRequest) (*waappv1.ListAccountMessagesResponse, error) {
	return s.application.ListAccountMessages(ctx, req)
}
func (s *waMessagingServiceServer) GetLongConnectionStatus(ctx context.Context, req *waappv1.GetLongConnectionStatusRequest) (*waappv1.GetLongConnectionStatusResponse, error) {
	return s.application.GetLongConnectionStatus(ctx, req)
}
func (s *waMessagingServiceServer) MarkAccountMessagesRead(ctx context.Context, req *waappv1.MarkAccountMessagesReadRequest) (*waappv1.MarkAccountMessagesReadResponse, error) {
	return s.application.MarkAccountMessagesRead(ctx, req)
}
func (s *waMessagingServiceServer) DeleteAccountMessages(ctx context.Context, req *waappv1.DeleteAccountMessagesRequest) (*waappv1.DeleteAccountMessagesResponse, error) {
	return s.application.DeleteAccountMessages(ctx, req)
}
func (s *waMessagingServiceServer) SendTextMessage(ctx context.Context, req *waappv1.SendTextMessageRequest) (*waappv1.SendTextMessageResponse, error) {
	return s.application.SendTextMessage(ctx, req)
}
func (s *waMessagingServiceServer) EditAccountMessage(ctx context.Context, req *waappv1.EditAccountMessageRequest) (*waappv1.SendTextMessageResponse, error) {
	return s.application.EditAccountMessage(ctx, req)
}
func (s *waMessagingServiceServer) ReactToAccountMessage(ctx context.Context, req *waappv1.ReactToAccountMessageRequest) (*waappv1.SendTextMessageResponse, error) {
	return s.application.ReactToAccountMessage(ctx, req)
}
func (s *waMessagingServiceServer) AcknowledgeMessage(ctx context.Context, req *waappv1.AcknowledgeMessageRequest) (*waappv1.AcknowledgeMessageResponse, error) {
	return s.application.AcknowledgeMessage(ctx, req)
}
func (s *waMessagingServiceServer) CloseMessageSession(ctx context.Context, req *waappv1.CloseMessageSessionRequest) (*waappv1.CloseMessageSessionResponse, error) {
	return s.application.CloseMessageSession(ctx, req)
}
