package wire

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
)

func ChatdPhase(phase string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s failed: %w", phase, err)
}

type BufferedConn struct {
	net.Conn
	Reader *bufio.Reader
}

func (c *BufferedConn) Read(b []byte) (int, error) { return c.Reader.Read(b) }

func DecodeRoutingInfo(value string) ([]byte, error) {
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

func NormalizeChatRoutingInfo(value string) string {
	raw, err := DecodeRoutingInfo(value)
	if err != nil || len(raw) == 0 || len(raw) > 256 {
		return ""
	}
	return B64u(raw)
}
