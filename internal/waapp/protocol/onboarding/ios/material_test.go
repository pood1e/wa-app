package ios

import (
	"bytes"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/communication"
	"github.com/pood1e/wa-app/internal/waapp/protocol/state"
)

func registeredSyntheticFlow(t *testing.T, firstRegister string) *Flow {
	t.Helper()
	var submissions atomic.Int32
	client := &http.Client{Transport: registrationTransport(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v2/exist":
			return jsonResponse(`{"status":"fail","reason":"incorrect","sms_wait":0}`), nil
		case "/v2/code":
			return jsonResponse(`{"status":"sent","method":"sms","length":6}`), nil
		default:
			if submissions.Add(1) == 1 && firstRegister != "" {
				if firstRegister == "transport_failure" {
					return nil, errors.New("synthetic transport failure")
				}
				return jsonResponse(firstRegister), nil
			}
			return jsonResponse(`{"status":"ok","login":"447700900000","lid":"test-account"}`), nil
		}
	})}
	flow, _, _ := beginSyntheticFlow(t, client)
	if _, err := flow.Material("447700900000", "test-account"); !errors.Is(err, ErrRegistrationState) {
		t.Fatal("unfinished flow exposed account material")
	}
	if _, err := flow.Probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.RequestCode(t.Context(), smsMethod); err != nil {
		t.Fatal(err)
	}
	if firstRegister != "" {
		_, err := flow.SubmitCode(t.Context(), smsMethod, "123456")
		if (err != nil) != (firstRegister == "transport_failure") || submissions.Load() != 1 {
			t.Fatal("uncertain registration was replayed or its outcome was lost")
		}
		if got := flow.OutcomeUnknown(); got != (firstRegister == "transport_failure") {
			t.Fatal("registration confused a rejected code with an unknown outcome")
		}
		_, materialErr := flow.Material("447700900000", "test-account")
		if firstRegister != "transport_failure" && !errors.Is(materialErr, ErrRegistrationState) {
			t.Fatal("explicitly rejected registration exposed recovery material")
		}
	}
	response, err := flow.SubmitCode(t.Context(), smsMethod, "123456")
	if err != nil || response.GetStatus() != "ok" || flow.OutcomeUnknown() {
		t.Fatal("explicit verification did not finish the original registration")
	}
	return flow
}

func TestFlowHandoffRetainsHostKeysAfterClosingSigner(t *testing.T) {
	flow := registeredSyntheticFlow(t, "")
	material, err := flow.Material("447700900000", "test-account")
	if err != nil {
		t.Fatal(err)
	}
	if err := communication.ValidateMaterial(material); err != nil {
		t.Fatal("iOS handoff is not valid runtime material", err)
	}
	restored, err := state.Restore(material.GetCredentials(), material.GetRecovery())
	if err != nil || restored.Client.GetPlatform() != protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS || restored.Profile.FDID != "" {
		t.Fatal("iOS recovery depends on registration device material")
	}
	encoded, err := protojson.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"buildTokenSecret", "authorization", "ios_id", "attestor"} {
		if bytes.Contains(encoded, []byte(field)) {
			t.Fatal("registration-only material reached the runtime handoff")
		}
	}
	if _, err := flow.Material("447700900001", "test-account"); err == nil {
		t.Fatal("handoff changed the confirmed phone")
	}
	if _, err := flow.Material("447700900000", "another-account"); err == nil {
		t.Fatal("handoff changed the confirmed account")
	}
	snapshot := proto.Clone(material)
	if err := flow.Close(t.Context()); err != nil || !proto.Equal(snapshot, material) {
		t.Fatal("closing the signing context destroyed the handoff keys")
	}
	if _, err := flow.Material("447700900000", "test-account"); !errors.Is(err, ErrRegistrationState) {
		t.Fatal("closed flow exposed cleared key material")
	}
}

func TestFlowExplicitVerificationResubmission(t *testing.T) {
	for _, first := range []string{"transport_failure", `{"status":"fail","reason":"incorrect"}`} {
		t.Run(first, func(t *testing.T) {
			flow := registeredSyntheticFlow(t, first)
			material, err := flow.Material("447700900000", "test-account")
			if err != nil || !bytes.Equal(material.GetCredentials().GetChatStatic().GetPrivate(), flow.keys.GetNoisePrivateKey()) {
				t.Fatal("resubmission replaced the original host identity")
			}
		})
	}
}

func TestBundledProfileOwnsItsMutableMetadata(t *testing.T) {
	first, err := DefaultProfile()
	if err != nil {
		t.Fatal(err)
	}
	want := proto.Clone(first)
	first.Client.NoiseRootPublicKey[0] ^= 255
	first.Client.Platform = protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID
	first.BuildTokenSecret = "changed"
	second, err := DefaultProfile()
	if err != nil || !proto.Equal(second, want) {
		t.Fatal("onboarding flows share mutable build metadata")
	}
}
