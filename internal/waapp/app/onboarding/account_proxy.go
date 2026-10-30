package onboarding

import (
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/proto"
)

type proxiedProtocol struct {
	Protocol
	proxy *waappv1.AccountProxy
}

// WithAccountProxy carries the adding flow's proxy into runtime acceptance.
func WithAccountProxy(protocol Protocol, proxy *waappv1.AccountProxy) Protocol {
	return &proxiedProtocol{Protocol: protocol, proxy: proto.CloneOf(proxy)}
}

func (p *proxiedProtocol) Material(loginID, accountID string) (*waappv1.AcceptedAccountMaterial, error) {
	material, err := p.Protocol.Material(loginID, accountID)
	if err != nil || material == nil {
		return material, err
	}
	material = proto.CloneOf(material)
	material.Proxy = proto.CloneOf(p.proxy)
	return material, nil
}
