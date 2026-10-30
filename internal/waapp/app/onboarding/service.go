package onboarding

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type Service struct {
	root     context.Context
	factory  Factory
	validate Validator
	acceptor Acceptor
	clock    shared.Clock
	ids      shared.IDGenerator
	ttl      time.Duration
	capacity int
	mu       sync.Mutex
	flows    map[string]*flow
	requests map[string]string
}

type flow struct {
	mu                sync.Mutex
	ctx               context.Context
	cancel            context.CancelFunc
	request           *waappv1.BeginAccountAddRequest
	requestDigest     [32]byte
	snapshot          *waappv1.AccountAddFlow
	protocol          Protocol
	material          *waappv1.AcceptedAccountMaterial
	validated         bool
	acceptanceID      string
	attemptedDelivery bool
	nextCodeAt        time.Time
	operations        map[string]operation
}

type operation struct {
	digest   [32]byte
	response *waappv1.AccountAddResponse
}

func New(ctx context.Context, factory Factory, validate Validator, acceptor Acceptor, clock shared.Clock, ids shared.IDGenerator) *Service {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	if ids == nil {
		ids = shared.RandomIDGenerator{}
	}
	return &Service{root: ctx, factory: factory, validate: validate, acceptor: acceptor, clock: clock, ids: ids, ttl: 30 * time.Minute, capacity: 256, flows: map[string]*flow{}, requests: map[string]string{}}
}

func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer s.closeAll()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.expire()
		}
	}
}

func (s *Service) GetAccountAdd(ctx context.Context, req *waappv1.AccountAddRequest) (*waappv1.AccountAddResponse, error) {
	current := s.lookup(req.GetFlowId())
	if current == nil {
		return failure(expired()), nil
	}
	current.mu.Lock()
	defer current.mu.Unlock()
	if terminal(current.snapshot.GetStatus()) {
		return current.response(), nil
	}
	if current.ctx.Err() != nil || !s.clock.Now().Before(current.snapshot.GetExpiresAt().AsTime()) {
		current.finish(waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_EXPIRED)
	}
	return current.response(), nil
}

func (s *Service) CancelAccountAdd(ctx context.Context, req *waappv1.AccountAddRequest) (*waappv1.AccountAddResponse, error) {
	current := s.lookup(req.GetFlowId())
	if current == nil {
		return failure(expired()), nil
	}
	current.cancel()
	current.mu.Lock()
	defer current.mu.Unlock()
	if current.snapshot.GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED {
		current.finish(waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_CANCELLED)
	}
	return current.response(), nil
}

func (s *Service) lookup(id string) *flow { s.mu.Lock(); defer s.mu.Unlock(); return s.flows[id] }

func (s *Service) operate(ctx context.Context, id string, request interface {
	proto.Message
	GetContext() *waappv1.RequestContext
}, run func(context.Context, *flow) error) (*waappv1.AccountAddResponse, error) {
	current := s.lookup(id)
	if current == nil {
		return failure(expired()), nil
	}
	current.mu.Lock()
	defer current.mu.Unlock()
	if terminal(current.snapshot.GetStatus()) {
		return current.response(), nil
	}
	if current.ctx.Err() != nil || !s.clock.Now().Before(current.snapshot.GetExpiresAt().AsTime()) {
		current.finish(waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_EXPIRED)
		return current.response(), nil
	}
	if current.snapshot.GetStatus() == waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED {
		return current.response(), nil
	}
	requestContext := request.GetContext()
	if err := shared.RequireRequestID(requestContext.GetRequestId()); err != nil {
		return failure(err), nil
	}
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(request)
	if err != nil {
		return failure(err), nil
	}
	digest := sha256.Sum256(data)
	clear(data)
	if previous, ok := current.operations[requestContext.GetRequestId()]; ok {
		if previous.digest != digest {
			return failure(conflict("request ID was already used for another operation")), nil
		}
		return proto.Clone(previous.response).(*waappv1.AccountAddResponse), nil
	}
	if len(current.operations) >= 64 {
		return failure(conflict("account adding flow operation limit reached")), nil
	}
	call, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(current.ctx, cancel)
	defer stop()
	current.snapshot.LastError = nil
	if err := run(call, current); err != nil {
		current.snapshot.LastError = shared.ToProtoError(err)
	}
	response := current.response()
	if current.snapshot.GetStatus() != waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED {
		current.operations[requestContext.GetRequestId()] = operation{digest, response}
	}
	return response, nil
}

func (s *Service) expire() {
	s.mu.Lock()
	expiredFlows := map[string]*flow{}
	for id, current := range s.flows {
		if current.ctx.Err() != nil {
			expiredFlows[id] = current
		}
	}
	s.mu.Unlock()
	for id, current := range expiredFlows {
		current.cancel()
		current.mu.Lock()
		current.finish(waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_EXPIRED)
		requestID := current.request.GetContext().GetRequestId()
		current.mu.Unlock()
		s.mu.Lock()
		delete(s.flows, id)
		delete(s.requests, requestID)
		s.mu.Unlock()
	}
}

func (s *Service) closeAll() {
	s.mu.Lock()
	flows := s.flows
	s.flows = map[string]*flow{}
	s.requests = map[string]string{}
	s.mu.Unlock()
	for _, current := range flows {
		current.cancel()
		current.mu.Lock()
		current.finish(waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_CANCELLED)
		current.mu.Unlock()
	}
}

func (f *flow) finish(status waappv1.AccountAddStatus) {
	if f.protocol != nil {
		_ = f.protocol.Close()
		f.protocol = nil
	}
	f.material = nil
	f.operations = map[string]operation{}
	f.snapshot.TransferChallenge = nil
	f.snapshot.PasswordChallenge = nil
	f.snapshot.Verification = nil
	f.snapshot.Status = status
}

func (f *flow) response() *waappv1.AccountAddResponse {
	return proto.Clone(&waappv1.AccountAddResponse{Flow: f.snapshot, Error: f.snapshot.GetLastError()}).(*waappv1.AccountAddResponse)
}

func failure(err error) *waappv1.AccountAddResponse {
	return &waappv1.AccountAddResponse{Error: shared.ToProtoError(err)}
}
func expired() error {
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_EXPIRED, "account adding flow expired", false)
}
func conflict(message string) error {
	return shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, message, false)
}

func terminal(status waappv1.AccountAddStatus) bool {
	return status == waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_ACCEPTED || status == waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_CANCELLED || status == waappv1.AccountAddStatus_ACCOUNT_ADD_STATUS_EXPIRED
}
