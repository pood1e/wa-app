package wire

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestRegistrationRoutingHeaderIsLimitedToGatewayRequests(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint string
		forwarded      bool
	}{
		{"direct", "https://v.whatsapp.net/v2/code", false},
		{"gateway", DefaultWACodeURL, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if (r.Header.Get("X-Forwarded-Host") == DefaultNativeHTTPForwardedHost) != tc.forwarded {
					t.Error("forwarded host does not match the selected route")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			client := &NativeHTTPClient{DialTLSContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}}
			endpoint, err := url.Parse(tc.endpoint)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.PostOrderedForm(t.Context(), endpoint, "ENC=synthetic&H=synthetic", "synthetic", "")
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
		})
	}
}

func TestRegistrationEndpointKindRecognizesDirectAndGatewayPaths(t *testing.T) {
	for _, tc := range []struct{ endpoint, want string }{
		{"https://v.whatsapp.net/v2/exist", "exist"},
		{"https://v.whatsapp.net/v2/code", "code"},
		{"https://v.whatsapp.net/v2/register", "register"},
		{DefaultWACodeURL, "code"},
		{"https://fixture.invalid/unrelated", ""},
	} {
		endpoint, err := url.Parse(tc.endpoint)
		if err != nil {
			t.Fatal(err)
		}
		if got := NativeRegistrationEndpointKind(endpoint); got != tc.want {
			t.Errorf("endpoint kind = %q, want %q", got, tc.want)
		}
	}
}
