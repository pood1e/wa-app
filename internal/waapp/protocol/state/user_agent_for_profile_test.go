package state

import "testing"

func TestAndroidUserAgentMatchesNativeTokenEncoding(t *testing.T) {
	profile := NativePhoneProfile{AndroidVersion: "17 beta", DeviceVendor: "Acme/Co", DeviceModel: "A(1), x-y.z:2\r\n"}
	want := "WhatsApp/2.26.40.76 Android/17_beta Device/Acme_Co-A(1),_x-y.z_2__"
	if got := NativeUserAgentForProfile(profile, "2.26.40.76"); got != want {
		t.Fatalf("user agent = %q, want %q", got, want)
	}
	if profile.DeviceModel != "A(1), x-y.z:2\r\n" {
		t.Fatal("HTTP token encoding changed the source model")
	}
}
