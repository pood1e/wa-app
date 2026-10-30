package wire

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/pood1e/wa-app/internal/waapp/shared"
)

func NativeAndroidOkHTTPClientHelloSpec() *utls.ClientHelloSpec {
	return &utls.ClientHelloSpec{
		CipherSuites: []uint16{
			utls.TLS_AES_256_GCM_SHA384,
			utls.TLS_AES_128_GCM_SHA256,
			utls.TLS_CHACHA20_POLY1305_SHA256,
			utls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			utls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			utls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			utls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			utls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_RSA_WITH_AES_128_CBC_SHA,
			utls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
		CompressionMethods: []byte{0},
		Extensions: []utls.TLSExtension{
			&utls.SNIExtension{},
			&utls.ExtendedMasterSecretExtension{},
			&utls.RenegotiationInfoExtension{Renegotiation: utls.RenegotiateOnceAsClient},
			&utls.SupportedCurvesExtension{Curves: []utls.CurveID{
				utls.X25519,
				utls.CurveP256,
				utls.CurveP384,
			}},
			&utls.SupportedPointsExtension{SupportedPoints: []byte{0}},
			&utls.SessionTicketExtension{},
			&utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}},
			&utls.StatusRequestExtension{},
			&utls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []utls.SignatureScheme{
				utls.ECDSAWithP256AndSHA256,
				utls.PSSWithSHA256,
				utls.PKCS1WithSHA256,
				utls.ECDSAWithP384AndSHA384,
				utls.PSSWithSHA384,
				utls.PKCS1WithSHA384,
				utls.PSSWithSHA512,
				utls.PKCS1WithSHA512,
				utls.PKCS1WithSHA1,
			}},
			&utls.KeyShareExtension{KeyShares: []utls.KeyShare{{Group: utls.X25519}}},
			&utls.PSKKeyExchangeModesExtension{Modes: []uint8{utls.PskModeDHE}},
			&utls.SupportedVersionsExtension{Versions: []uint16{utls.VersionTLS13, utls.VersionTLS12}},
		},
	}
}

func (c *NativeHTTPClient) PostWASafe(ctx context.Context, endpoint string, plain string, userAgent string, serverPublicKey []byte) (map[string]any, string, error) {
	return c.postWASafeEnvelope(ctx, endpoint, plain, userAgent, serverPublicKey, true)
}

func (c *NativeHTTPClient) PostWASafeNoAuth(ctx context.Context, endpoint string, plain string, userAgent string, serverPublicKey []byte) (map[string]any, error) {
	data, _, err := c.postWASafeEnvelope(ctx, endpoint, plain, userAgent, serverPublicKey, false)
	return data, err
}

func (c *NativeHTTPClient) postWASafeEnvelope(ctx context.Context, endpoint string, plain string, userAgent string, serverPublicKey []byte, sendAttestation bool) (map[string]any, string, error) {
	if endpoint == "" {
		return nil, "", fmt.Errorf("endpoint is not configured")
	}
	if c == nil || c.DialTLSContext == nil {
		return nil, "", fmt.Errorf("native HTTP client is not configured")
	}
	envelope, err := BuildWASafeEnvelope(ctx, []byte(plain), serverPublicKey, c.Attestor, sendAttestation)
	if err != nil {
		return nil, "", err
	}
	endpointURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, "", err
	}
	if endpointURL.Scheme != "https" || endpointURL.Host == "" {
		return nil, "", fmt.Errorf("native endpoint must be https")
	}
	effectiveUserAgent := shared.FirstNonEmpty(userAgent, "WhatsApp/"+DefaultWAAppVersion+" Android/13")
	LogWASafeEnvelopeShape(endpointURL, envelope, effectiveUserAgent)
	resp, err := c.PostOrderedForm(ctx, endpointURL, envelope.Body, effectiveUserAgent, envelope.Authorization)
	if err != nil {
		return nil, envelope.Enc, err
	}
	defer resp.Body.Close()
	body := io.Reader(resp.Body)
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gzipReader, gzipErr := gzip.NewReader(resp.Body)
		if gzipErr != nil {
			return nil, envelope.Enc, gzipErr
		}
		defer gzipReader.Close()
		body = gzipReader
	}
	data, _ := io.ReadAll(io.LimitReader(body, 1<<20))
	result := map[string]any{"status_code": float64(resp.StatusCode), "response_text": string(data)}
	var parsed map[string]any
	if json.Unmarshal(data, &parsed) == nil {
		for key, value := range parsed {
			result[key] = value
		}
	}
	logRegistrationResponse(endpointURL, resp.StatusCode, parsed, envelope.Enc)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, envelope.Enc, fmt.Errorf("wasafe endpoint returned status %d", resp.StatusCode)
	}
	return result, envelope.Enc, nil
}

func (c *NativeHTTPClient) PostOrderedForm(ctx context.Context, endpoint *url.URL, body string, userAgent string, authorization string) (*http.Response, error) {
	host := endpoint.Host
	address := NativeEndpointAddress(endpoint)
	conn, err := c.DialTLSContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	release := true
	defer func() {
		if release {
			_ = conn.Close()
		}
	}()
	if deadline := NativeRequestDeadline(ctx, c.Timeout); !deadline.IsZero() {
		_ = conn.SetDeadline(deadline)
	}
	headers := []string{
		"POST " + NativeRequestURI(endpoint) + " HTTP/1.1",
		"User-Agent: " + userAgent,
		"WaMsysRequest: 1",
	}
	if authorization != "" {
		headers = append(headers, "Authorization: "+authorization)
	}
	headers = append(headers, "request_token: "+strings.ToUpper(NewUUIDString()))
	if endpoint.Hostname() != DefaultNativeHTTPForwardedHost {
		headers = append(headers, "X-Forwarded-Host: "+DefaultNativeHTTPForwardedHost)
	}
	headers = append(headers,
		"Host: "+host,
		"Content-Type: application/x-www-form-urlencoded",
		fmt.Sprintf("Content-Length: %d", len(body)),
		"Connection: Keep-Alive",
		"Accept-Encoding: gzip",
	)
	if _, err := conn.Write([]byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body)); err != nil {
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPost})
	if err != nil {
		return nil, err
	}
	release = false
	resp.Body = &NativeResponseBody{ReadCloser: resp.Body, Conn: conn}
	return resp, nil
}

func LogWASafeEnvelopeShape(endpoint *url.URL, envelope WaSafeEnvelope, userAgent string) {
	if endpoint == nil {
		return
	}
	path := NativeRegistrationEndpointKind(endpoint)
	if path == "" {
		return
	}
	log.Printf(
		"wa_registration_envelope_shape kind=%s enc_sha256=%s app_version=%s body_len=%d enc_len=%d h_present=%t h_len=%d authorization=%t",
		shared.ProbeLogValue(path),
		EncHash(envelope.Enc),
		shared.ProbeLogValue(NativeAppVersionFromUserAgent(userAgent)),
		len(envelope.Body),
		len(envelope.Enc),
		strings.Contains(envelope.Body, "&H="),
		NativeWASafeHLength(envelope.Body),
		envelope.Authorization != "",
	)
}

func NativeRegistrationEndpointKind(endpoint *url.URL) string {
	if endpoint == nil {
		return ""
	}
	raw := endpoint.Query().Get("_")
	if raw == "" {
		raw = endpoint.Path
	}
	switch raw {
	case "/v2/reg_onboard_abprop":
		return "abprop"
	case "/v2/exist":
		return "exist"
	case "/v2/code":
		return "code"
	case "/v2/register":
		return "register"
	default:
		return ""
	}
}

func NativeWASafeHLength(body string) int {
	for _, part := range strings.Split(body, "&") {
		if strings.HasPrefix(part, "H=") {
			return len(strings.TrimPrefix(part, "H="))
		}
	}
	return 0
}

func NativeEndpointAddress(endpoint *url.URL) string {
	if endpoint == nil {
		return ""
	}
	if _, _, err := net.SplitHostPort(endpoint.Host); err == nil {
		return endpoint.Host
	}
	return net.JoinHostPort(endpoint.Hostname(), "443")
}

func NativeRequestURI(endpoint *url.URL) string {
	if endpoint == nil {
		return "/"
	}
	uri := endpoint.RequestURI()
	if uri == "" {
		return "/"
	}
	return uri
}

func NativeRequestDeadline(ctx context.Context, timeout time.Duration) time.Time {
	deadline := time.Time{}
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	if ctxDeadline, ok := ctx.Deadline(); ok && (deadline.IsZero() || ctxDeadline.Before(deadline)) {
		deadline = ctxDeadline
	}
	return deadline
}

type NativeResponseBody struct {
	io.ReadCloser
	Conn net.Conn
}
