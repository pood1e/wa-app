package communication

import (
	"crypto/rand"
	"encoding/binary"
	"io"

	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func DefaultLoginPayload(identity pwire.LoginIdentity, state pstate.NativeState, version string) []byte {
	return LoginPayload(identity, state, version, false, false)
}

func PassiveLoginCheckPayload(identity pwire.LoginIdentity, state pstate.NativeState, version string) []byte {
	return LoginPayload(identity, state, version, true, true)
}

func LoginPayload(identity pwire.LoginIdentity, state pstate.NativeState, version string, passive bool, shortConnect bool) []byte {
	ua := ChatdUserAgentForState(state, version)
	buf := make([]byte, 8)
	_, _ = io.ReadFull(rand.Reader, buf)
	sessionID := binary.BigEndian.Uint64(buf)&0x7fffffff + 1
	cfg := pwire.LoginPayloadConfig{SessionID: sessionID, Passive: passive, ShortConnect: shortConnect, ConnectType: 1, ConnectReason: 1, Product: 0, Oc: true, Lc: 0}
	return pwire.BuildLoginPayload(identity, ua, cfg)
}

func ChatdUserAgentForState(current pstate.NativeState, version string) pwire.UserAgentConfig {
	p := current.Client
	return pwire.UserAgentConfig{
		Platform: p.GetPlatform(),
		Version:  shared.FirstNonEmpty(p.GetAppVersion(), version), OsVersion: p.GetOsVersion(), Manufacturer: p.GetManufacturer(), Device: p.GetDevice(),
		OsBuildNumber: p.GetOsBuildNumber(), PhoneID: p.GetPhoneId(), LocaleLang: p.GetLocaleLanguage(), LocaleCountry: p.GetLocaleCountry(),
		DeviceBoard: p.GetDeviceBoard(), DeviceExpID: p.GetDeviceExperimentId(), ModelType: p.GetModelType(), Mcc: p.GetMcc(), Mnc: p.GetMnc(),
	}
}
