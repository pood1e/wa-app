package account

import (
	"context"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
	"google.golang.org/protobuf/proto"
)

func (s *profileHandler) GetAccountProxy(ctx context.Context, req *waappv1.GetAccountProxyRequest) (*waappv1.AccountProxyResponse, error) {
	id, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return accountProxyResponse(nil, err), nil
	}
	if _, err := s.GetWAAccountRecord(ctx, id); err != nil {
		return accountProxyResponse(nil, err), nil
	}
	value, err := s.store.GetAccountProxy(ctx, id)
	return accountProxyResponse(value, err), nil
}

func (s *profileHandler) SetAccountProxy(ctx context.Context, req *waappv1.SetAccountProxyRequest) (*waappv1.AccountProxyResponse, error) {
	id, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return accountProxyResponse(nil, err), nil
	}
	if err := s.lifecycle.Acquire(ctx, 1); err != nil {
		return accountProxyResponse(nil, err), nil
	}
	defer s.lifecycle.Release(1)
	if _, err := s.GetWAAccountRecord(ctx, id); err != nil {
		return accountProxyResponse(nil, err), nil
	}
	current, err := s.store.GetAccountProxy(ctx, id)
	if err != nil {
		return accountProxyResponse(nil, err), nil
	}
	requested := req.GetProxy()
	value, err := wamodel.NormalizeAccountProxy(requested)
	if err != nil {
		return accountProxyResponse(nil, err), nil
	}
	if value != nil && requested.Password == nil && current.GetHost() == value.GetHost() && current.GetPort() == value.GetPort() && current.GetProtocol() == value.GetProtocol() && current.GetUsername() == value.GetUsername() {
		value.Password = proto.CloneOf(current.GetPassword())
	}
	if !proto.Equal(current, value) {
		if err := s.store.SaveAccountProxy(ctx, id, value); err != nil {
			return accountProxyResponse(nil, err), nil
		}
	}
	login, err := s.store.GetActiveLoginState(ctx, id, "")
	if shared.ToProtoError(err).GetCode() == waappv1.WaErrorCode_WA_ERROR_CODE_REGISTRATION_NOT_FOUND {
		return accountProxyResponse(value, nil), nil
	}
	if err != nil {
		return accountProxyResponse(value, err), nil
	}
	// Complete the local connection change after its configuration is durable.
	reconnect, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	if err := s.longConnections.Reconnect(reconnect, login); err != nil {
		return accountProxyResponse(value, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "account proxy was saved; connection refresh must be retried", true)), nil
	}
	return accountProxyResponse(value, nil), nil
}

func accountProxyResponse(value *waappv1.AccountProxy, err error) *waappv1.AccountProxyResponse {
	if value != nil {
		value = proto.Clone(value).(*waappv1.AccountProxy)
		if value.GetPassword().GetValue() != "" {
			value.Password = &waappv1.SensitiveText{RedactedValue: "********"}
		}
	}
	return &waappv1.AccountProxyResponse{Proxy: value, Error: shared.ToProtoError(err)}
}
