package onboarding

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/protocol/protocoltest"
)

func TestAndroidFlowFetchesRegistrationConfigBeforeRequests(t *testing.T) {
	routes := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := r.URL.Query().Get("_")
		routes <- route
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch route {
		case "/v2/reg_onboard_abprop":
			response = struct {
				Status     string `json:"status"`
				RetryAfter int    `json:"retry_after"`
				ExpConfig  string `json:"exp_cfg"`
			}{"ok", 3600, `[{"config_code":3753,"config_value":0},{"config_code":4435,"config_value":0}]`}
		case "/v2/exist":
			response = struct {
				Status          string   `json:"status"`
				FallbackMethods []string `json:"fallback_methods"`
				TransferWait    int      `json:"acc_tr_wait"`
				TransferAllowed bool     `json:"pref_acc_tr_eligibility"`
			}{"registered", []string{"acc_tr"}, 0, true}
		case "/v2/code":
			response = struct {
				Status string `json:"status"`
				Length int    `json:"length"`
			}{"sent", 6}
		default:
			http.Error(w, "unexpected registration route", http.StatusBadRequest)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error("write synthetic registration response")
		}
	}))
	t.Cleanup(server.Close)
	profile := protocoltest.AndroidBuild(t)
	flow, err := NewAndroidFlow(t.Context(), &waappv1.PhoneTarget{CountryCallingCode: "1", NationalNumber: "2025550123"}, profile, protocoltest.AndroidClient(), "", AndroidDependencies{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Close() })
	flow.http.DialTLSContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	probe := flow.Probe(t.Context())
	if probe.Err != nil {
		t.Fatal("synthetic probe failed")
	}
	for _, method := range probe.SupportedMethods {
		if method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER {
			t.Fatal("probe advertised an unimplemented QR migration")
		}
	}
	result := flow.RequestCode(t.Context(), waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS)
	if result.Err != nil || result.Status != waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT {
		t.Fatal("synthetic verification request failed")
	}
	if len(routes) != 3 {
		t.Fatalf("HTTP requests = %d, want one config query, one probe and one code request", len(routes))
	}
	got := []string{<-routes, <-routes, <-routes}
	if !slices.Equal(got, []string{"/v2/reg_onboard_abprop", "/v2/exist", "/v2/code"}) {
		t.Fatalf("registration request order = %v", got)
	}
	if NativeShouldSendRegistrationGPIA(flow.current) {
		t.Fatal("upstream disabled GPIA but the flow ignored the configuration")
	}
	params, err := flow.CodeRequestOrderedParams(t.Context(), flow.phone, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, flow.current, "", profile.GetAppVersion())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"gpia", "_gi", "_gg", "sim_type", "cellular_strength", "airplane_mode_type"} {
		if params.Get(key) != "" {
			t.Fatalf("upstream disabled material %s was still generated", key)
		}
	}
	if params.Get("_gp") == "" {
		t.Fatal("requested permissions digest was omitted")
	}
}

func TestAndroidRequestsDoNotSendNativeFileOrSettingNames(t *testing.T) {
	profile := protocoltest.AndroidBuild(t)
	flow, err := NewAndroidFlow(t.Context(), &waappv1.PhoneTarget{CountryCallingCode: "1", NationalNumber: "2025550123"}, profile, protocoltest.AndroidClient(), "", AndroidDependencies{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Close() })
	flow.current.Profile.AdditionalMapFields["rc2"] = "1"
	flow.current.Profile.AdditionalMapFields["airplane_mode_on"] = "1"
	for _, build := range []func() (OrderedParams, error){
		func() (OrderedParams, error) {
			return flow.CodeRequestOrderedParams(t.Context(), flow.phone, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, flow.current, "", profile.GetAppVersion())
		},
		func() (OrderedParams, error) {
			return flow.RegisterOrderedParams(t.Context(), flow.phone, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, "000000", flow.current, "", profile.GetAppVersion())
		},
	} {
		params, err := build()
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range params {
			if field.Key == "rc2" || field.Key == "airplane_mode_on" {
				t.Fatalf("native file or setting name %s became a form field", field.Key)
			}
		}
	}
}
