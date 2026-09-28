package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// AppConfig holds application configuration
type AppConfig struct {
	Port               string
	Host               string
	Env                string
	DatabaseURL        string
	DBMaxOpenConns     int
	DBMaxIdleConns     int
	DBConnMaxLifetime  time.Duration
	DBConnMaxIdleTime  time.Duration
	VectorEFSearch     int
	CORSAllowedOrigin  string
	DataDir            string
	MaxUploadBytes     int64
	ChatModel          string
	EmbeddingModel     string
	EmbeddingBatchSize int
	RedisAddr          string
	StorageBackend     string
	S3Bucket           string
	S3Region           string
	S3Endpoint         string
	S3AccessKey        string
	S3SecretKey        string
	RateLimitRPS       int
}

// NewAppConfig creates AppConfig from environment variables
func NewAppConfig() AppConfig {
	env := getEnv("APP_ENV", "development")

	port := getEnv("PORT", "8080")
	host := getEnv("HOST", "localhost")

	databaseURL := getEnv("DATABASE_URL", "")
	if databaseURL == "" {
		// Build from individual components
		user := getEnv("POSTGRES_USER", "pguser")
		password := getEnv("POSTGRES_PASSWORD", "pgpass")
		dbHost := getEnv("POSTGRES_HOST", "localhost")
		dbPort := getEnv("POSTGRES_PORT", "5432")
		dbname := getEnv("POSTGRES_DB", "holygrail_dev")
		databaseURL = "postgres://" + user + ":" + password + "@" + dbHost + ":" + dbPort + "/" + dbname + "?sslmode=disable"
	}

	return AppConfig{
		Port:               port,
		Host:               host,
		Env:                env,
		DatabaseURL:        databaseURL,
		DBMaxOpenConns:     getIntEnv("DB_MAX_OPEN_CONNS", 25),
		DBMaxIdleConns:     getIntEnv("DB_MAX_IDLE_CONNS", 5),
		DBConnMaxLifetime:  getDurationEnv("DB_CONN_MAX_LIFETIME", 30*time.Minute),
		DBConnMaxIdleTime:  getDurationEnv("DB_CONN_MAX_IDLE_TIME", 5*time.Minute),
		VectorEFSearch:     getIntEnv("PGVECTOR_EF_SEARCH", 40),
		CORSAllowedOrigin:  getEnv("CORS_ALLOWED_ORIGIN", ""),
		DataDir:            getEnv("STORAGE_PATH", "data"),
		MaxUploadBytes:     getInt64Env("MAX_UPLOAD_BYTES", 50*1024*1024),
		ChatModel:          getEnv("OPENAI_MODEL", "gpt-3.5-turbo"),
		EmbeddingModel:     getEnv("OPENAI_EMBEDDING_MODEL", "text-embedding-3-small"),
		EmbeddingBatchSize: getIntEnv("EMBEDDING_BATCH_SIZE", 64),
		RedisAddr:          getEnv("REDIS_ADDR", "localhost:6379"),
		StorageBackend:     getEnv("STORAGE_BACKEND", "local"),
		S3Bucket:           getEnv("S3_BUCKET", ""),
		S3Region:           getEnv("S3_REGION", ""),
		S3Endpoint:         getEnv("S3_ENDPOINT", ""),
		S3AccessKey:        getEnv("S3_ACCESS_KEY", ""),
		S3SecretKey:        getEnv("S3_SECRET_KEY", ""),
		RateLimitRPS:       getIntEnv("RATE_LIMIT_RPS", 100),
	}
}

// IsProduction reports whether APP_ENV is production.
func (c AppConfig) IsProduction() bool {
	return strings.EqualFold(strings.TrimSpace(c.Env), "production")
}

// ValidateProduction fails fast on unsafe production configuration.
// It is a no-op outside APP_ENV=production. In production it requires:
//   - an explicit non-wildcard CORS_ALLOWED_ORIGIN,
//   - DATABASE_URL explicitly set (no built-from-parts fallback),
//   - no default/placeholder DB password,
//   - no sslmode=disable (TLS required),
//   - S3_BUCKET when STORAGE_BACKEND=s3.
func (c AppConfig) ValidateProduction() error {
	if !c.IsProduction() {
		return nil
	}
	origin := strings.TrimSpace(c.CORSAllowedOrigin)
	if origin == "" || origin == "*" || strings.Contains(origin, "*") {
		return fmt.Errorf("production requires explicit CORS_ALLOWED_ORIGIN (wildcard not allowed)")
	}
	if strings.TrimSpace(os.Getenv("DATABASE_URL")) == "" {
		return fmt.Errorf("production requires DATABASE_URL to be set")
	}
	if strings.Contains(c.DatabaseURL, "pgpass") ||
		strings.Contains(c.DatabaseURL, "changeme") ||
		strings.Contains(c.DatabaseURL, "password123") {
		return fmt.Errorf("production requires a non-default database password")
	}
	if strings.Contains(strings.ToLower(c.DatabaseURL), "sslmode=disable") {
		return fmt.Errorf("production DATABASE_URL must not use sslmode=disable")
	}
	if strings.EqualFold(strings.TrimSpace(c.StorageBackend), "s3") {
		if strings.TrimSpace(c.S3Bucket) == "" {
			return fmt.Errorf("production STORAGE_BACKEND=s3 requires S3_BUCKET to be set")
		}
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getInt64Env(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
