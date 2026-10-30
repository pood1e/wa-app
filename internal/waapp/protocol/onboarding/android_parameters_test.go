package onboarding

import (
	"encoding/base64"
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
)

func TestAndroidRegistrationRequestPrefixes(t *testing.T) {
	phone := &waappv1.PhoneTarget{E164Number: "+12025550123", CountryCallingCode: "1", NationalNumber: "2025550123", CountryIso2: "US"}
	profile := protocoltest.AndroidBuild(t)
	parameters := protocoltest.AndroidClient()
	parameters.Options = &protocolv1.AndroidRegistrationOptions{ClickedEducationLink: proto.Bool(false), ManageCallPermission: proto.Bool(false),
		ClientStartMessage: []byte{0xfb, 0xff}, AdbEnabled: proto.Bool(false), EnablePqRegistration: true}
	flow, err := NewAndroidFlow(t.Context(), phone, profile, parameters, "", AndroidDependencies{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Close() })
	flow.current.PreChatdAB.ExpConfig = `[{"config_code":7999,"config_value":1}]`
	flow.verification = &protocolv1.AndroidVerificationContext{Login: "synthetic-login", AuthResponse: "synthetic-response"}
	for k, v := range map[string]string{"create_paa": "true", "entrypoint": "synthetic-entry", "suma": "false", "cred_token": "synthetic-credential", "db": "1"} {
		flow.current.Profile.AdditionalMapFields[k] = v
	}
	base := []string{"cc", "in", "lg", "lc", "fdid", "expid", "access_session_id", "id", "backup_token"}
	e2e := []string{"authkey", "e_ident", "e_keytype", "e_regid", "e_skey_id", "e_skey_val", "e_skey_sig", "e_pq_last_resort_id", "e_pq_last_resort_val", "e_pq_last_resort_sig"}
	for _, test := range []struct {
		name   string
		build  func() (OrderedParams, error)
		prefix []string
	}{
		{"exist", func() (OrderedParams, error) {
			return flow.ExistOrderedParamsWithWamsys(t.Context(), phone, flow.current, nil, false, profile.GetAppVersion())
		}, slices.Concat(base, []string{"advertising_id", "login", "type"}, e2e, []string{"token"})},
		{"code", func() (OrderedParams, error) {
			return flow.CodeRequestOrderedParamsWithWamsys(t.Context(), phone, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, flow.current, "synthetic-context", nil, false, profile.GetAppVersion())
		}, slices.Concat(base, []string{"token", "method", "context", "clicked_education_link", "manage_call_permission", "client_start_message", "advertising_id", "login", "type"}, e2e)},
		{"register", func() (OrderedParams, error) {
			return flow.RegisterOrderedParamsWithWamsys(t.Context(), phone, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, "000000", flow.current, "synthetic-context", nil, false, profile.GetAppVersion())
		}, slices.Concat(base, []string{"code", "auth_response", "context", "method", "advertising_id", "login", "type"}, e2e)},
	} {
		t.Run(test.name, func(t *testing.T) {
			params, err := test.build()
			if err != nil {
				t.Fatal(err)
			}
			keys := make([]string, len(params))
			for i, field := range params {
				keys[i] = field.Key
			}
			if len(keys) < len(test.prefix) || !slices.Equal(keys[:len(test.prefix)], test.prefix) {
				t.Fatalf("%s field prefix = %v, want %v", test.name, keys, test.prefix)
			}
			if params.Get("db") != "0" || slices.Contains(keys, "call_log_permission") {
				t.Fatal("optional device observations were invented or overridden")
			}
			if test.name == "code" && params.Get("client_start_message") != "-_8" {
				t.Fatal("client start message is not raw Base64URL")
			}
			if test.name == "register" && slices.Contains(keys, "token") {
				t.Fatal("register request includes a build token")
			}
			for name, length := range map[string]int{"e_pq_last_resort_id": 3, "e_pq_last_resort_val": 1568, "e_pq_last_resort_sig": 64} {
				raw, err := base64.RawURLEncoding.DecodeString(params.Get(name))
				if err != nil || len(raw) != length {
					t.Fatalf("%s encoding or length differs from the native bundle", name)
				}
			}
			if i, j := slices.Index(keys, "db"), slices.Index(keys, "recaptcha"); i < 0 || j < i {
				t.Fatal("ADB state moved after the registration tail")
			}
		})
	}
	// The flow owns a copy; caller changes cannot replace its token or request gates.
	parameters.Options.AdbEnabled = proto.Bool(true)
	clear(profile.TokenHmacKey)
	if flow.parameters.GetOptions().GetAdbEnabled() || flow.build.GetTokenHmacKey()[0] == 0 {
		t.Fatal("registration flow retained caller-owned profile buffers")
	}
}
