package wire

import (
	"bufio"
	"context"
	stdtls "crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	xproxy "golang.org/x/net/proxy"
)

const (
	DefaultWASafeServerPublicKeyHex = "8e8c0f74c3ebc5d7a6865c6c3c843856b06121cce8ea774d22fb6f122512302d"
	DefaultNativeHTTPForwardedHost  = "v.whatsapp.net"
)

type NativeHTTPClient struct {
	Client         *http.Client
	Attestor       RegistrationAttestor
	DialTLSContext func(context.Context, string, string) (net.Conn, error)
	Timeout        time.Duration
}

func (c *NativeHTTPClient) CloseIdleConnections() {
	if c == nil || c.Client == nil {
		return
	}
	c.Client.CloseIdleConnections()
}

func NewNativeHTTPClient(proxy string) (*NativeHTTPClient, error) {
	dialer := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 20 * time.Second}
	dialTLSContext := NativeAndroidDialTLSContext(dialer.DialContext)
	transport := &http.Transport{
		ForceAttemptHTTP2: false,
		TLSClientConfig:   &stdtls.Config{InsecureSkipVerify: true},
		DialContext:       dialer.DialContext,
		DialTLSContext:    dialTLSContext,
	}
	if proxy != "" {
		parsed, err := ParseOutboundProxyURL(proxy)
		if err != nil {
			return nil, err
		}
		if err := ConfigureNativeHTTPProxy(transport, parsed, dialer); err != nil {
			return nil, err
		}
		proxyDialTLSContext, err := NativeProxyDialTLSContext(parsed, dialer)
		if err != nil {
			return nil, err
		}
		dialTLSContext = proxyDialTLSContext
	}
	return &NativeHTTPClient{Client: &http.Client{Timeout: 20 * time.Second, Transport: transport}, DialTLSContext: dialTLSContext, Timeout: 20 * time.Second}, nil
}

func ConfigureNativeHTTPProxy(transport *http.Transport, parsed *url.URL, dialer *net.Dialer) error {
	if transport == nil || parsed == nil {
		return nil
	}
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 20 * time.Second}
	}
	switch {
	case parsed.Scheme == "http" || parsed.Scheme == "https":
		transport.Proxy = nil
		transport.DialContext = NativeHTTPProxyConnectDialContext(dialer.DialContext, parsed)
		transport.DialTLSContext = NativeAndroidDialTLSContext(NativeHTTPProxyConnectDialContext(dialer.DialContext, parsed))
	case strings.HasPrefix(parsed.Scheme, "socks5"):
		var auth *xproxy.Auth
		if parsed.User != nil {
			password, _ := parsed.User.Password()
			auth = &xproxy.Auth{User: parsed.User.Username(), Password: password}
		}
		proxyDialer, err := xproxy.SOCKS5("tcp", parsed.Host, auth, dialer)
		if err != nil {
			return err
		}
		contextDialer, ok := proxyDialer.(xproxy.ContextDialer)
		if !ok {
			return fmt.Errorf("SOCKS5 proxy dialer does not support context")
		}
		transport.DialContext = contextDialer.DialContext
		transport.DialTLSContext = NativeAndroidDialTLSContext(contextDialer.DialContext)
	default:
		return fmt.Errorf("unsupported HTTP proxy scheme")
	}
	return nil
}

func NativeProxyDialTLSContext(parsed *url.URL, dialer *net.Dialer) (func(context.Context, string, string) (net.Conn, error), error) {
	if parsed == nil {
		return nil, fmt.Errorf("proxy URL is required")
	}
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 20 * time.Second}
	}
	switch {
	case parsed.Scheme == "http" || parsed.Scheme == "https":
		return NativeAndroidDialTLSContext(NativeHTTPProxyConnectDialContext(dialer.DialContext, parsed)), nil
	case strings.HasPrefix(parsed.Scheme, "socks5"):
		var auth *xproxy.Auth
		if parsed.User != nil {
			password, _ := parsed.User.Password()
			auth = &xproxy.Auth{User: parsed.User.Username(), Password: password}
		}
		proxyDialer, err := xproxy.SOCKS5("tcp", parsed.Host, auth, dialer)
		if err != nil {
			return nil, err
		}
		contextDialer, ok := proxyDialer.(xproxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("SOCKS5 proxy dialer does not support context")
		}
		return NativeAndroidDialTLSContext(contextDialer.DialContext), nil
	default:
		return nil, fmt.Errorf("unsupported HTTP proxy scheme")
	}
}

func NativeHTTPProxyConnectDialContext(dialContext func(context.Context, string, string) (net.Conn, error), parsed *url.URL) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network string, addr string) (net.Conn, error) {
		if dialContext == nil {
			return nil, fmt.Errorf("native proxy dialer is not configured")
		}
		if parsed == nil || parsed.Host == "" {
			return nil, fmt.Errorf("HTTP proxy host is required")
		}
		proxyAddress := NativeProxyAddress(parsed)
		conn, err := dialContext(ctx, network, proxyAddress)
		if err != nil {
			return nil, err
		}
		if parsed.Scheme == "https" {
			tlsConn := stdtls.Client(conn, &stdtls.Config{ServerName: parsed.Hostname(), InsecureSkipVerify: true})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
				return nil, err
			}
			conn = tlsConn
		}
		_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
		if err := WriteNativeHTTPConnect(conn, parsed, addr); err != nil {
			_ = conn.Close()
			return nil, err
		}
		reader := bufio.NewReader(conn)
		statusLine, err := reader.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		statusLine = strings.TrimSpace(statusLine)
		if !NativeHTTPConnectStatusOK(statusLine) {
			_ = conn.Close()
			return nil, fmt.Errorf("HTTP CONNECT proxy failed")
		}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				_ = conn.Close()
				return nil, err
			}
			if line == "\r\n" || line == "\n" {
				break
			}
		}
		_ = conn.SetDeadline(time.Time{})
		return &BufferedConn{Conn: conn, Reader: reader}, nil
	}
}

func NativeProxyAddress(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	if _, _, err := net.SplitHostPort(parsed.Host); err == nil {
		return parsed.Host
	}
	port := "80"
	if parsed.Scheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(parsed.Hostname(), port)
}

func WriteNativeHTTPConnect(conn net.Conn, parsed *url.URL, target string) error {
	headers := []string{
		"CONNECT " + target + " HTTP/1.1",
		"Host: " + target,
		"Proxy-Connection: Keep-Alive",
	}
	if parsed != nil && parsed.User != nil {
		password, _ := parsed.User.Password()
		credential := parsed.User.Username() + ":" + password
		headers = append(headers, "Proxy-Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte(credential)))
	}
	_, err := conn.Write([]byte(strings.Join(headers, "\r\n") + "\r\n\r\n"))
	return err
}

func NativeHTTPConnectStatusOK(statusLine string) bool {
	parts := strings.Fields(statusLine)
	return len(parts) >= 2 && len(parts[1]) == 3 && parts[1][0] == '2'
}

func NativeAndroidDialTLSContext(dialContext func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network string, addr string) (net.Conn, error) {
		if dialContext == nil {
			return nil, fmt.Errorf("native TLS dialer is not configured")
		}
		rawConn, err := dialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			_ = rawConn.Close()
			return nil, err
		}
		tlsConn := utls.UClient(rawConn, &utls.Config{ServerName: host, InsecureSkipVerify: true, NextProtos: []string{"http/1.1"}}, utls.HelloCustom)
		if err := tlsConn.ApplyPreset(NativeAndroidOkHTTPClientHelloSpec()); err != nil {
			_ = rawConn.Close()
			return nil, err
		}
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return nil, err
		}
		return tlsConn, nil
	}
}
