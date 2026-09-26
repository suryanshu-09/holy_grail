package http

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/config"
)

func decodeMetrics(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("metrics body is not JSON: %v", err)
	}
	return decoded
}

func TestMetricsEndpoint(t *testing.T) {
	router := NewRouter(&config.AppConfig{}, RouterDeps{Monitor: NewMonitor(nil)})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := decodeMetrics(t, rec.Body.Bytes())
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	if _, ok := got["uptime_s"]; !ok {
		t.Error("missing uptime_s")
	}
	g, ok := got["goroutines"].(float64)
	if !ok || g < 1 {
		t.Errorf("goroutines = %v, want >= 1", got["goroutines"])
	}
	if _, ok := got["time"]; !ok {
		t.Error("missing time")
	}
	if _, ok := got["db"]; ok {
		t.Error("db section must be omitted without a database")
	}
}

func TestMetricsMethodNotAllowed(t *testing.T) {
	router := NewRouter(&config.AppConfig{}, RouterDeps{Monitor: NewMonitor(nil)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestMetricsNilMonitorDefaults(t *testing.T) {
	// Routers built without a Monitor (e.g. older tests) still serve metrics.
	router := NewRouter(&config.AppConfig{}, RouterDeps{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if decodeMetrics(t, rec.Body.Bytes())["status"] != "ok" {
		t.Error("status field != ok")
	}
}

func TestMetricsDBStats(t *testing.T) {
	mon := NewMonitor(nil)
	mon.stats = func() sql.DBStats {
		return sql.DBStats{MaxOpenConnections: 25, OpenConnections: 3, InUse: 1, Idle: 2, WaitCount: 4}
	}
	router := NewRouter(&config.AppConfig{}, RouterDeps{Monitor: mon})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	db, ok := decodeMetrics(t, rec.Body.Bytes())["db"].(map[string]any)
	if !ok {
		t.Fatal("missing db section")
	}
	for k, want := range map[string]float64{
		"max_open": 25, "open": 3, "in_use": 1, "idle": 2, "wait_count": 4,
	} {
		if db[k] != want {
			t.Errorf("db.%s = %v, want %v", k, db[k], want)
		}
	}
}

func TestMetricsNilSnapshotSafe(t *testing.T) {
	var mon *Monitor
	if mon.Uptime() != 0 {
		t.Error("nil monitor uptime must be 0")
	}
	snap := mon.Snapshot()
	if snap.Status != "ok" || snap.DB != nil {
		t.Errorf("nil snapshot = %+v, want status ok without db", snap)
	}
	// Nil-monitor handler still serves (defensive; router never passes nil).
	rec := httptest.NewRecorder()
	handleMetrics(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
