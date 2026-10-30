// Package attestation provides the private transport to device SDK workers.
package attestation

import (
	"errors"
	"net"
	"net/netip"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Dial opens a Unix socket or a loopback SSH tunnel without RPC retries.
// The caller owns the connection.
func Dial(target string) (*grpc.ClientConn, error) {
	if !strings.HasPrefix(target, "unix:///") {
		host, _, err := net.SplitHostPort(target)
		address, parseErr := netip.ParseAddr(host)
		if err != nil || parseErr != nil || !address.IsLoopback() {
			return nil, errors.New("target must be a private Unix socket or loopback SSH forward")
		}
	}
	return grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDisableRetry(), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(65536)))
}
