package communication

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
)

func (c *ChatdClient) Dial(ctx context.Context, endpoint ChatdEndpoint) (net.Conn, error) {
	address := endpoint.Address()
	var conn net.Conn
	var err error
	if strings.TrimSpace(c.Cfg.ProxyURL) != "" {
		conn, err = c.DialProxy(ctx, endpoint)
	} else {
		dialer := c.NetDialer()
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, err
	}
	if !c.Cfg.TLS {
		return conn, nil
	}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: endpoint.Host, InsecureSkipVerify: c.Cfg.InsecureTLS})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func (c *ChatdClient) DialProxy(ctx context.Context, endpoint ChatdEndpoint) (net.Conn, error) {
	parsed, err := pwire.ParseOutboundProxyURL(c.Cfg.ProxyURL)
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(parsed.Scheme, "socks5"):
		return c.DialSOCKS5(ctx, parsed, endpoint)
	case parsed.Scheme == "http" || parsed.Scheme == "https":
		return c.DialHTTPConnect(ctx, parsed, endpoint.Address())
	default:
		return nil, fmt.Errorf("unsupported chatd proxy scheme %q", parsed.Scheme)
	}
}

func (c *ChatdClient) DialHTTPConnect(ctx context.Context, parsed *url.URL, target string) (net.Conn, error) {
	if parsed.Host == "" {
		return nil, fmt.Errorf("proxy host is required")
	}
	dialer := c.NetDialer()
	conn, err := dialer.DialContext(ctx, "tcp", parsed.Host)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: parsed.Hostname(), InsecureSkipVerify: c.Cfg.InsecureTLS})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		conn = tlsConn
	}
	_ = conn.SetDeadline(time.Now().Add(c.OpenTimeout()))
	headers := []string{"CONNECT " + target + " HTTP/1.1", "Host: " + target, "Proxy-Connection: keep-alive", "User-Agent: WhatsApp-GoChatd/1"}
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		credential := parsed.User.Username() + ":" + password
		headers = append(headers, "Proxy-Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte(credential)))
	}
	if _, err := conn.Write([]byte(strings.Join(headers, "\r\n") + "\r\n\r\n")); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if !regexp.MustCompile(`^HTTP/\d(?:\.\d)?\s+2\d\d\b`).MatchString(strings.TrimSpace(statusLine)) {
		_ = conn.Close()
		return nil, fmt.Errorf("HTTP CONNECT proxy failed: %s", strings.TrimSpace(statusLine))
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
	return &pwire.BufferedConn{Conn: conn, Reader: reader}, nil
}

func (c *ChatdClient) DialSOCKS5(ctx context.Context, parsed *url.URL, endpoint ChatdEndpoint) (net.Conn, error) {
	if parsed.Host == "" {
		return nil, fmt.Errorf("SOCKS5 proxy host is required")
	}
	dialer := c.NetDialer()
	conn, err := dialer.DialContext(ctx, "tcp", parsed.Host)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(c.OpenTimeout()))
	methods := []byte{0x00}
	if parsed.User != nil {
		methods = append(methods, 0x02)
	}
	if _, err := conn.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if resp[0] != 0x05 || resp[1] == 0xff {
		_ = conn.Close()
		return nil, fmt.Errorf("SOCKS5 proxy rejected authentication methods")
	}
	if resp[1] == 0x02 {
		password, _ := parsed.User.Password()
		username := parsed.User.Username()
		if len(username) > 255 || len(password) > 255 {
			_ = conn.Close()
			return nil, fmt.Errorf("SOCKS5 credentials too long")
		}
		msg := []byte{0x01, byte(len(username))}
		msg = append(msg, username...)
		msg = append(msg, byte(len(password)))
		msg = append(msg, password...)
		if _, err := conn.Write(msg); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if _, err := io.ReadFull(conn, resp); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if resp[1] != 0x00 {
			_ = conn.Close()
			return nil, fmt.Errorf("SOCKS5 username/password authentication failed")
		}
	}
	hostBytes := []byte(endpoint.Host)
	request := []byte{0x05, 0x01, 0x00, 0x03, byte(len(hostBytes))}
	request = append(request, hostBytes...)
	request = append(request, byte(endpoint.Port>>8), byte(endpoint.Port))
	if _, err := conn.Write(request); err != nil {
		_ = conn.Close()
		return nil, err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if head[0] != 0x05 || head[1] != 0x00 {
		_ = conn.Close()
		return nil, fmt.Errorf("SOCKS5 connect failed: %x", head)
	}
	switch head[3] {
	case 0x01:
		_, err = io.CopyN(io.Discard, conn, 4)
	case 0x03:
		var ln [1]byte
		if _, err = io.ReadFull(conn, ln[:]); err == nil {
			_, err = io.CopyN(io.Discard, conn, int64(ln[0]))
		}
	case 0x04:
		_, err = io.CopyN(io.Discard, conn, 16)
	default:
		err = fmt.Errorf("SOCKS5 invalid address type %d", head[3])
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_, err = io.CopyN(io.Discard, conn, 2)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (c *ChatdClient) NetDialer() net.Dialer {
	return net.Dialer{Timeout: c.OpenTimeout(), KeepAlive: DefaultChatdKeepAlive}
}

func ResolveLoginIdentity(registeredIdentityID string, state pstate.NativeState) (pwire.LoginIdentity, error) {
	candidates := []string{state.RegistrationJID, registeredIdentityID, state.CC + state.Phone}
	var lastJID string
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		jid := pstate.NormalizeJID(candidate)
		lastJID = jid
		user := strings.SplitN(strings.SplitN(jid, "@", 2)[0], ":", 2)[0]
		username, err := strconv.ParseUint(user, 10, 64)
		if err == nil {
			return pwire.LoginIdentity{Jid: jid, Username: username}, nil
		}
	}
	return pwire.LoginIdentity{}, fmt.Errorf("cannot derive numeric chatd username from %q", lastJID)
}

func InboundKind(tag string) waappv1.InboundMessageKind {
	switch tag {
	case "message":
		return waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE
	case "notification":
		return waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_NOTIFICATION
	case "receipt":
		return waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_RECEIPT
	default:
		return waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_SYSTEM
	}
}

func AckStatusForNode(node pwire.ChatdNode) waappv1.MessageAckStatus {
	if _, ok := pwire.BuildAckForNode(node); ok {
		return waappv1.MessageAckStatus_MESSAGE_ACK_STATUS_ACKED
	}
	return waappv1.MessageAckStatus_MESSAGE_ACK_STATUS_NOT_REQUIRED
}
