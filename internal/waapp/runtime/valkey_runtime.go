package runtime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	valkey "github.com/valkey-io/valkey-go"

	"github.com/pood1e/wa-app/internal/waapp/shared"
)

type ValkeyRuntime struct {
	client valkey.Client
}

func NewValkeyRuntime(ctx context.Context, rawURL string) (*ValkeyRuntime, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("WA_APP_VALKEY_URL is required")
	}
	opts, err := valkey.ParseURL(rawURL)
	if err != nil {
		return nil, errors.New("invalid WA_APP_VALKEY_URL")
	}
	opts.DisableCache = true
	client, err := valkey.NewClient(opts)
	if err != nil {
		return nil, fmt.Errorf("connect valkey: %w", err)
	}
	if err := client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()
		return nil, fmt.Errorf("ping valkey: %w", err)
	}
	return &ValkeyRuntime{client: client}, nil
}

func (r *ValkeyRuntime) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	r.client.Close()
	return nil
}

func (r *ValkeyRuntime) SaveTransientState(ctx context.Context, ref string, data []byte, ttl time.Duration) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("transient state ref is required")
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return r.client.Do(ctx, r.client.B().Set().Key(runtimeKey("transient-state", ref)).Value(string(data)).Px(ttl).Build()).Error()
}

func (r *ValkeyRuntime) GetTransientState(ctx context.Context, ref string) ([]byte, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, fmt.Errorf("transient state ref is required")
	}
	value, err := r.client.Do(ctx, r.client.B().Get().Key(runtimeKey("transient-state", ref)).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return nil, fmt.Errorf("transient state ref not found")
	}
	if err != nil {
		return nil, err
	}
	return []byte(value), nil
}

func (r *ValkeyRuntime) DeleteTransientState(ctx context.Context, ref string) error {
	if strings.TrimSpace(ref) == "" {
		return nil
	}
	return r.client.Do(ctx, r.client.B().Del().Key(runtimeKey("transient-state", ref)).Build()).Error()
}

func (r *ValkeyRuntime) ClaimLease(ctx context.Context, key string, holder string, ttl time.Duration) (bool, error) {
	key = strings.TrimSpace(key)
	holder = strings.TrimSpace(holder)
	if key == "" || holder == "" {
		return false, errors.New("lease key and holder are required")
	}
	ttl = shared.NormalizeLeaseTTL(ttl)
	result, err := valkey.NewLuaScript(`
local current = redis.call("GET", KEYS[1])
if not current or current == ARGV[1] then
  redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
  return 1
end
return 0
`).Exec(ctx, r.client, []string{runtimeKey("lease", key)}, []string{holder, strconv.FormatInt(shared.LeaseTTLMilliseconds(ttl), 10)}).ToInt64()
	return result == 1, err
}

func (r *ValkeyRuntime) RenewLease(ctx context.Context, key string, holder string, ttl time.Duration) (bool, error) {
	key = strings.TrimSpace(key)
	holder = strings.TrimSpace(holder)
	if key == "" || holder == "" {
		return false, errors.New("lease key and holder are required")
	}
	ttl = shared.NormalizeLeaseTTL(ttl)
	result, err := valkey.NewLuaScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  redis.call("PEXPIRE", KEYS[1], ARGV[2])
  return 1
end
return 0
`).Exec(ctx, r.client, []string{runtimeKey("lease", key)}, []string{holder, strconv.FormatInt(shared.LeaseTTLMilliseconds(ttl), 10)}).ToInt64()
	return result == 1, err
}

func (r *ValkeyRuntime) ReleaseLease(ctx context.Context, key string, holder string) error {
	key = strings.TrimSpace(key)
	holder = strings.TrimSpace(holder)
	if key == "" || holder == "" {
		return errors.New("lease key and holder are required")
	}
	return valkey.NewLuaScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`).Exec(ctx, r.client, []string{runtimeKey("lease", key)}, []string{holder}).Error()
}

func (r *ValkeyRuntime) OpenSessionLease(ctx context.Context, sessionID string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return r.client.Do(ctx, r.client.B().Set().Key(runtimeKey("message-session", sessionID)).Value("open").Px(ttl).Build()).Error()
}

func (r *ValkeyRuntime) CloseSessionLease(ctx context.Context, sessionID string) error {
	return r.client.Do(ctx, r.client.B().Del().Key(runtimeKey("message-session", sessionID)).Build()).Error()
}

func runtimeKey(scope string, key string) string {
	return "wa-app:" + strings.Trim(scope, ":") + ":" + strings.Trim(strings.TrimSpace(key), ":")
}
