package wamodel

import (
	"net/url"
	"strings"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/proto"
)

func TestAccountProxyURL(t *testing.T) {
	for _, tc := range []struct{ host, normalized string }{
		{" Proxy.Example ", "proxy.example"}, {"[2001:db8::1]", "2001:db8::1"}, {"例子.example", "xn--fsqu00a.example"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			value := &waappv1.AccountProxy{Protocol: waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_SOCKS5, Host: tc.host, Port: 1080, Username: "fixture@user", Password: &waappv1.SensitiveText{Value: "fixture:/@?password"}}
			before := proto.CloneOf(value)
			address, err := AccountProxyURL(value)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := url.Parse(address)
			if err != nil {
				t.Fatal(err)
			}
			password, _ := parsed.User.Password()
			if parsed.Scheme != "socks5" || parsed.Hostname() != tc.normalized || parsed.Port() != "1080" || parsed.User.Username() != value.Username || password != value.Password.Value || !proto.Equal(before, value) {
				t.Fatal("proxy normalization changed credentials, address, or caller input")
			}
		})
	}
	if address, err := AccountProxyURL(nil); err != nil || address != "" {
		t.Fatal("absent override did not remain absent")
	}
}

func TestInvalidAccountProxy(t *testing.T) {
	for _, change := range []func(*waappv1.AccountProxy){
		func(p *waappv1.AccountProxy) {
			p.Protocol = waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_UNSPECIFIED
		},
		func(p *waappv1.AccountProxy) { p.Host = "http://fixture-secret@example.com" },
		func(p *waappv1.AccountProxy) { p.Host = "[::1" },
		func(p *waappv1.AccountProxy) { p.Host = "bad host" },
		func(p *waappv1.AccountProxy) { p.Host = "proxy.example:8080" },
		func(p *waappv1.AccountProxy) { p.Port = 65536 },
		func(p *waappv1.AccountProxy) { p.Port = 0 },
		func(p *waappv1.AccountProxy) { p.Username = "bad:name" },
		func(p *waappv1.AccountProxy) { p.Username = "" },
		func(p *waappv1.AccountProxy) { p.Password.RedactedValue = "********" },
		func(p *waappv1.AccountProxy) { p.Password.Value = "fixture-secret\r\n" },
	} {
		value := &waappv1.AccountProxy{Protocol: waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_HTTP, Host: "proxy.example", Port: 8080, Username: "fixture-user", Password: &waappv1.SensitiveText{Value: "fixture-secret"}}
		change(value)
		if _, err := AccountProxyURL(value); err == nil || strings.Contains(err.Error(), "fixture-secret") {
			t.Fatal("invalid proxy accepted or credentials exposed in validation error")
		}
	}
}
