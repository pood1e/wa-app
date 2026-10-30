package onboarding

import (
	"context"
	"crypto/sha256"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *Service) BeginAccountAdd(ctx context.Context, req *waappv1.BeginAccountAddRequest) (*waappv1.AccountAddResponse, error) {
	if err := shared.RequireRequestID(req.GetContext().GetRequestId()); err != nil {
		return failure(err), nil
	}
	normalized := proto.Clone(req).(*waappv1.BeginAccountAddRequest)
	normalized.Phone = wamodel.NormalizePhone(req.GetPhone())
	if normalized.GetPhone().GetE164Number() == "" || normalized.GetSource().GetMethod() == nil {
		return failure(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "phone and adding source are required", false)), nil
	}
	if normalized.GetPlatform() == protocolv1.ClientPlatform_CLIENT_PLATFORM_UNSPECIFIED {
		normalized.Platform = protocolv1.ClientPlatform_CLIENT_PLATFORM_ANDROID
	}
	var err error
	normalized.Proxy, err = wamodel.NormalizeAccountProxy(req.GetProxy())
	if err != nil {
		return failure(err), nil
	}
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(normalized)
	if err != nil {
		return failure(err), nil
	}
	digest := sha256.Sum256(data)
	s.mu.Lock()
	if id := s.requests[req.GetContext().GetRequestId()]; id != "" {
		current := s.flows[id]
		s.mu.Unlock()
		current.mu.Lock()
		defer current.mu.Unlock()
		if current.requestDigest != digest {
			return failure(conflict("request ID was already used for another account adding flow")), nil
		}
		return current.response(), nil
	}
	if len(s.flows) >= s.capacity {
		s.mu.Unlock()
		return failure(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "account adding capacity reached", true)), nil
	}
	flowCtx, cancel := context.WithTimeout(s.root, s.ttl)
	id := s.ids.NewID("waadd_")
	stored := proto.CloneOf(normalized)
	stored.Proxy = nil
	current := &flow{ctx: flowCtx, cancel: cancel, request: stored, requestDigest: digest, acceptanceID: "account-add/" + req.GetContext().GetRequestId(), operations: map[string]operation{},
		snapshot: &waappv1.AccountAddFlow{FlowId: id, Phone: normalized.GetPhone(), Platform: normalized.GetPlatform(), Source: normalized.GetSource(), Status: waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_READY, ExpiresAt: timestamppb.New(s.clock.Now().Add(s.ttl))}}
	current.mu.Lock()
	s.flows[id] = current
	s.requests[req.GetContext().GetRequestId()] = id
	s.mu.Unlock()
	defer current.mu.Unlock()
	call, stopCall := context.WithCancel(ctx)
	defer stopCall()
	stop := context.AfterFunc(flowCtx, stopCall)
	defer stop()
	protocol, err := s.factory(call, normalized)
	if err != nil {
		current.snapshot.LastError = shared.ToProtoError(err)
		cancel()
		current.finish(waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_EXPIRED)
		return current.response(), nil
	}
	current.protocol = protocol
	result := protocol.Probe(call)
	current.snapshot.Probe = &waappv1.AccountProbe{AccountProbeId: s.ids.NewID("waprobe_"), Status: result.Status, SupportedMethods: result.SupportedMethods, ProbedAt: timestamppb.New(s.clock.Now()), MethodStatuses: methodStatuses(result.MethodStatuses), LastError: shared.ToProtoError(result.Err)}
	current.snapshot.LastError = current.snapshot.Probe.GetLastError()
	return current.response(), nil
}
