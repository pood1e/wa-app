package app

import (
	"context"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) attachClientProfileRuntime(ctx context.Context, profile *waappv1.ClientProfile) *waappv1.ClientProfile {
	if profile == nil {
		return nil
	}
	state, err := s.store.GetNativeState(ctx, profile.GetClientProfileId())
	if err != nil {
		return profile
	}
	profile.DeviceFingerprint = deviceFingerprintFromState(state)
	return profile
}

func (s *Server) attachClientProfilesRuntime(ctx context.Context, profiles []*waappv1.ClientProfile) []*waappv1.ClientProfile {
	for _, profile := range profiles {
		s.attachClientProfileRuntime(ctx, profile)
	}
	return profiles
}

func deviceFingerprintFromState(state nativeState) *waappv1.DeviceFingerprint {
	profile := normalizeNativePhoneProfile(state.Profile, "")
	normalizedState := state
	normalizedState.Profile = profile
	fields := nativeDeviceMapFields(normalizedState)
	createdAt := profile.CreatedAtUnix
	if createdAt == 0 {
		createdAt = state.CreatedAtUnix
	}
	fingerprintSource := []string{
		profile.FDID,
		profile.PhoneSHA256,
		profile.DeviceVendor,
		profile.DeviceModel,
		profile.AndroidVersion,
		fields["device_ram"],
		fields["mcc"],
		fields["mnc"],
		fields["sim_mcc"],
		fields["sim_mnc"],
		fields["network_radio_type"],
	}
	return &waappv1.DeviceFingerprint{
		FingerprintId:     "wafp_" + stableID(strings.Join(fingerprintSource, ":")),
		DeviceVendor:      profile.DeviceVendor,
		DeviceModel:       profile.DeviceModel,
		AndroidVersion:    profile.AndroidVersion,
		Fdid:              profile.FDID,
		PhoneSha256Prefix: prefixRunes(profile.PhoneSHA256, 12),
		DeviceRamGib:      fields["device_ram"],
		Mcc:               fields["mcc"],
		Mnc:               fields["mnc"],
		SimMcc:            fields["sim_mcc"],
		SimMnc:            fields["sim_mnc"],
		NetworkRadioType:  fields["network_radio_type"],
		CreatedAt:         unixTimestamp(createdAt),
	}
}

func unixTimestamp(value int64) *timestamppb.Timestamp {
	if value <= 0 {
		return nil
	}
	return timestamppb.New(time.Unix(value, 0).UTC())
}

func prefixRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}
