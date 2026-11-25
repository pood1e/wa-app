package ios

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waVnameCert"
	"google.golang.org/protobuf/proto"

	modelv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/model/v1"
	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
	"github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func TestBusinessProfilesRequireMatchingRegistrationIntent(t *testing.T) {
	profile, err := DefaultBusinessProfile()
	if err != nil {
		t.Fatal(err)
	}
	personal, err := DefaultProfile()
	if err != nil || personal.GetBuildTokenSecret() == profile.GetBuildTokenSecret() {
		t.Fatal("business registration reused the consumer token secret", err)
	}
	if _, err := Begin(t.Context(), syntheticPhone(), profile, nil, Dependencies{}); err == nil {
		t.Fatal("business flow accepted a missing business name")
	}
	personal.Business = &modelv1.BusinessRegistration{Name: "WA Lab"}
	if _, err := Begin(t.Context(), syntheticPhone(), personal, nil, Dependencies{}); err == nil {
		t.Fatal("consumer build accepted business registration")
	}
}

func TestBusinessTransferRetainsHostCertificateWithoutWorker(t *testing.T) {
	profile, err := DefaultBusinessProfile()
	if err != nil {
		t.Fatal(err)
	}
	profile.Business = &modelv1.BusinessRegistration{Name: " WA Lab "}
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var certificate, identity []byte
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if r.Header.Get("User-Agent") != profile.GetUserAgent() || r.Header.Get("Authorization") != "" || r.Form.Has("H") {
			t.Error("business request used a consumer or worker context")
			return
		}
		frame, err := base64.URLEncoding.DecodeString(r.Form.Get("ENC"))
		if err != nil {
			t.Error(err)
			return
		}
		query, err := protocoltest.OpenRegistrationFrame(frame, serverKey)
		if err != nil {
			t.Error(err)
			return
		}
		if identity == nil {
			identity, err = base64.URLEncoding.DecodeString(query.Get("e_ident"))
			if err != nil {
				t.Error(err)
				return
			}
		}
		data, err := base64.URLEncoding.DecodeString(query.Get("vname"))
		if err != nil || len(data) == 0 || (certificate != nil && !bytes.Equal(data, certificate)) {
			t.Error("business registration stage lost its bootstrap certificate")
			return
		}
		certificate = data
		requests++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/exist":
			_, _ = io.WriteString(w, `{"status":"fail","reason":"incorrect","wa_old_eligible":1,"wa_old_wait":0}`)
		case "/code":
			if query.Get("method") != "wa_old" {
				t.Error("business transfer used another delivery channel")
			}
			_, _ = io.WriteString(w, `{"status":"sent","method":"wa_old","wa_old_length":8}`)
		case "/register":
			if query.Get("method") != "wa_old" || query.Get("code") != "00123456" {
				t.Error("business transfer lost its channel or code")
			}
			_, _ = io.WriteString(w, `{"status":"ok","login":"447700900000","lid":"test-account"}`)
		}
	}))
	defer server.Close()
	flow, err := Begin(t.Context(), syntheticPhone(), profile, server.Client(), Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Close(t.Context()) })
	flow.http.baseURL, flow.http.serverKey = server.URL+"/", serverKey.PublicKey().Bytes()
	if _, err := flow.Probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.RequestCode(t.Context(), oldDeviceMethod); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.SubmitCode(t.Context(), oldDeviceMethod, "00123456"); err != nil {
		t.Fatal(err)
	}
	material, err := flow.Material("447700900000", "test-account")
	if err != nil || requests != 3 || material.GetRecovery().GetPushName() != "WA Lab" || material.GetRecovery().GetClient().GetAccountType() != modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS {
		t.Fatal("business registration lost its confirmed identity", err)
	}
	var outer waVnameCert.VerifiedNameCertificate
	var details waVnameCert.VerifiedNameCertificate_Details
	if proto.Unmarshal(certificate, &outer) != nil || proto.Unmarshal(outer.GetDetails(), &details) != nil || details.GetVerifiedName() != "" || details.VerifiedName == nil || details.GetIssuer() != "smb:wa" || details.GetSerial() > 0x00ffffffffffffff {
		t.Fatal("business certificate differs from the native schema")
	}
	valid, err := wire.XeddsaVerifyCurve25519(identity, outer.GetDetails(), outer.GetSignature())
	if err != nil || !valid || !bytes.Equal(identity, material.GetCredentials().GetIdentity().GetPublic()) {
		t.Fatal("business certificate is not bound to the host Signal identity", err)
	}
	password := flow.params.request("password", "", "sealed-password", time.Now())
	if !bytes.Contains(password.plaintext(), []byte("vname=")) {
		t.Fatal("password continuation lost its business certificate")
	}
	material.Recovery.BusinessIdentity.Certificate[0] ^= 255
	another, err := flow.Material("447700900000", "test-account")
	if err != nil || !bytes.Equal(another.GetRecovery().GetBusinessIdentity().GetCertificate(), certificate) {
		t.Fatal("handoff shares the flow's business certificate", err)
	}
	if err := flow.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	restored, err := state.Restore(another.GetCredentials(), another.GetRecovery())
	if err != nil || !bytes.Equal(restored.BusinessIdentity.GetCertificate(), certificate) || restored.Client.GetPlatform() != protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS {
		t.Fatal("business recovery lost native iOS credentials", err)
	}
}
