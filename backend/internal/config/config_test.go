package config

import "testing"

func validProductionConfig() Config {
	return Config{
		Environment:        "production",
		AuthSecret:         "0123456789abcdef0123456789abcdef",
		AdminEmail:         "admin@example.com",
		AdminPassword:      "a-strong-password",
		APIKeyEncKey:       "0123456789abcdef0123456789abcdef",
		ClickHousePassword: "clickhouse-password",
		PostgresURL:        "postgres://ndovu:postgres-password@postgres:5432/ndovu?sslmode=disable",
		CORSOrigins:        "https://ndovu.example.com,https://app.example.com",
		S3Endpoint:         "account.r2.cloudflarestorage.com",
		S3AccessKey:        "access-key",
		S3SecretKey:        "secret-key",
		S3UseSSL:           true,
		SnapshotBucket:     "ndovu-snapshots",
		SessionEncKey:      "0123456789abcdef0123456789abcdef",
	}
}

func TestValidateDevelopmentAllowsDevelopmentDefaults(t *testing.T) {
	if err := validate(Config{Environment: "development"}, "api"); err != nil {
		t.Fatalf("defaults de desenvolvimento não deveriam falhar: %v", err)
	}
}

func TestValidateRejectsUnknownEnvironment(t *testing.T) {
	if err := validate(Config{Environment: "staging"}, "api"); err == nil {
		t.Fatal("esperava erro para ambiente desconhecido")
	}
}

func TestValidateProductionAcceptsExplicitConfiguration(t *testing.T) {
	if err := validate(validProductionConfig(), "api"); err != nil {
		t.Fatalf("configuração válida de produção rejeitada: %v", err)
	}
}

func TestLoadProductionDoesNotSetBootstrapIngestKey(t *testing.T) {
	t.Setenv("NDOVU_ENV", "production")
	t.Setenv("NDOVU_AUTH_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("NDOVU_ADMIN_EMAIL", "admin@example.com")
	t.Setenv("NDOVU_ADMIN_PASSWORD", "a-strong-password")
	t.Setenv("NDOVU_API_KEY_ENC_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("NDOVU_CORS_ORIGINS", "https://ndovu.example.com")
	t.Setenv("NDOVU_CLICKHOUSE_PASSWORD", "clickhouse-password")
	t.Setenv("NDOVU_POSTGRES_URL", "postgres://ndovu:postgres-password@postgres:5432/ndovu")
	t.Setenv("NDOVU_S3_ENDPOINT", "account.r2.cloudflarestorage.com")
	t.Setenv("NDOVU_S3_ACCESS_KEY", "access-key")
	t.Setenv("NDOVU_S3_SECRET_KEY", "secret-key")
	t.Setenv("NDOVU_S3_USE_SSL", "true")
	t.Setenv("NDOVU_SNAPSHOT_BUCKET", "ndovu-snapshots")
	t.Setenv("NDOVU_SESSION_ENC_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("NDOVU_BOOTSTRAP_INGEST_KEY", "dev-ingest-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("configuração de produção válida rejeitada: %v", err)
	}
	if cfg.BootstrapIngestKey != "" {
		t.Fatal("a chave bootstrap de desenvolvimento não deve ser carregada em produção")
	}
}

func TestValidateProductionWriterDoesNotRequireAPISecrets(t *testing.T) {
	cfg := validProductionConfig()
	cfg.AuthSecret = ""
	cfg.AdminEmail = ""
	cfg.AdminPassword = ""
	cfg.CORSOrigins = ""
	cfg.S3Endpoint = ""
	cfg.S3AccessKey = ""
	cfg.S3SecretKey = ""
	cfg.SnapshotBucket = ""
	cfg.S3UseSSL = false
	if err := validate(cfg, "writer"); err != nil {
		t.Fatalf("writer não deveria exigir segredos exclusivos da API: %v", err)
	}
}

func TestValidateProductionRejectsInsecureSettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"segredo JWT padrão", func(c *Config) { c.AuthSecret = "dev-secret-troque-em-producao" }},
		{"chave de cifragem de API curta", func(c *Config) { c.APIKeyEncKey = "curta" }},
		{"senha admin curta", func(c *Config) { c.AdminPassword = "curta" }},
		{"senha ClickHouse padrão", func(c *Config) { c.ClickHousePassword = "ndovu" }},
		{"senha Postgres padrão", func(c *Config) { c.PostgresURL = "postgres://ndovu:ndovu@postgres:5432/ndovu" }},
		{"CORS wildcard", func(c *Config) { c.CORSOrigins = "*" }},
		{"origem sem HTTPS", func(c *Config) { c.CORSOrigins = "http://ndovu.example.com" }},
		{"TLS S3 desabilitado", func(c *Config) { c.S3UseSSL = false }},
		{"snapshot sem credenciais", func(c *Config) { c.S3SecretKey = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validProductionConfig()
			tt.mutate(&cfg)
			if err := validate(cfg, "api"); err == nil {
				t.Fatal("esperava rejeição de configuração insegura")
			}
		})
	}
}
