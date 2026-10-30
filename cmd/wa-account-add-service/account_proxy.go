package main

import (
	"context"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func resolveAccountProxy(ctx context.Context, runtime waappv1.WaAccountAcceptanceServiceClient, request *waappv1.BeginAccountAddRequest, fallback string) (*waappv1.AccountProxy, string, error) {
	value := request.GetProxy()
	if value == nil {
		response, err := runtime.ResolveAccountAddProxy(ctx, &waappv1.ResolveAccountAddProxyRequest{Phone: request.GetPhone()})
		if err != nil {
			return nil, "", err
		}
		if response.GetError() != nil {
			return nil, "", shared.ErrorFromProto(response.GetError())
		}
		value = response.GetProxy()
	}
	value, err := wamodel.NormalizeAccountProxy(value)
	if err != nil {
		return nil, "", err
	}
	if value == nil {
		return nil, fallback, nil
	}
	address, err := wamodel.AccountProxyURL(value)
	return value, address, err
}
