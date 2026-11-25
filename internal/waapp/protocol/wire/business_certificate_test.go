package wire

import (
	"testing"

	modelv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/model/v1"
	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"go.mau.fi/whatsmeow/proto/waVnameCert"
	"go.mau.fi/whatsmeow/proto/waWa6"
	"google.golang.org/protobuf/proto"
)

func TestBusinessCertificateUsesNativeSchemaAndExistingIdentity(t *testing.T) {
	identity, err := NewNativeCurveKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	private, _ := DecodeB64Any(identity.Private)
	public, _ := DecodeB64Any(identity.Public)
	data, err := NewBusinessCertificate(private, "WA Lab")
	if err != nil {
		t.Fatal(err)
	}
	var certificate waVnameCert.VerifiedNameCertificate
	var details waVnameCert.VerifiedNameCertificate_Details
	if proto.Unmarshal(data, &certificate) != nil || proto.Unmarshal(certificate.GetDetails(), &details) != nil {
		t.Fatal("certificate is incompatible with the shared native protobuf")
	}
	valid, err := XeddsaVerifyCurve25519(public, certificate.GetDetails(), certificate.GetSignature())
	if err != nil || !valid || details.GetIssuer() != "smb:wa" || details.GetVerifiedName() != "WA Lab" || details.GetSerial() == 0 || details.GetSerial() > 0x00ffffffffffffff {
		t.Fatal("certificate does not bind the business name to the existing identity")
	}
	if _, err := NewBusinessCertificate(nil, "WA Lab"); err == nil {
		t.Fatal("missing identity signed a certificate")
	}
}

func TestBusinessUserAgentKeepsMobilePrimaryPlatform(t *testing.T) {
	for _, test := range []struct {
		platform protocolv1.ClientPlatform
		kind     modelv1.WAAccountType
		want     waWa6.ClientPayload_UserAgent_Platform
	}{
		{protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID, modelv1.WAAccountType_WA_ACCOUNT_TYPE_PERSONAL, waWa6.ClientPayload_UserAgent_ANDROID},
		{protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS, modelv1.WAAccountType_WA_ACCOUNT_TYPE_PERSONAL, waWa6.ClientPayload_UserAgent_IOS},
		{protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID, modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS, waWa6.ClientPayload_UserAgent_SMB_ANDROID},
		{protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS, modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS, waWa6.ClientPayload_UserAgent_SMB_IOS},
	} {
		t.Run(test.want.String(), func(t *testing.T) {
			var ua waWa6.ClientPayload_UserAgent
			if err := proto.Unmarshal(BuildUserAgentPayload(UserAgentConfig{Platform: test.platform, AccountType: test.kind, Version: "2.26.42.74"}), &ua); err != nil || ua.GetPlatform() != test.want {
				t.Fatalf("platform=%v, want %v: %v", ua.GetPlatform(), test.want, err)
			}
		})
	}
}
