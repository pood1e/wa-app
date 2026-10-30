package wamodel

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"golang.org/x/net/idna"
	"google.golang.org/protobuf/proto"
)

func NormalizeAccountProxy(value *waappv1.AccountProxy) (*waappv1.AccountProxy, error) {
	if value == nil {
		return nil, nil
	}
	result := proto.Clone(value).(*waappv1.AccountProxy)
	if result.GetProtocol() != waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_HTTP && result.GetProtocol() != waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_SOCKS5 {
		return nil, proxyValidation("proxy protocol must be HTTP or SOCKS5")
	}
	host := strings.TrimSpace(result.GetHost())
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") && net.ParseIP(host[1:len(host)-1]) != nil {
		host = host[1 : len(host)-1]
	}
	if host == "" || strings.ContainsAny(host, "/\\@?#%") || strings.ContainsFunc(host, unicode.IsSpace) {
		return nil, proxyValidation("proxy host must be an IP address or hostname")
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	} else {
		ascii, err := idna.Lookup.ToASCII(host)
		if err != nil || len(ascii) > 253 {
			return nil, proxyValidation("proxy hostname is invalid")
		}
		for _, label := range strings.Split(strings.TrimSuffix(ascii, "."), ".") {
			if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") || strings.ContainsFunc(label, func(c rune) bool {
				return !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-')
			}) {
				return nil, proxyValidation("proxy hostname is invalid")
			}
		}
		host = strings.ToLower(ascii)
	}
	result.Host = host
	if result.GetPort() == 0 || result.GetPort() > 65535 {
		return nil, proxyValidation("proxy port must be between 1 and 65535")
	}
	password := result.GetPassword().GetValue()
	if len(result.GetUsername()) > 255 || len(password) > 255 || strings.ContainsAny(result.GetUsername(), ":\r\n\x00") || strings.ContainsAny(password, "\r\n\x00") {
		return nil, proxyValidation("proxy credentials are invalid")
	}
	if result.GetUsername() == "" && password != "" {
		return nil, proxyValidation("proxy username is required with a password")
	}
	if result.GetPassword().GetSecretRef() != "" || result.GetPassword().GetRedactedValue() != "" {
		return nil, proxyValidation("proxy password must contain a new value or be omitted")
	}
	if password == "" {
		result.Password = nil
	}
	return result, nil
}

func AccountProxyURL(value *waappv1.AccountProxy) (string, error) {
	value, err := NormalizeAccountProxy(value)
	if err != nil || value == nil {
		return "", err
	}
	scheme := "http"
	if value.GetProtocol() == waappv1.AccountProxyProtocol_ACCOUNT_PROXY_PROTOCOL_SOCKS5 {
		scheme = "socks5"
	}
	address := &url.URL{Scheme: scheme, Host: net.JoinHostPort(value.GetHost(), strconv.Itoa(int(value.GetPort())))}
	if value.GetUsername() != "" {
		address.User = url.UserPassword(value.GetUsername(), value.GetPassword().GetValue())
	}
	return address.String(), nil
}

func proxyValidation(message string) error {
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, message, false)
}
