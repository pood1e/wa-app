package app

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

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultChatdHost                   = "g.whatsapp.net"
	defaultChatdPort                   = 443
	defaultChatdMaxFrame               = 4 << 20
	defaultChatdReadWindow             = 20 * time.Second
	defaultChatdKeepAlive              = 30 * time.Second
	defaultChatdPostMessageDrainWindow = 250 * time.Millisecond
)

type chatdClientConfig struct {
	Host          string
	Port          int
	Endpoints     []chatdEndpoint
	TLS           bool
	RoutingInfo   string
	ProxyURL      string
	InsecureTLS   bool
	Timeout       time.Duration
	OpenTimeout   time.Duration
	MaxFrameBytes int
	MaxEndpoints  int
}

type chatdClient struct {
	cfg   chatdClientConfig
	codec *binaryNodeCodec
}

type chatdEndpoint struct {
	Host string
	Port int
}

type chatdSession struct {
	conn               net.Conn
	transport          chatdTransport
	endpoint           chatdEndpoint
	serverStaticPublic string
	deviceID           string
}

type chatdSessionUpdate struct {
	RoutingInfo        string
	Endpoint           chatdEndpoint
	ServerStaticPublic string
	ContactHints       []waContactHint
	PrivacyTokens      []nativePrivacyTokenUpdate
	AccountLogout      *chatdDeviceLogout
}

type chatdReceivedItem struct {
	message *waappv1.InboundMessage
	payload *chatdEncPayload
	otp     *waappv1.OtpMessage
}

func chatdPhase(phase string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s failed: %w", phase, err)
}

func newChatdClient(cfg chatdClientConfig) *chatdClient {
	if cfg.Host == "" {
		cfg.Host = defaultChatdHost
	}
	if cfg.Port == 0 {
		cfg.Port = defaultChatdPort
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultChatdReadWindow
	}
	if cfg.MaxFrameBytes <= 0 {
		cfg.MaxFrameBytes = defaultChatdMaxFrame
	}
	return &chatdClient{cfg: cfg, codec: newBinaryNodeCodec()}
}

func (c *chatdClient) openTimeout() time.Duration {
	if c.cfg.OpenTimeout > 0 {
		return c.cfg.OpenTimeout
	}
	return c.cfg.Timeout
}

func (c *chatdClient) receiveBatch(ctx context.Context, state nativeState, input EngineMessageInput, appVersion string, now time.Time) ([]*waappv1.InboundMessage, []chatdEncPayload, []*waappv1.OtpMessage, chatdSessionUpdate, error) {
	session, err := c.openSession(ctx, state, input.RegisteredIdentityID, defaultLoginPayload, appVersion)
	if err != nil {
		return nil, nil, nil, chatdSessionUpdate{}, err
	}
	defer session.Close()
	return session.receiveBatch(input, now)
}

func (c *chatdClient) openSession(ctx context.Context, state nativeState, registeredIdentityID string, payloadBuilder func(loginIdentity, nativeState, string) []byte, appVersion string) (*chatdSession, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var lastErr error
	for _, endpoint := range c.endpoints() {
		endpointCtx, cancel := context.WithTimeout(ctx, c.openTimeout())
		session, err := c.openEndpointSession(endpointCtx, endpoint, state, registeredIdentityID, payloadBuilder, appVersion)
		if err == nil {
			cancel()
			return session, nil
		}
		cancel()
		lastErr = fmt.Errorf("chatd endpoint %s failed: %w", endpoint.address(), err)
		if ctx.Err() != nil {
			if lastErr != nil {
				return nil, fmt.Errorf("%w: %v", ctx.Err(), lastErr)
			}
			return nil, ctx.Err()
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no chatd endpoint candidates")
}

func (c *chatdClient) openEndpointSession(ctx context.Context, endpoint chatdEndpoint, state nativeState, registeredIdentityID string, payloadBuilder func(loginIdentity, nativeState, string) []byte, appVersion string) (*chatdSession, error) {
	state.ChatStatic = ensureChatStatic(state.ChatStatic)
	privateKey, err := state.ChatStatic.privateBytes()
	if err != nil {
		return nil, chatdPhase("prepare chatd static identity", err)
	}
	publicKey, err := state.ChatStatic.publicBytes()
	if err != nil {
		return nil, chatdPhase("prepare chatd static identity", err)
	}
	identity, err := resolveLoginIdentity(registeredIdentityID, state)
	if err != nil {
		return nil, chatdPhase("resolve chatd login identity", err)
	}
	routingInfo, err := decodeRoutingInfo(c.cfg.RoutingInfo)
	if err != nil {
		return nil, chatdPhase("decode chatd routing info", err)
	}
	loginPayload := payloadBuilder(identity, state, appVersion)
	conn, err := c.dial(ctx, endpoint)
	if err != nil {
		return nil, chatdPhase("chatd dial", err)
	}
	stopContextClose := closeChatdConnOnContext(ctx, conn)
	defer stopContextClose()
	_ = conn.SetDeadline(time.Now().Add(c.openTimeout()))
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	keys, err := doNoiseHandshake(rw, privateKey, publicKey, loginPayload, routingInfo, c.cfg.MaxFrameBytes)
	if err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %v", ctx.Err(), err)
		}
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return &chatdSession{
		conn:               conn,
		transport:          chatdTransport{rw: rw, keys: keys, codec: c.codec, maxFrameBytes: c.cfg.MaxFrameBytes},
		endpoint:           endpoint,
		serverStaticPublic: b64u(keys.serverStaticPublic),
		deviceID:           chatdDeviceIDFromState(state),
	}, nil
}

func (c *chatdClient) endpoints() []chatdEndpoint {
	source := c.cfg.Endpoints
	if len(source) == 0 {
		source = []chatdEndpoint{{Host: c.cfg.Host, Port: c.cfg.Port}}
	}
	out := make([]chatdEndpoint, 0, len(source))
	seen := map[string]struct{}{}
	for _, endpoint := range source {
		endpoint = endpoint.normalized(c.cfg.Host, c.cfg.Port)
		if endpoint.Host == "" || endpoint.Port <= 0 {
			continue
		}
		key := endpoint.address()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, endpoint)
	}
	if c.cfg.MaxEndpoints > 0 && len(out) > c.cfg.MaxEndpoints {
		return out[:c.cfg.MaxEndpoints]
	}
	return out
}

func (e chatdEndpoint) normalized(defaultHost string, defaultPort int) chatdEndpoint {
	if strings.TrimSpace(e.Host) == "" {
		e.Host = defaultHost
	}
	e.Host = strings.TrimSuffix(strings.TrimSpace(e.Host), ".")
	if e.Port <= 0 {
		e.Port = defaultPort
	}
	return e
}

func (e chatdEndpoint) address() string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

func (s *chatdSession) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func closeChatdConnOnContext(ctx context.Context, conn net.Conn) func() {
	if ctx == nil || conn == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() {
		close(done)
	}
}

func (s *chatdSession) update() chatdSessionUpdate {
	if s == nil {
		return chatdSessionUpdate{}
	}
	return chatdSessionUpdate{Endpoint: s.endpoint, ServerStaticPublic: s.serverStaticPublic}
}

func (s *chatdSession) receiveBatch(input EngineMessageInput, now time.Time) ([]*waappv1.InboundMessage, []chatdEncPayload, []*waappv1.OtpMessage, chatdSessionUpdate, error) {
	if s == nil {
		return nil, nil, nil, chatdSessionUpdate{}, fmt.Errorf("chatd session is not open")
	}
	update := s.update()
	maxMessages := input.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 10
	}
	deadline := waitDeadline(input.WaitTimeout)
	items := []chatdReceivedItem{}
	for len(items) < maxMessages && time.Now().Before(deadline) {
		_ = s.conn.SetReadDeadline(time.Now().Add(time.Until(deadline)))
		node, err := s.transport.readNode()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				break
			}
			if len(items) > 0 {
				break
			}
			return nil, nil, nil, update, chatdPhase("chatd frame read", err)
		}
		nextUpdate, nextItems, err := s.consumeIncomingNode(input, node, update, now)
		update = nextUpdate
		if err != nil {
			if len(items) > 0 {
				break
			}
			return nil, nil, nil, update, err
		}
		before := len(items)
		items = appendReceivedItems(items, nextItems, maxMessages)
		if len(items) > before && len(items) < maxMessages {
			deadline = minTime(deadline, time.Now().Add(defaultChatdPostMessageDrainWindow))
		}
	}
	messages, payloads, otps := splitReceivedItems(items)
	return messages, payloads, otps, update, nil
}

func (s *chatdSession) consumeIncomingNode(input EngineMessageInput, node chatdNode, update chatdSessionUpdate, now time.Time) (chatdSessionUpdate, []chatdReceivedItem, error) {
	if node.Tag == "xmlstreamend" {
		return update, nil, newChatdError("server closed xml stream")
	}
	if isChatdTerminalNode(node) {
		summary := controlNodeSummary(node)
		if chatdTerminalNodeAccountTakeover(node) {
			return update, nil, accountTakenOverError(summary)
		}
		return update, nil, newChatdError("server sent %s", summary)
	}
	oldRegistrationOTP, isOldRegistrationNode, oldRegistrationValid := oldRegistrationOTPFromChatdNode(node, s.deviceID, now)
	ackAttrs := map[string]string(nil)
	if isOldRegistrationNode && s.deviceID != "" {
		ackAttrs = map[string]string{"device_id": s.deviceID}
	}
	if ack, ok := buildAckForNodeWithAttrs(node, ackAttrs); ok {
		if err := s.transport.sendNode(ack); err != nil {
			return update, nil, chatdPhase("chatd ack write", err)
		}
	}
	if nextRouting := routingInfoFromNode(node); nextRouting != "" {
		update.RoutingInfo = nextRouting
	}
	update.ContactHints = dedupeWAContactHints(append(update.ContactHints, contactHintsFromChatdNode(node)...))
	update.PrivacyTokens = dedupePrivacyTokenUpdates(append(update.PrivacyTokens, privacyTokenUpdatesFromChatdNode(node)...))
	if logout, ok := deviceLogoutFromChatdNode(node); ok {
		update.AccountLogout = &logout
	}
	items := []chatdReceivedItem{}
	if oldRegistrationValid {
		if otp := oldRegistrationOTPMessage(input, node, oldRegistrationOTP, now); otp != nil {
			items = append(items, chatdReceivedItem{otp: otp})
		}
	}
	if input.MessageSessionID == "" {
		return update, items, nil
	}
	encs := iterEncPayloads(node)
	if len(encs) == 0 {
		if node.Tag != "message" {
			return update, items, nil
		}
		contact := firstNonEmpty(node.Attrs["from"], node.Attrs["participant"])
		sender := firstNonEmpty(node.Attrs["participant"], node.Attrs["from"])
		payloadSummary := nodePayloadSummary(node)
		message := &waappv1.InboundMessage{MessageId: inboundMessageID(input.WAAccountID, node.Attrs["id"], node.Tag, sender, payloadSummary), MessageSessionId: input.MessageSessionID, Kind: inboundKind(node.Tag), EncryptionState: waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_PLAINTEXT, AckStatus: ackStatusForNode(node), ContactRef: contact, SenderRef: sender, PayloadRef: "node:" + redacted(payloadSummary), ProviderMessageId: node.Attrs["id"], ProviderTimestamp: chatdProviderTimestamp(node.Attrs["t"]), DeleteStatus: waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_NOT_DELETED, ReceivedAt: timestamppb.New(now)}
		return update, append(items, chatdReceivedItem{message: message}), nil
	}
	for _, enc := range encs {
		payload := enc
		payloadRef := payloadRefForEnc(input.WAAccountID, payload.Payload)
		message := &waappv1.InboundMessage{MessageId: inboundMessageID(input.WAAccountID, payload.StanzaID, node.Tag, payload.Sender, payload.Path+":"+hexKey(payload.Payload)), MessageSessionId: input.MessageSessionID, Kind: inboundKind(node.Tag), EncryptionState: waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_ENCRYPTED, AckStatus: ackStatusForNode(node), ContactRef: payload.Contact, SenderRef: payload.Sender, PayloadRef: payloadRef, ProviderMessageId: payload.StanzaID, ProviderTimestamp: chatdProviderTimestamp(payload.StanzaTimestamp), DeleteStatus: waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_NOT_DELETED, ReceivedAt: timestamppb.New(now)}
		items = append(items, chatdReceivedItem{message: message, payload: &payload})
	}
	return update, items, nil
}

func appendReceivedItems(dst []chatdReceivedItem, src []chatdReceivedItem, limit int) []chatdReceivedItem {
	if limit <= 0 {
		return append(dst, src...)
	}
	for _, item := range src {
		if len(dst) >= limit {
			return dst
		}
		dst = append(dst, item)
	}
	return dst
}

func splitReceivedItems(items []chatdReceivedItem) ([]*waappv1.InboundMessage, []chatdEncPayload, []*waappv1.OtpMessage) {
	messages := make([]*waappv1.InboundMessage, 0, len(items))
	payloads := []chatdEncPayload{}
	otps := []*waappv1.OtpMessage{}
	for _, item := range items {
		if item.otp != nil {
			otps = append(otps, item.otp)
		}
		if item.message != nil {
			messages = append(messages, item.message)
			if item.payload != nil {
				payloads = append(payloads, *item.payload)
			}
		}
	}
	return messages, payloads, otps
}

func chatdProviderTimestamp(value string) *timestamppb.Timestamp {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	stamp, err := strconv.ParseInt(value, 10, 64)
	if err != nil || stamp <= 0 {
		return nil
	}
	if stamp > 1_000_000_000_000 {
		return timestamppb.New(time.UnixMilli(stamp).UTC())
	}
	return timestamppb.New(time.Unix(stamp, 0).UTC())
}

func inboundMessageID(accountID string, stanzaID string, tag string, sender string, fingerprint string) string {
	return "wamsg_" + stableID(strings.Join([]string{accountID, stanzaID, tag, sender, fingerprint}, ":"))
}

func (c *chatdClient) checkLoginState(ctx context.Context, state nativeState, input EngineLoginCheckInput, appVersion string) (chatdSessionUpdate, error) {
	session, err := c.openSession(ctx, state, input.RegisteredIdentityID, passiveLoginCheckPayload, appVersion)
	if err != nil {
		return chatdSessionUpdate{}, err
	}
	defer session.Close()
	update := session.update()
	if err := session.transport.sendNode(buildPingNode()); err != nil {
		return update, chatdPhase("chatd ping write", err)
	}
	_ = session.conn.SetReadDeadline(time.Now().Add(minDuration(2*time.Second, c.cfg.Timeout)))
	node, err := session.transport.readNode()
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return update, nil
		}
		return update, chatdPhase("chatd frame read", err)
	}
	if nextRouting := routingInfoFromNode(node); nextRouting != "" {
		update.RoutingInfo = nextRouting
	}
	update.ContactHints = dedupeWAContactHints(append(update.ContactHints, contactHintsFromChatdNode(node)...))
	update.PrivacyTokens = dedupePrivacyTokenUpdates(append(update.PrivacyTokens, privacyTokenUpdatesFromChatdNode(node)...))
	return update, nil
}

func ensureChatStatic(key nativeCurveKeyPair) nativeCurveKeyPair {
	if key.Private != "" && key.Public != "" {
		return key
	}
	newKey, err := newNativeCurveKeyPair()
	if err != nil {
		return key
	}
	return newKey
}

func minTime(left time.Time, right time.Time) time.Time {
	if right.IsZero() || left.Before(right) {
		return left
	}
	return right
}

func minDuration(left time.Duration, right time.Duration) time.Duration {
	if right <= 0 || left < right {
		return left
	}
	return right
}

type chatdTransport struct {
	rw            *bufio.ReadWriter
	keys          *chatdTransportKeys
	codec         *binaryNodeCodec
	maxFrameBytes int
}

func (t *chatdTransport) readNode() (chatdNode, error) {
	for {
		ciphertext, err := chatdReadFrame(t.rw.Reader, t.maxFrameBytes)
		if err != nil {
			return chatdNode{}, err
		}
		plaintext, err := t.keys.decrypt(ciphertext)
		if err != nil {
			return chatdNode{}, err
		}
		if len(plaintext) == 0 {
			continue
		}
		return t.codec.decodeNodePayload(plaintext)
	}
}

func (t *chatdTransport) sendNode(node chatdNode) error {
	payload, err := t.codec.encodeNode(node)
	if err != nil {
		return err
	}
	plaintext := append([]byte{0}, payload...)
	ciphertext, err := t.keys.encrypt(plaintext)
	if err != nil {
		return err
	}
	return chatdWriteFrame(t.rw.Writer, ciphertext)
}

func (c *chatdClient) dial(ctx context.Context, endpoint chatdEndpoint) (net.Conn, error) {
	address := endpoint.address()
	var conn net.Conn
	var err error
	if strings.TrimSpace(c.cfg.ProxyURL) != "" {
		conn, err = c.dialProxy(ctx, endpoint)
	} else {
		dialer := c.netDialer()
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, err
	}
	if !c.cfg.TLS {
		return conn, nil
	}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: endpoint.Host, InsecureSkipVerify: c.cfg.InsecureTLS})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func (c *chatdClient) dialProxy(ctx context.Context, endpoint chatdEndpoint) (net.Conn, error) {
	parsed, err := parseOutboundProxyURL(c.cfg.ProxyURL)
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(parsed.Scheme, "socks5"):
		return c.dialSOCKS5(ctx, parsed, endpoint)
	case parsed.Scheme == "http" || parsed.Scheme == "https":
		return c.dialHTTPConnect(ctx, parsed, endpoint.address())
	default:
		return nil, fmt.Errorf("unsupported chatd proxy scheme %q", parsed.Scheme)
	}
}

func (c *chatdClient) dialHTTPConnect(ctx context.Context, parsed *url.URL, target string) (net.Conn, error) {
	if parsed.Host == "" {
		return nil, fmt.Errorf("proxy host is required")
	}
	dialer := c.netDialer()
	conn, err := dialer.DialContext(ctx, "tcp", parsed.Host)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: parsed.Hostname(), InsecureSkipVerify: c.cfg.InsecureTLS})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		conn = tlsConn
	}
	_ = conn.SetDeadline(time.Now().Add(c.openTimeout()))
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
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

func (c *chatdClient) dialSOCKS5(ctx context.Context, parsed *url.URL, endpoint chatdEndpoint) (net.Conn, error) {
	if parsed.Host == "" {
		return nil, fmt.Errorf("SOCKS5 proxy host is required")
	}
	dialer := c.netDialer()
	conn, err := dialer.DialContext(ctx, "tcp", parsed.Host)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(c.openTimeout()))
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

func (c *chatdClient) netDialer() net.Dialer {
	return net.Dialer{Timeout: c.openTimeout(), KeepAlive: defaultChatdKeepAlive}
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.reader.Read(b) }

func decodeRoutingInfo(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if raw, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return raw, nil
	}
	if raw, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return raw, nil
	}
	if raw, err := base64.StdEncoding.DecodeString(value); err == nil {
		return raw, nil
	}
	return []byte(value), nil
}

func normalizeChatRoutingInfo(value string) string {
	raw, err := decodeRoutingInfo(value)
	if err != nil || len(raw) == 0 || len(raw) > 256 {
		return ""
	}
	return b64u(raw)
}

func resolveLoginIdentity(registeredIdentityID string, state nativeState) (loginIdentity, error) {
	candidates := []string{state.RegistrationJID, registeredIdentityID, state.CC + state.Phone}
	var lastJID string
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		jid := normalizeJID(candidate)
		lastJID = jid
		user := strings.SplitN(strings.SplitN(jid, "@", 2)[0], ":", 2)[0]
		username, err := strconv.ParseUint(user, 10, 64)
		if err == nil {
			return loginIdentity{jid: jid, username: username}, nil
		}
	}
	return loginIdentity{}, fmt.Errorf("cannot derive numeric chatd username from %q", lastJID)
}

func normalizeJID(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "@") {
		return value
	}
	compact := regexp.MustCompile(`\D+`).ReplaceAllString(value, "")
	if compact == "" {
		return value
	}
	return compact + "@s.whatsapp.net"
}

func inboundKind(tag string) waappv1.InboundMessageKind {
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

func ackStatusForNode(node chatdNode) waappv1.MessageAckStatus {
	if _, ok := buildAckForNode(node); ok {
		return waappv1.MessageAckStatus_MESSAGE_ACK_STATUS_ACKED
	}
	return waappv1.MessageAckStatus_MESSAGE_ACK_STATUS_NOT_REQUIRED
}
