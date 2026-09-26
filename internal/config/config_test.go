package config

import (
	"testing"
)

func prodCfg() AppConfig {
	return AppConfig{
		Env:               "production",
		CORSAllowedOrigin: "https://app.example.com",
		DatabaseURL:       "postgres://user:strongsecret@db:5432/holygrail?sslmode=require",
		StorageBackend:    "local",
		RateLimitRPS:      100,
	}
}

func TestValidateProductionOK(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:strongsecret@db:5432/holygrail?sslmode=require")
	cfg := prodCfg()
	if err := cfg.ValidateProduction(); err != nil {
		t.Fatalf("valid production config rejected: %v", err)
	}
}

func TestValidateProductionWildcardCORS(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:strongsecret@db:5432/holygrail?sslmode=require")
	for _, origin := range []string{"", "*", "https://*.example.com"} {
		cfg := prodCfg()
		cfg.CORSAllowedOrigin = origin
		if err := cfg.ValidateProduction(); err == nil {
			t.Fatalf("wildcard/empty CORS %q should fail in production", origin)
		}
	}
}

func TestValidateProductionMissingDatabaseURL(t *testing.T) {
	// Ensure DATABASE_URL is unset.
	t.Setenv("DATABASE_URL", "")
	// t.Setenv with "" still sets it to ""; unset via direct approach is not
	// available pre-Go1.23, but ValidateProduction treats "" as missing,
	// which is what we assert.
	cfg := prodCfg()
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatalf("missing DATABASE_URL should fail in production")
	}
}

func TestValidateProductionSSLMode(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:strongsecret@db:5432/holygrail?sslmode=disable")
	cfg := prodCfg()
	cfg.DatabaseURL = "postgres://user:strongsecret@db:5432/holygrail?sslmode=disable"
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatalf("sslmode=disable should fail in production")
	}
}

func TestValidateProductionDefaultPassword(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://pguser:pgpass@localhost:5432/holygrail_dev?sslmode=require")
	cfg := prodCfg()
	cfg.DatabaseURL = "postgres://pguser:pgpass@localhost:5432/holygrail_dev?sslmode=require"
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatalf("default DB password should fail in production")
	}
}

func TestValidateProductionS3RequiresBucket(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:strongsecret@db:5432/holygrail?sslmode=require")
	cfg := prodCfg()
	cfg.StorageBackend = "s3"
	cfg.S3Bucket = ""
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatalf("s3 backend without bucket should fail in production")
	}
	cfg.S3Bucket = "my-bucket"
	if err := cfg.ValidateProduction(); err != nil {
		t.Fatalf("s3 backend with bucket rejected: %v", err)
	}
}

func TestValidateProductionSkippedOutsideProduction(t *testing.T) {
	cfg := AppConfig{Env: "development", CORSAllowedOrigin: "*", DatabaseURL: "anything"}
	if err := cfg.ValidateProduction(); err != nil {
		t.Fatalf("non-production should skip validation: %v", err)
	}
}

func TestNewAppConfigProdFields(t *testing.T) {
	t.Setenv("STORAGE_BACKEND", "s3")
	t.Setenv("S3_BUCKET", "bkt")
	t.Setenv("S3_REGION", "us-east-1")
	t.Setenv("RATE_LIMIT_RPS", "50")
	cfg := NewAppConfig()
	if cfg.StorageBackend != "s3" {
		t.Fatalf("StorageBackend = %q", cfg.StorageBackend)
	}
	if cfg.S3Bucket != "bkt" || cfg.S3Region != "us-east-1" {
		t.Fatalf("S3 fields not parsed: %+v", cfg)
	}
	if cfg.RateLimitRPS != 50 {
		t.Fatalf("RateLimitRPS = %d", cfg.RateLimitRPS)
	}
}
