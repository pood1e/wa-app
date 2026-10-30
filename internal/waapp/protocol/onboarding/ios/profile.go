package ios

import (
	_ "embed"
	"errors"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

//go:embed ios-26.40.77.pb
var profileData string

// DefaultProfile returns an independent copy of the bundled iOS build material.
func DefaultProfile() (*protocolv1.IOSRegistrationProfile, error) {
	profile := new(protocolv1.IOSRegistrationProfile)
	if err := proto.Unmarshal([]byte(profileData), profile); err != nil {
		return nil, errors.New("decode bundled iOS registration profile")
	}
	if err := validateProfile(profile); err != nil {
		return nil, err
	}
	return profile, nil
}
