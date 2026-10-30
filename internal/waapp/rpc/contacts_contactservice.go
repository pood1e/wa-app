package rpc

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
)

type waContactServiceServer struct {
	waappv1.UnimplementedWaContactServiceServer
	application *account.Application
}

func (s *waContactServiceServer) ListWAContacts(ctx context.Context, req *waappv1.ListWAContactsRequest) (*waappv1.ListWAContactsResponse, error) {
	return s.application.ListWAContacts(ctx, req)
}
func (s *waContactServiceServer) SaveWAContact(ctx context.Context, req *waappv1.SaveWAContactRequest) (*waappv1.SaveWAContactResponse, error) {
	return s.application.SaveWAContact(ctx, req)
}
func (s *waContactServiceServer) ResolveWAContacts(ctx context.Context, req *waappv1.ResolveWAContactsRequest) (*waappv1.ResolveWAContactsResponse, error) {
	return s.application.ResolveWAContacts(ctx, req)
}
func (s *waContactServiceServer) DeleteWAContact(ctx context.Context, req *waappv1.DeleteWAContactRequest) (*waappv1.DeleteWAContactResponse, error) {
	return s.application.DeleteWAContact(ctx, req)
}

func (s *waContactServiceServer) SubscribeWAContactPresence(ctx context.Context, req *waappv1.SubscribeWAContactPresenceRequest) (*waappv1.SubscribeWAContactPresenceResponse, error) {
	return s.application.SubscribeWAContactPresence(ctx, req)
}
