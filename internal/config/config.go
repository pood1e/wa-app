package config

import (
	"fmt"
	"strings"

	"github.com/caarlos0/env/v11"
)

type Runtime struct {
	ServiceToken       string `env:"WA_SERVICE_TOKEN,required"`
	GRPCListenAddr     string `env:"WA_APP_LISTEN_ADDR" envDefault:":50091"`
	DashboardHTTPAddr  string `env:"WA_APP_DASHBOARD_HTTP_ADDR" envDefault:":8080"`
	DashboardStaticDir string `env:"WA_APP_DASHBOARD_STATIC_DIR" envDefault:"/app/dashboard/wa"`
	DashboardAuthPass  string `env:"WA_APP_AUTH_PASSWORD"`
	Store              string `env:"WA_APP_STORE,required"`
	PGDSN              string `env:"WA_APP_PG_DSN"`
	SQLiteFile         string `env:"WA_APP_SQLITE_FILE"`
	EphemeralStore     string `env:"WA_APP_EPHEMERAL_STORE" envDefault:"memory"`
	ValkeyURL          string `env:"WA_APP_VALKEY_URL"`
	AccountAddTarget   string `env:"WA_ACCOUNT_ADD_TARGET,required"`
	CommonProxy        string `env:"WA_COMMON_PROXY"`
}

type AccountAdd struct {
	ServiceToken        string `env:"WA_SERVICE_TOKEN,required"`
	GRPCListenAddr      string `env:"WA_ACCOUNT_ADD_LISTEN_ADDR" envDefault:":50092"`
	RuntimeTarget       string `env:"WA_ACCOUNT_RUNTIME_TARGET,required"`
	CommonProxy         string `env:"WA_COMMON_PROXY"`
	IOSSignerTarget     string `env:"WA_ACCOUNT_ADD_IOS_SIGNER_TARGET"`
	AndroidSignerTarget string `env:"WA_ACCOUNT_ADD_ANDROID_SIGNER_TARGET"`
}

func LoadRuntime() (Runtime, error) {
	var cfg Runtime
	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("required runtime configuration is missing or invalid")
	}
	switch cfg.Store {
	case "postgres":
		if strings.TrimSpace(cfg.PGDSN) == "" {
			return cfg, fmt.Errorf("WA_APP_PG_DSN is required for postgres")
		}
	case "sqlite":
		if strings.TrimSpace(cfg.SQLiteFile) == "" {
			return cfg, fmt.Errorf("WA_APP_SQLITE_FILE is required for sqlite")
		}
	default:
		return cfg, fmt.Errorf("WA_APP_STORE must be postgres or sqlite")
	}
	switch cfg.EphemeralStore {
	case "memory":
	case "valkey":
		if strings.TrimSpace(cfg.ValkeyURL) == "" {
			return cfg, fmt.Errorf("WA_APP_VALKEY_URL is required for valkey")
		}
	default:
		return cfg, fmt.Errorf("WA_APP_EPHEMERAL_STORE must be memory or valkey")
	}
	if cfg.ServiceToken == "" || cfg.AccountAddTarget == "" {
		return cfg, fmt.Errorf("runtime service token and adding service target are required")
	}
	return cfg, nil
}

func LoadAccountAdd() (AccountAdd, error) {
	var cfg AccountAdd
	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("required account adding configuration is missing or invalid")
	}
	if cfg.ServiceToken == "" || cfg.RuntimeTarget == "" {
		return cfg, fmt.Errorf("service token and account runtime target are required")
	}
	return cfg, nil
}
