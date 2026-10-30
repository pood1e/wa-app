package communication

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	pstate "github.com/pood1e/wa-app/internal/waapp/protocol/state"
	pwire "github.com/pood1e/wa-app/internal/waapp/protocol/wire"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

const (
	DefaultChatdHost                   = "g.whatsapp.net"
	DefaultChatdPort                   = 443
	DefaultChatdMaxFrame               = 4 << 20
	DefaultChatdReadWindow             = 20 * time.Second
	DefaultChatdKeepAlive              = 30 * time.Second
	DefaultChatdPostMessageDrainWindow = 250 * time.Millisecond
)

type ChatdClientConfig struct {
	Host          string
	Port          int
	Endpoints     []ChatdEndpoint
	TLS           bool
	RoutingInfo   string
	ProxyURL      string
	InsecureTLS   bool
	Timeout       time.Duration
	OpenTimeout   time.Duration
	MaxFrameBytes int
	MaxEndpoints  int
}

type ChatdClient struct {
	Cfg   ChatdClientConfig
	Codec *pwire.BinaryNodeCodec
}

type ChatdEndpoint struct {
	Host string
	Port int
}

type ChatdSession struct {
	Conn                 net.Conn
	Transport            ChatdTransport
	Endpoint             ChatdEndpoint
	ServerStaticPublic   string
	ServerStaticPQPublic []byte
	DeviceID             string
	PhoneJID             string
	IdentityJID          string
	loginUpdate          ChatdSessionUpdate
	canonicalNonce       string
	nextHeartbeatAt      time.Time
	lastHeartbeatSuccess time.Time
}

type ChatdSessionUpdate struct {
	RoutingInfo          string
	Endpoint             ChatdEndpoint
	ServerStaticPublic   string
	ServerStaticPQPublic []byte
	ContactHints         []wacore.WAContactHint
	PrivacyTokens        []NativePrivacyTokenUpdate
	AccountLogout        *ChatdDeviceLogout
	Receipts             []*waappv1.MessageReceipt
	Presences            []*waappv1.WAContactPresence
	SignalRetries        []pwire.ChatdNode
}

type ChatdReceivedItem struct {
	Message *waappv1.InboundMessage
	Payload *pwire.ChatdEncPayload
	Otp     *waappv1.OtpMessage
}

func NewChatdClient(cfg ChatdClientConfig) *ChatdClient {
	if cfg.Host == "" {
		cfg.Host = DefaultChatdHost
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultChatdPort
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultChatdReadWindow
	}
	if cfg.MaxFrameBytes <= 0 {
		cfg.MaxFrameBytes = DefaultChatdMaxFrame
	}
	return &ChatdClient{Cfg: cfg, Codec: pwire.NewBinaryNodeCodec()}
}

func (c *ChatdClient) OpenTimeout() time.Duration {
	if c.Cfg.OpenTimeout > 0 {
		return c.Cfg.OpenTimeout
	}
	return c.Cfg.Timeout
}

func (c *ChatdClient) ReceiveBatch(ctx context.Context, state pstate.NativeState, input wacore.EngineMessageInput, appVersion string, clock shared.Clock) ([]*waappv1.InboundMessage, []pwire.ChatdEncPayload, []*waappv1.OtpMessage, ChatdSessionUpdate, error) {
	session, err := c.OpenSession(ctx, state, input.RegisteredIdentityID, DefaultLoginPayload, appVersion)
	if err != nil {
		return nil, nil, nil, ChatdSessionUpdate{}, err
	}
	defer session.Close()
	return session.ReceiveBatch(ctx, input, clock)
}

func (c *ChatdClient) OpenSession(ctx context.Context, state pstate.NativeState, registeredIdentityID string, payloadBuilder func(pwire.LoginIdentity, pstate.NativeState, string) []byte, appVersion string) (*ChatdSession, error) {
	if err := requireChatdPlatform(state.Client); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var lastErr error
	for _, endpoint := range c.Endpoints() {
		endpointCtx, cancel := context.WithTimeout(ctx, c.OpenTimeout())
		session, err := c.OpenEndpointSession(endpointCtx, endpoint, state, registeredIdentityID, payloadBuilder, appVersion)
		if err == nil {
			cancel()
			return session, nil
		}
		cancel()
		lastErr = fmt.Errorf("chatd endpoint %s failed: %w", endpoint.Address(), err)
		if IsAuthenticationFailure(err) {
			return nil, lastErr
		}
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

func (c *ChatdClient) OpenEndpointSession(ctx context.Context, endpoint ChatdEndpoint, state pstate.NativeState, registeredIdentityID string, payloadBuilder func(pwire.LoginIdentity, pstate.NativeState, string) []byte, appVersion string) (*ChatdSession, error) {
	state.ChatStatic = EnsureChatStatic(state.ChatStatic)
	privateKey, err := state.ChatStatic.PrivateBytes()
	if err != nil {
		return nil, pwire.ChatdPhase("prepare chatd static identity", err)
	}
	publicKey, err := state.ChatStatic.PublicBytes()
	if err != nil {
		return nil, pwire.ChatdPhase("prepare chatd static identity", err)
	}
	identity, err := ResolveLoginIdentity(registeredIdentityID, state)
	if err != nil {
		return nil, pwire.ChatdPhase("resolve chatd login identity", err)
	}
	routingInfo, err := pwire.DecodeRoutingInfo(c.Cfg.RoutingInfo)
	if err != nil {
		return nil, pwire.ChatdPhase("decode chatd routing info", err)
	}
	loginPayload := payloadBuilder(identity, state, appVersion)
	conn, err := c.Dial(ctx, endpoint)
	if err != nil {
		return nil, pwire.ChatdPhase("chatd dial", err)
	}
	stopContextClose := CloseChatdConnOnContext(ctx, conn)
	defer stopContextClose()
	_ = conn.SetDeadline(time.Now().Add(c.OpenTimeout()))
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	keys, err := pwire.DoNoiseHandshake(rw, &protocolv1.CurveKeyPair{Public: publicKey, Private: privateKey}, state.Client,
		state.ChatConnection,
		loginPayload, routingInfo, c.Cfg.MaxFrameBytes)
	if err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %v", ctx.Err(), err)
		}
		return nil, err
	}
	session := &ChatdSession{
		Conn:                 conn,
		Transport:            ChatdTransport{Rw: rw, Keys: keys, Codec: c.Codec, MaxFrameBytes: c.Cfg.MaxFrameBytes},
		Endpoint:             endpoint,
		ServerStaticPublic:   pwire.B64u(keys.ServerStaticPublic),
		ServerStaticPQPublic: bytes.Clone(keys.ServerStaticPQPublic),
		DeviceID:             ChatdDeviceIDFromState(state),
		PhoneJID:             wacore.NormalizeWAJID(shared.FirstNonEmpty(state.RegistrationJID, state.CC+state.Phone)),
	}
	_, session.IdentityJID = ownMessageJIDs(&state)
	if err := session.authenticate(); err != nil {
		_ = session.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return session, nil
}

func (c *ChatdClient) Endpoints() []ChatdEndpoint {
	source := c.Cfg.Endpoints
	if len(source) == 0 {
		source = []ChatdEndpoint{{Host: c.Cfg.Host, Port: c.Cfg.Port}}
	}
	out := make([]ChatdEndpoint, 0, len(source))
	seen := map[string]struct{}{}
	for _, endpoint := range source {
		endpoint = endpoint.Normalized(c.Cfg.Host, c.Cfg.Port)
		if endpoint.Host == "" || endpoint.Port <= 0 {
			continue
		}
		key := endpoint.Address()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, endpoint)
	}
	if c.Cfg.MaxEndpoints > 0 && len(out) > c.Cfg.MaxEndpoints {
		return out[:c.Cfg.MaxEndpoints]
	}
	return out
}

func (e ChatdEndpoint) Normalized(defaultHost string, defaultPort int) ChatdEndpoint {
	if strings.TrimSpace(e.Host) == "" {
		e.Host = defaultHost
	}
	e.Host = strings.TrimSuffix(strings.TrimSpace(e.Host), ".")
	if e.Port <= 0 {
		e.Port = defaultPort
	}
	return e
}

func (e ChatdEndpoint) Address() string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

func (s *ChatdSession) Close() error {
	if s == nil || s.Conn == nil {
		return nil
	}
	return s.Conn.Close()
}

func CloseChatdConnOnContext(ctx context.Context, conn net.Conn) func() {
	if ctx == nil || conn == nil {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

func (s *ChatdSession) Update() ChatdSessionUpdate {
	if s == nil {
		return ChatdSessionUpdate{}
	}
	update := s.loginUpdate
	update.Endpoint, update.ServerStaticPublic = s.Endpoint, s.ServerStaticPublic
	update.ServerStaticPQPublic = bytes.Clone(s.ServerStaticPQPublic)
	return update
}
