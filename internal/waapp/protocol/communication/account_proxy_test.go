package communication

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type proxyRoutingRepository struct {
	StateRepository
	proxies map[string]*waappv1.AccountProxy
}

func (r proxyRoutingRepository) GetProfileProxy(_ context.Context, profile string) (*waappv1.AccountProxy, error) {
	if profile == "lookup-error" {
		return nil, errors.New("injected proxy lookup failure")
	}
	return r.proxies[profile], nil
}

func TestAccountProxyRoutesHTTPAndTLS(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "tls"}[encrypted], func(t *testing.T) {
			target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "routed") }))
			if encrypted {
				target.StartTLS()
			} else {
				target.Start()
			}
			t.Cleanup(target.Close)
			fallback, fallbackHits := fixtureHTTPProxy(t, target.Listener.Addr().String(), false)
			httpProxy, httpHits := fixtureHTTPProxy(t, target.Listener.Addr().String(), true)
			socksProxy, socksHits := fixtureSOCKSProxy(t, target.Listener.Addr().String())
			repository := proxyRoutingRepository{proxies: map[string]*waappv1.AccountProxy{"http": httpProxy, "socks": socksProxy}}
			engine, err := New(repository, "http://"+net.JoinHostPort(fallback.Host, strconv.Itoa(int(fallback.Port))), nil, nil, WithProxyResolver(repository))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(engine.CloseIdleConnections)
			endpoint, _ := url.Parse(target.URL)
			endpoint.Host = net.JoinHostPort("proxy-destination.test", endpoint.Port())
			for _, profile := range []string{"http", "socks", "default"} {
				client, finish, err := engine.HTTPForProfile(t.Context(), profile)
				if err != nil {
					t.Fatal(err)
				}
				response, err := client.Client.Get(endpoint.String())
				if err != nil {
					finish()
					t.Fatalf("%s routing failed: %v", profile, err)
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				finish()
				if err != nil || string(body) != "routed" {
					t.Fatal("proxy did not reach the fixture origin")
				}
			}
			if fallbackHits.Load() != 1 || httpHits.Load() != 1 || socksHits.Load() != 1 {
				t.Fatal("account proxy routing crossed account boundaries")
			}
			if _, _, err := engine.HTTPForProfile(t.Context(), "lookup-error"); err == nil {
				t.Fatal("lookup failure fell back to global egress")
			}
		})
	}
}

func TestAccountProxyFailureDoesNotReachOrigin(t *testing.T) {
	var reached atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached.Add(1) }))
	defer origin.Close()
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unavailable", http.StatusBadGateway) }))
	defer failed.Close()
	repository := proxyRoutingRepository{proxies: map[string]*waappv1.AccountProxy{"failed": proxyAt(t, failed.Listener.Addr().String(), waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_HTTP)}}
	engine, err := New(repository, "", nil, nil, WithProxyResolver(repository))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.CloseIdleConnections()
	client, finish, err := engine.HTTPForProfile(t.Context(), "failed")
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	if response, err := client.Client.Get(origin.URL); err == nil {
		_ = response.Body.Close()
		t.Fatal("failed proxy silently used direct egress")
	}
	if reached.Load() != 0 {
		t.Fatal("failed proxy leaked a request to the direct origin")
	}
}

func fixtureHTTPProxy(t *testing.T, target string, authenticated bool) (*waappv1.AccountProxy, *atomic.Int32) {
	t.Helper()
	hits := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		expected := ""
		if authenticated {
			expected = "Basic " + base64.StdEncoding.EncodeToString([]byte("fixture-user:fixture-password"))
		}
		if request.Header.Get("Proxy-Authorization") != expected {
			t.Error("HTTP proxy authentication was missing or crossed account boundaries")
			http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
			return
		}
		if request.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", http.StatusBadRequest)
			return
		}
		upstream, err := net.Dial("tcp", target)
		if err != nil {
			http.Error(w, "fixture origin unavailable", http.StatusBadGateway)
			return
		}
		client, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = upstream.Close()
			return
		}
		hits.Add(1)
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
		_ = buffered.Flush()
		relayProxy(client, upstream)
	}))
	t.Cleanup(server.Close)
	value := proxyAt(t, server.Listener.Addr().String(), waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_HTTP)
	if authenticated {
		value.Username = "fixture-user"
		value.Password = &waappv1.SensitiveText{Value: "fixture-password"}
	}
	return value, hits
}

func proxyAt(t *testing.T, address string, protocol waappv1.AccountProxyProtocol) *waappv1.AccountProxy {
	t.Helper()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	return &waappv1.AccountProxy{Protocol: protocol, Host: host, Port: uint32(number)}
}

func fixtureSOCKSProxy(t *testing.T, target string) (*waappv1.AccountProxy, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hits := new(atomic.Int32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			go func() { defer client.Close(); serveSOCKSFixture(t, client, target, hits) }()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-done })
	value := proxyAt(t, listener.Addr().String(), waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_SOCKS5)
	value.Username = "fixture-user"
	value.Password = &waappv1.SensitiveText{Value: "fixture-password"}
	return value, hits
}

func serveSOCKSFixture(t *testing.T, client net.Conn, target string, hits *atomic.Int32) {
	t.Helper()
	reader := bufio.NewReader(client)
	var header [2]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}
	_, _ = client.Write([]byte{5, 2})
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return
	}
	username := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, username); err != nil {
		return
	}
	length, err := reader.ReadByte()
	if err != nil {
		return
	}
	password := make([]byte, int(length))
	if _, err := io.ReadFull(reader, password); err != nil {
		return
	}
	if string(username) != "fixture-user" || string(password) != "fixture-password" {
		t.Error("SOCKS credentials changed in transit")
		return
	}
	_, _ = client.Write([]byte{1, 0})
	var request [4]byte
	if _, err := io.ReadFull(reader, request[:]); err != nil {
		return
	}
	if request != [4]byte{5, 1, 0, 3} {
		t.Error("SOCKS request did not preserve remote DNS")
		return
	}
	length, err = reader.ReadByte()
	if err != nil {
		return
	}
	address := make([]byte, int(length)+2)
	if _, err := io.ReadFull(reader, address); err != nil {
		return
	}
	if string(address[:len(address)-2]) != "proxy-destination.test" {
		t.Error("SOCKS destination changed")
		return
	}
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	hits.Add(1)
	_, _ = client.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
	relayProxy(client, upstream)
}

func relayProxy(client, upstream net.Conn) {
	defer client.Close()
	defer upstream.Close()
	done := make(chan struct{})
	go func() { defer close(done); _, _ = io.Copy(upstream, client); _ = upstream.Close() }()
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
	<-done
}
