package onboarding

import (
	"bytes"
	"strings"
	"testing"

	modelv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/model/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"go.mau.fi/whatsmeow/proto/waVnameCert"
	"google.golang.org/protobuf/proto"
)

func TestBusinessRegistrationBindsNameToItsFlowIdentity(t *testing.T) {
	build, err := DefaultBusinessAndroidBuild()
	if err != nil {
		t.Fatal(err)
	}
	parameters := DefaultBusinessAndroidClient()
	parameters.Business = &modelv1.BusinessRegistration{Name: "WA Lab"}
	phone := &waappv1.PhoneTarget{E164Number: "+12025550123", CountryCallingCode: "1", NationalNumber: "2025550123", CountryIso2: "US"}
	flow, err := NewAndroidFlow(t.Context(), phone, build, parameters, "", AndroidDependencies{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Close() })
	if build.GetAppVersion() != "2.26.45.83" || build.GetAccountType() != modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS || flow.current.Client.GetAccountType() != modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS || !strings.HasPrefix(state.NativeUserAgentForState(flow.current, build.GetAppVersion()), "WhatsApp/2.26.45.83 SMBA/") {
		t.Fatal("business registration did not select its bundled native application")
	}
	request, err := flow.RegisterOrderedParamsWithWamsys(t.Context(), phone, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD, "012345", flow.current, "", nil, false, build.GetAppVersion())
	if err != nil {
		t.Fatal(err)
	}
	data, err := wire.DecodeB64Any(request.ToMap()["vname"])
	if err != nil || !bytes.Equal(data, flow.current.BusinessIdentity.GetCertificate()) {
		t.Fatal("registration changed or omitted the native vname certificate")
	}
	var certificate waVnameCert.VerifiedNameCertificate
	var details waVnameCert.VerifiedNameCertificate_Details
	if proto.Unmarshal(data, &certificate) != nil || proto.Unmarshal(certificate.GetDetails(), &details) != nil || details.GetVerifiedName() != "WA Lab" {
		t.Fatal("business registration lost the requested name")
	}
	public, _ := flow.current.Signal.Identity.PublicBytes()
	valid, err := wire.XeddsaVerifyCurve25519(public, certificate.GetDetails(), certificate.GetSignature())
	if err != nil || !valid {
		t.Fatal("business name certificate belongs to another account identity")
	}
	material, err := flow.Material("12025550123", "synthetic-business-account")
	if err != nil || !proto.Equal(material.GetRecovery().GetBusinessIdentity(), flow.current.BusinessIdentity) || material.GetRecovery().GetPushName() != "WA Lab" {
		t.Fatal("business name or certificate was lost at account handoff", err)
	}
	if parameters.GetMetadata().GetAppVersion() != "" || parameters.GetMetadata().GetAccountType() != modelv1.WAAccountType_WA_ACCOUNT_TYPE_PERSONAL {
		t.Fatal("business registration mutated shared client parameters")
	}
}
