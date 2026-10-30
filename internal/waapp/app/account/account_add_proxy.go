package account

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *profileHandler) ResolveAccountAddProxy(ctx context.Context, req *waappv1.ResolveAccountAddProxyRequest) (*waappv1.AccountProxyResponse, error) {
	phone := wamodel.NormalizePhone(req.GetPhone())
	if phone.GetE164Number() == "" {
		return accountProxyResponse(nil, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "account phone is required", false)), nil
	}
	record, err := s.store.FindWAAccountByPhone(ctx, phone.GetE164Number())
	if wamodel.IsWAAccountNotFound(err) {
		return &waappv1.AccountProxyResponse{}, nil
	}
	if err != nil {
		return accountProxyResponse(nil, err), nil
	}
	value, err := s.store.GetAccountProxy(ctx, record.GetWaAccountId())
	return &waappv1.AccountProxyResponse{Proxy: value, Error: shared.ToProtoError(err)}, nil
}
