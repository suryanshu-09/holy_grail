package http

import (
	"database/sql"
	"net/http"
	"runtime"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/httpx"
)

// Monitor is a lightweight process/dependency monitor backing
// GET /api/v1/metrics (PLAN5 Phase 26 — Monitoring). It reports process
// uptime, goroutine count and, when constructed with a live *sql.DB,
// current connection-pool stats. All methods are nil-safe: a nil Monitor
// reports zero uptime and omits the db section.
type Monitor struct {
	start time.Time
	stats func() sql.DBStats
}

// NewMonitor returns a Monitor measuring uptime from construction time.
// A nil db is allowed; the db section is then omitted from snapshots.
func NewMonitor(db *sql.DB) *Monitor {
	m := &Monitor{start: time.Now()}
	if db != nil {
		m.stats = db.Stats
	}
	return m
}

// Uptime returns the time since the Monitor was constructed (0 when nil).
func (m *Monitor) Uptime() time.Duration {
	if m == nil {
		return 0
	}
	return time.Since(m.start)
}

type metricsDB struct {
	MaxOpen        int   `json:"max_open"`
	Open           int   `json:"open"`
	InUse          int   `json:"in_use"`
	Idle           int   `json:"idle"`
	WaitCount      int64 `json:"wait_count"`
	WaitDurationMs int64 `json:"wait_duration_ms"`
}

type metricsResponse struct {
	Status     string     `json:"status"`
	UptimeS    int64      `json:"uptime_s"`
	Goroutines int        `json:"goroutines"`
	Time       string     `json:"time"`
	DB         *metricsDB `json:"db,omitempty"`
}

// Snapshot builds the current metrics payload. It never touches the
// database connection itself — sql.DB.Stats is lock-local — so the endpoint
// stays cheap under scrape load.
func (m *Monitor) Snapshot() metricsResponse {
	res := metricsResponse{
		Status:     "ok",
		Goroutines: runtime.NumGoroutine(),
		Time:       time.Now().UTC().Format(time.RFC3339),
	}
	if m != nil {
		res.UptimeS = int64(m.Uptime().Seconds())
		if m.stats != nil {
			s := m.stats()
			res.DB = &metricsDB{
				MaxOpen:        s.MaxOpenConnections,
				Open:           s.OpenConnections,
				InUse:          s.InUse,
				Idle:           s.Idle,
				WaitCount:      s.WaitCount,
				WaitDurationMs: s.WaitDuration.Milliseconds(),
			}
		}
	}
	return res
}

func handleMetrics(m *Monitor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, m.Snapshot())
	})
}
