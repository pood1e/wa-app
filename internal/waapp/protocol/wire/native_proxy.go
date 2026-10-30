package wire

import (
	"fmt"
	"net/url"
	"strings"
)

func NormalizeProxyURLString(value string) (string, error) {
	parsed, err := ParseOutboundProxyURL(value)
	if err != nil || parsed == nil {
		return "", err
	}
	return parsed.String(), nil
}

func ParseOutboundProxyURL(value string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if !strings.Contains(value, "://") {
		value = "socks5://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL")
	}
	parsed.Scheme = NormalizeProxyScheme(parsed.Scheme)
	if parsed.Scheme == "" {
		return nil, fmt.Errorf("invalid proxy URL scheme")
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return nil, fmt.Errorf("proxy host is required")
	}
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

func NormalizeProxyScheme(scheme string) string {
	scheme = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(scheme)), "://")
	switch scheme {
	case "http", "https", "socks5", "socks5h":
		return scheme
	case "socks", "socks5-proxy":
		return "socks5"
	default:
		return ""
	}
}
