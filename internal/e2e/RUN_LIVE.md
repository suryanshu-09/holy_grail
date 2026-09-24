# Live E2E run — Upload PDF → Results

The scripted suite (`go test ./internal/e2e/`) uses fakes and needs nothing.
This note covers the live variant (`TestLive_UploadToResults`), which replays
the same stage order against a real server + Postgres.

## 1. Prerequisites

- Server running (default `http://localhost:8080`, `GET /health` 2xx).
- Postgres reachable with migrations applied (`make migrate-up` or equivalent)
  and seed data loaded (`seeds/seed.sql`) so topics/questions exist.
- A real PDF fixture on disk.

## 2. Environment

```bash
export E2E_LIVE=1
export E2E_BASE_URL=http://localhost:8080
export E2E_TEST_PDF=/path/to/sample.pdf   # defaults to testdata/sample.pdf
export E2E_API_TOKEN=...                  # optional Bearer token
export E2E_TOPIC=Deadlock                 # optional, default Deadlock
```

Without `E2E_LIVE=1` (or when `E2E_BASE_URL` / the PDF is missing) the live
test calls `t.Skip` and the package stays green.

## 3. Run

```bash
# Fake/scripted suite (no keys, no DB):
go test ./internal/e2e/ -v

# Live variant (needs the env above + running server/DB):
E2E_LIVE=1 E2E_BASE_URL=http://localhost:8080 E2E_TEST_PDF=/path/to/sample.pdf \
  go test ./internal/e2e/ -run TestLive -v
```

## 4. What the live test does

1. `POST /api/documents` (multipart `file`) — upload stage.
2. Polls `GET /health` for server readiness while extract/classify/embed run.
3. `GET /api/topics` — list + select topic.
4. `POST /api/quiz` (`mcq`, 2 questions, chosen topic) — retrieve + generate.
5. Probes quiz sessions — answer/results stage (grading server-side).

Adjust the endpoint paths in `live_test.go` if the API shapes change.
