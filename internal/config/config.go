package config

import (
	"log"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	DashboardAuthPass                string `env:"WA_APP_AUTH_PASSWORD"`
	GRPCListenAddr                   string `env:"WA_APP_LISTEN_ADDR"`
	DashboardHTTPAddr                string `env:"WA_APP_DASHBOARD_HTTP_ADDR"`
	DashboardStaticDir               string `env:"WA_APP_DASHBOARD_STATIC_DIR"`
	DataDir                          string `env:"WA_APP_DATA_DIR"`
	CommonProxy                      string `env:"WA_COMMON_PROXY"`
	RegistrationProxyLeaseMode       string `env:"WA_REGISTRATION_PROXY_LEASE_MODE"`
	RegistrationProxyLeaseAPIBaseURL string `env:"WA_REGISTRATION_PROXY_LEASE_API_BASE_URL"`
	RegistrationProxyLeaseAccountID  string `env:"WA_REGISTRATION_PROXY_LEASE_ACCOUNT_ID"`
	RegistrationProxyLeaseAuthToken  string `env:"WA_REGISTRATION_PROXY_LEASE_AUTH_TOKEN"`
	PGDSN                            string `env:"WA_APP_PG_DSN"`
	RedisURL                         string `env:"WA_APP_REDIS_URL"`
}

func Load() Config {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("load wa-app config: %v", err)
	}
	return cfg
}
