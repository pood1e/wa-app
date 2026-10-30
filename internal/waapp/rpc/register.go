package rpc

import (
	"google.golang.org/grpc"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/app/account"
	addapp "github.com/pood1e/wa-app/internal/waapp/app/onboarding"
)

func RegisterRuntime(registrar grpc.ServiceRegistrar, application *account.Application) {
	waappv1.RegisterWaAccountProxyServiceServer(registrar, &accountProxyServiceServer{application: application})
	waappv1.RegisterWaAccountReviewServiceServer(registrar, &accountReviewServiceServer{application: application})
	waappv1.RegisterWaGroupServiceServer(registrar, &groupServiceServer{application: application})
	waappv1.RegisterWaAccountSettingsServiceServer(registrar, &waAccountSettingsServiceServer{application: application})
	waappv1.RegisterWaMessagingServiceServer(registrar, &waMessagingServiceServer{application: application})
	waappv1.RegisterWaProfileServiceServer(registrar, &waProfileServiceServer{application: application})
	waappv1.RegisterWaAccountAcceptanceServiceServer(registrar, &waAccountAcceptanceServiceServer{application: application})
	waappv1.RegisterWaLoginServiceServer(registrar, &waLoginServiceServer{application: application})
	waappv1.RegisterWaExtractionServiceServer(registrar, &waExtractionServiceServer{application: application})
	waappv1.RegisterWaContactServiceServer(registrar, &waContactServiceServer{application: application})
	waappv1.RegisterWaDiscoveryServiceServer(registrar, &waDiscoveryServiceServer{application: application})
}
func RegisterAccountAdd(registrar grpc.ServiceRegistrar, application *addapp.Service) {
	waappv1.RegisterWaAccountAddServiceServer(registrar, &waAccountAddServiceServer{application: application})
}
