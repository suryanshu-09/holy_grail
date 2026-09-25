package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"

	"github.com/suryanshu-09/holy_grail/internal/config"
)

const pingTimeout = 5 * time.Second

// NewConnection opens a PostgreSQL connection pool using the provided config,
// applies pool limits and verifies connectivity with a ping.
// Zero/negative pool values fall back to production defaults so a missing
// env knob can never create a zero-size or unbounded pool.
func NewConnection(cfg *config.AppConfig) (*sql.DB, error) {
	maxOpen := cfg.DBMaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 25
	}
	maxIdle := cfg.DBMaxIdleConns
	if maxIdle < 0 {
		maxIdle = 0
	}
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	maxLifetime := cfg.DBConnMaxLifetime
	if maxLifetime <= 0 {
		maxLifetime = 30 * time.Minute
	}
	maxIdleTime := cfg.DBConnMaxIdleTime
	if maxIdleTime <= 0 {
		maxIdleTime = 5 * time.Minute
	}

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: open connection: %w", err)
	}

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(maxLifetime)
	db.SetConnMaxIdleTime(maxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}

	return db, nil
}

// PoolStats reports current connection-pool utilization for telemetry and
// tuning (Phase 24 performance). It is nil-safe: a nil DB yields a zero
// sql.DBStats. Key fields: MaxOpenConnections (configured ceiling), InUse and
// Idle (current split), and WaitCount/WaitDuration (pressure signal — if
// WaitCount grows, raise DBMaxOpenConns or reduce per-request hold time).
func PoolStats(db *sql.DB) sql.DBStats {
	if db == nil {
		return sql.DBStats{}
	}
	return db.Stats()
}
