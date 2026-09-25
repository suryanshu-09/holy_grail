package config

import (
	"os"
	"strconv"
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
	EmbeddingModel     string
	EmbeddingBatchSize int
	RedisAddr          string
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
		EmbeddingModel:     getEnv("OPENAI_EMBEDDING_MODEL", "text-embedding-3-small"),
		EmbeddingBatchSize: getIntEnv("EMBEDDING_BATCH_SIZE", 64),
		RedisAddr:          getEnv("REDIS_ADDR", "localhost:6379"),
	}
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
