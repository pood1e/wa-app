package onboarding

import (
	"crypto/sha256"
	"errors"
	"math"
	"strconv"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"google.golang.org/protobuf/proto"
)

// DefaultAndroidClient returns host-owned logical client parameters.
// These describe the protocol client and do not select or identify a signing device.
func DefaultAndroidClient() *protocolv1.AndroidClientParameters {
	permissions := []byte{
		0xab, 0x5f, 0x60, 0x99, 0xa4, 0x1a, 0x21, 0x35,
		0x6d, 0xca, 0x80, 0x42, 0x74, 0xbb, 0x03, 0xec,
		0x28, 0x99, 0x2a, 0xbc, 0x86, 0x03, 0xdc, 0xe5,
		0xb4, 0x4f, 0x32, 0x4d, 0xdd, 0xf4, 0x35, 0xd2,
	}
	emptyLibraries := sha256.Sum256([]byte("signal_error"))
	return &protocolv1.AndroidClientParameters{
		Metadata: &protocolv1.ClientMetadata{
			Platform:  protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID,
			OsVersion: "17", Manufacturer: "Google", Device: "raven",
			OsBuildNumber: "CP2A.260705.006", DeviceBoard: "raven", ModelType: "Pixel 6 Pro",
		},
		MemoryGib:                  "11.28",
		Options:                    &protocolv1.AndroidRegistrationOptions{AdbEnabled: proto.Bool(true), HandshakePqMode: protocolv1.HandshakePQMode_WA_CLASSICAL},
		RequestedPermissionsSha256: permissions,
		NativeLibraryNamesSha256:   append([]byte(nil), emptyLibraries[:]...),
		DataSoNamesSha256:          append([]byte(nil), emptyLibraries[:]...),
	}
}

func validateAndroidClient(parameters *protocolv1.AndroidClientParameters) error {
	client := parameters.GetMetadata()
	if client.GetPlatform() != protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID ||
		client.GetOsVersion() == "" || client.GetManufacturer() == "" || client.GetDevice() == "" ||
		client.GetOsBuildNumber() == "" || client.GetDeviceBoard() == "" || client.GetModelType() == "" ||
		client.GetAppVersion() != "" || client.GetPhoneId() != "" || client.GetDeviceExperimentId() != "" {
		return errors.New("invalid Android logical client parameters")
	}
	ram, err := strconv.ParseFloat(parameters.GetMemoryGib(), 64)
	if err != nil || ram <= 0 || math.IsNaN(ram) || math.IsInf(ram, 0) {
		return errors.New("invalid Android client memory")
	}
	if err := wire.ValidateHandshakeMode(parameters.GetOptions().GetHandshakePqMode()); err != nil {
		return err
	}
	for _, value := range [][]byte{parameters.GetRequestedPermissionsSha256(), parameters.GetNativeLibraryNamesSha256(), parameters.GetDataSoNamesSha256()} {
		if len(value) != sha256.Size {
			return errors.New("Android client runtime digest is required")
		}
	}
	return nil
}
