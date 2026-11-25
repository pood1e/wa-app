package onboarding

import (
	_ "embed"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

//go:embed android-business-2.26.45.83.pb
var androidBusinessBuildData string

func DefaultBusinessAndroidBuild() (*protocolv1.AndroidBuildMaterial, error) {
	return decodeAndroidBuild(androidBusinessBuildData)
}

func DefaultBusinessAndroidClient() *protocolv1.AndroidClientParameters {
	parameters := DefaultAndroidClient()
	parameters.RequestedPermissionsSha256 = []byte{
		0x24, 0xe4, 0x03, 0xdf, 0xe2, 0x9d, 0x8b, 0x21,
		0xee, 0x50, 0xff, 0x1b, 0x9a, 0x11, 0xbf, 0xa3,
		0x7e, 0xe3, 0xdf, 0x30, 0x40, 0xf3, 0x74, 0x2a,
		0x01, 0xe5, 0x7c, 0xef, 0x59, 0xbf, 0xfb, 0x39,
	}
	return parameters
}
