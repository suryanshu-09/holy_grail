# 22. Phase 17 — Retrieval Evaluation

## Goal

Determine whether your RAG system actually works.

Do not rely on:

> "It feels pretty good."

---

## Create Evaluation Dataset

Create approximately 50–100 queries.

Example:

```text
"deadlock questions"
"questions about circular wait"
"paging questions"
"virtual memory"
"Banker's algorithm"
"CPU scheduling algorithms"
```

For each query manually determine expected relevant questions.

---

## Measure

### Recall

Did we retrieve the questions that should have been retrieved?

### Precision

How many retrieved questions were actually relevant?

### Top-K accuracy

Was a relevant question within:

```text
Top 3?
Top 5?
Top 10?
```

---

## Test Different Retrieval Strategies

Compare:

```text
Vector only

Metadata only

Keyword only

Hybrid

Hybrid + reranker
```

Document the results.

---

## Completion Criteria

You can demonstrate quantitatively that your retrieval improved.

---

## Tasks

- [x] Create evaluation dataset (60 queries, 12 categories x 5)
- [x] Manually determine expected relevant questions per query
- [x] Implement recall / precision / Top-K (3/5/10) metrics
- [x] Test vector-only retrieval
- [x] Test metadata-only retrieval
- [x] Test keyword-only retrieval
- [x] Test hybrid retrieval
- [x] Test hybrid + reranker retrieval
- [x] Compare strategies side-by-side and document results
- [x] Expose debug evaluation endpoint

---

Completed: 2026-09-22

Verified: `internal/search/evaluation.go` adds pure recall/precision/HitAtK, `EvaluateQuery`, `AggregateMetricsFor`/`CompareStrategies` over strategies vector/metadata/keyword/hybrid/hybrid+reranker with Top-K 3/5/10; `internal/search/evaluation_dataset.go` bundles 60 queries (12 OS categories x 5, keyword + natural-language + paraphrase phrasing) with expected IDs; `internal/search/evaluation_runner.go` adds `StrategyRunner` (vector via `Service.Search`, metadata via topic filter, keyword via FTS repo, hybrid with/without rerank) plus `ComparisonReport`/`BestByRecall`; `internal/http/search_eval.go` exposes `GET /api/v1/debug/eval` (GET/POST, 503 when unconfigured) wired via `EvalRunner` in `internal/http/router.go` and constructed in `cmd/api/main.go` (nil-safe when search pipeline unavailable); `lib/api.ts` adds `RetrievalStrategy`/`DebugEvalResponse` types plus `getDebugEval()`; unit tests cover metrics, dataset, runner and handler; `go vet ./...`, `go test ./...` and `npm run build` pass.

# 23. Phase 18 — Multimodal Improvements

## Goal

Improve handling of questions where images contain essential information.

---

## First Version

Simply preserve images.

```text
Question
 ├── text
 └── image
```

The LLM receives the image when generating the quiz.

---

## Later

Use vision models during ingestion.

For each image:

```text
Image
 ↓
Vision model
 ↓
Description
```

Example:

```text
"Directed graph with nodes A, B, C and edges..."
```

Store:

```text
image_description
```

---

## Potential Multimodal Retrieval

Eventually:

```text
Text embedding
+
Image embedding
```

But this should be considered an advanced feature.

Do not implement it until text retrieval works well.

---

## Tasks

- [x] Vision model integration
- [x] Image descriptions
- [x] Image-aware question extraction
- [x] Image-aware quiz generation
- [x] Test diagrams
- [x] Test graphs
- [x] Test mathematical figures
- [x] Test tables
- [x] Test charts

---

Completed: 2026-09-22

Verified: `internal/llm/vision.go` adds OpenAI vision describing (image_url base64, `OPENAI_VISION_MODEL` default gpt-4o-mini, graceful nil-safe fallback) with figure-type classification/normalization (diagram/graph/math_figure/table/chart/photo/unknown); `internal/extraction/vision.go` adds `VisionDescriber` interface; `ImageRef` gains description/figure-type fields persisted in manifests/debug artifacts via best-effort pipeline step; question extraction propagates image descriptions + visual-context notes into `ImagesJSON`/`ExtractionNotesJSON`; quiz `SourceBlock` renders `image_description`/`figure_type` into prompts (all 4 modes) with image-aware deterministic fallback; `cmd/api/main.go` wires describer when `OPENAI_API_KEY` present; unit tests cover vision prompts, figure types, description association, image-aware prompts and extraction→quiz e2e; `go vet ./...` and `go test -count=1 ./...` pass.

# 24. Phase 19 — Background Jobs

## Goal

Move expensive document processing out of HTTP requests.

---

## Add Redis

Docker Compose becomes:

```text
docker compose
├── postgres
└── redis
```

---

## Queue

Example:

```text
upload document
       │
       ▼
create processing job
       │
       ▼
Redis
       │
       ▼
worker
       │
       ├── extract text
       ├── OCR
       ├── extract images
       ├── detect questions
       ├── classify topics
       └── generate embeddings
```

---

## Job Types

```text
process_document
extract_questions
classify_questions
generate_embeddings
```

Initially these can all be one job.

Later split them if necessary.

---

## Tasks

- [x] Add Redis container
- [x] Add Asynq
- [x] Create worker application
- [x] Create processing queue
- [x] Move PDF processing to worker
- [x] Store job status
- [x] Retry failed jobs
- [x] Prevent duplicate processing
- [x] Add job timeout
- [x] Add progress reporting

---

Completed: 2026-09-23

Verified: `docker-compose.yml` adds `redis:7-alpine` with healthcheck/persistence and `Makefile db-up` starts `db redis`; `go.mod` adds `hibiken/asynq`; `internal/jobs/types.go` defines 4 job types, queued/active/completed/failed statuses (legacy pending normalized), 6 progress steps with representative percents, and retry/timeout defaults; `migrations/008_add_jobs.sql` creates `jobs` table (status/progress/attempts/max_retries/timeout/unique_key) with indexes and updated_at trigger; `internal/jobs/store.go` adds `PostgresStore` + `MemoryStore` with unique_key dedup, claim with stale-heartbeat timeout reclaim, and progress clamping; `internal/jobs/queue.go` + `asynq.go` add `MemoryEnqueuer` and `AsynqEnqueuer` bridge (deterministic TaskID, nil-client persist-only mode) with task envelope build/parse; `internal/jobs/runner.go` adds `Runner` with per-job timeout context, exponential-backoff retry, progress callbacks, structured logging, `ClaimNext` and Redis-free poll loop plus duplicate-delivery skip; `cmd/worker/main.go` runs Asynq server (4 handlers, concurrency 4) when `REDIS_ADDR` set else DB poll loop, executing the full ExtractionService pipeline (extract → questions → classify → embeddings) with vision/LLM/embeddings wired like the API; `internal/http/jobs.go` exposes `POST /api/v1/documents/{id}/process` (202, 409 dedup conflict), `GET /api/v1/jobs/{id}`, `GET /api/v1/documents/{id}/processing-status` (PLAN-style steps checklist with done/active/pending/failed + percent) wired in `internal/http/router.go` and `cmd/api/main.go` (persist-only enqueuer, nil-safe 503s); `lib/api.ts` adds `ProcessingJob`/`ProcessingStatus` types plus `enqueueProcessDocument`/`getJob`/`getProcessingStatus`; `components/ProcessingStatus.tsx` renders the PLAN checklist with progress bar and polls every 2s on `pages/documents/[id].tsx` until terminal state; unit tests cover store dedup/claim/timeout, runner retry/timeout/progress, task envelopes and HTTP handlers; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

## UI

Show:

```text
Processing...

✓ Uploaded
✓ Extracting pages
✓ Extracting questions
✓ Detecting images
→ Classifying topics
○ Generating embeddings

73%
```

---

# 25. Phase 20 — Authentication and User Data

Only implement this after the core system works.

---

## Tasks

- [x] User registration/login
- [x] Sessions
- [x] User-specific documents
- [x] User-specific quizzes
- [x] Authorization middleware
- [x] Prevent access to another user's documents
- [x] User preferences
- [x] Quiz history

---

## Security

- [x] Password hashing if handling passwords directly
- [x] Secure cookies
- [x] CSRF considerations
- [x] File upload validation
- [x] Rate limiting
- [x] Request size limits
- [x] SQL injection prevention
- [x] Path traversal prevention

---

Completed: 2026-09-23

Verified: `internal/auth` adds registration/login with bcrypt hashing (`password.go`, `golang.org/x/crypto`), opaque bearer sessions persisted as SHA-256 hashes with 30-day TTL and lazy expiry revocation (`service.go`, `tokens.go`, `store.go` with Postgres + memory stores), `OptionalAuth`/`RequireAuth` middleware plus `TokenFromRequest` (Bearer precedence, `hg_session` cookie fallback) and `SetSessionCookie`/`ClearSessionCookie` (HttpOnly, SameSite=Lax, Secure in production) in `context.go`, and `CSRFMiddleware` rejecting cookie-authenticated mutations with mismatched Origin/Referer (`csrf.go`); `migrations/009_add_auth.sql` creates `users` (case-insensitive unique email), `sessions`, `user_preferences` and nullable `user_id` ownership on `documents`/`quiz_sessions`; `internal/documents` scopes `List` to owner+legacy (anonymous sees legacy only), masks cross-owner `Get` as not found and attributes uploads (`service.go`, `repository.go` with pre-009 fallback); `internal/quiz` attributes sessions, masks cross-owner reads/writes as not found and adds `ListSessions` history via optional `SessionLister` (`evaluation_store.go`, repo fallback returns empty history pre-009); `internal/http/auth.go` exposes `POST /api/v1/auth/register` (201, 409 dedup), `POST /api/v1/auth/login` (401 without enumeration), `POST /api/v1/auth/logout` (idempotent), `GET /api/v1/auth/me` and `GET/PATCH /api/v1/users/me/preferences`; `internal/http/quiz_history.go` exposes `GET /api/v1/quiz/sessions` (anonymous gets `[]`); image and job-status handlers enforce document ownership; hardening via per-IP fixed-window rate limiting on auth (10/min, 429+Retry-After), 1 MiB JSON body caps (`internal/httpx/body.go`, `ratelimit.go`), existing PDF magic-byte upload validation, parameterized SQL, and filename/path traversal guards; `lib/api.ts` adds `AuthUser`/`AuthSession`/`UserPreferences` plus `registerUser`/`loginUser`/`logoutUser`/`getCurrentUser`/`getPreferences`/`updatePreferences`/`listQuizHistory` (cookie credentials); unit tests cover auth service, CSRF, rate limiting, handler auth/preferences/ownership/history and quiz ownership; `go vet ./...`, `go test ./...` and `npm run build` pass.

---

# 26. Phase 21 — UX and Product Polish

## Dashboard

Show:

```text
Your Library

12 Documents
483 Questions
14 Topics

Recent Quizzes

Deadlocks       82%
Paging          61%
Scheduling      91%
```

---

## Document Page

Show:

```text
Operating Systems PYQs

2021
2022
2023
2024

Questions: 483

Topics:
Deadlocks
Paging
Scheduling
Processes
...
```

---

## Topic Page

Example:

```text
Deadlocks

42 questions

[Easy]   12
[Medium] 20
[Hard]   10

[Start Quiz]
```

---

## Quiz Configuration

Allow:

```text
Topic
Difficulty
Number of questions
Question type
Year range
Only unseen questions
Only incorrect questions
```

---

# 27. Phase 22 — Testing

Testing should happen continuously, but this phase consolidates it.

---

## Go Unit Tests

Test:

- [ ] Question parser
- [ ] Topic normalization
- [ ] Retrieval
- [ ] Metadata filters
- [ ] Quiz generation validation
- [ ] Document validation
- [ ] File path handling
- [ ] Services
- [ ] Repositories

---

## Integration Tests

Test:

```text
HTTP
 ↓
service
 ↓
PostgreSQL
```

---

## Database Tests

- [ ] Fresh migrations
- [ ] Rollbacks
- [ ] Constraints
- [ ] Foreign keys
- [ ] Vector search
- [ ] Seed data

---

## Frontend Tests

Test:

- [ ] Upload flow
- [ ] Document listing
- [ ] Topic selection
- [ ] Quiz interaction
- [ ] Answer submission
- [ ] Quiz completion

---

## End-to-End Test

The most important test:

```text
Upload PDF
    ↓
Process PDF
    ↓
Extract questions
    ↓
Classify topics
    ↓
Generate embeddings
    ↓
Select topic
    ↓
Retrieve questions
    ↓
Generate quiz
    ↓
Answer quiz
    ↓
See results
```

This should eventually be automated.

---

# 28. Phase 23 — Observability and Debugging

AI pipelines are difficult to debug without visibility.

---

## Logging

Every processing step should log:

```text
document_id
job_id
question_id
operation
duration
status
error
```

Example:

```text
document=abc123
operation=question_extraction
questions=87
duration=4.21s
status=success
```

---

## AI Logging

During development, optionally record:

```text
model
prompt version
input tokens
output tokens
latency
error
```

Be careful not to log private user data unnecessarily.

---

## Retrieval Debugging

Create a development endpoint/page:

```text
/debug/retrieval
```

Input:

```text
"questions about deadlock prevention"
```

Output:

```text
Q17
score: 0.91
topic: Deadlock

Q32
score: 0.88
topic: Deadlock Prevention

Q72
score: 0.83
topic: Synchronization
```

This will be extremely useful.

---

# 29. Phase 24 — Performance

Only optimize after correctness.

---

## Database

- [ ] Add appropriate indexes
- [ ] Analyze slow queries
- [ ] Tune vector indexes
- [ ] Avoid N+1 queries
- [ ] Use connection pooling
- [ ] Batch database operations

---

## Embeddings

Instead of:

```text
Question 1 → API call
Question 2 → API call
Question 3 → API call
```

prefer:

```text
Questions 1–100
        ↓
batch embedding
```

---

## Processing

- [ ] Process pages concurrently where safe
- [ ] Batch LLM calls where appropriate
- [ ] Avoid reprocessing unchanged documents
- [ ] Cache embeddings
- [ ] Cache expensive AI operations
- [ ] Track processing duration

---

# 30. Phase 25 — Proper Docker Build

This is the final major infrastructure milestone.

Up to this point, Docker Compose should primarily be used for local infrastructure.

Now containerize the actual applications.

---

# 30.1 Backend Dockerfile

Create a production-grade multi-stage Docker build.

Conceptually:

```text
Go source
   ↓
builder image
   ↓
go build
   ↓
minimal runtime image
```

Tasks:

- [ ] Multi-stage build
- [ ] Build static binary
- [ ] Minimal runtime image
- [ ] Non-root user
- [ ] No unnecessary packages
- [ ] Proper signal handling
- [ ] Healthcheck
- [ ] Environment-based configuration

---

# 30.2 Frontend Dockerfile

Build Next.js separately.

Tasks:

- [ ] Multi-stage build
- [ ] Production dependencies only
- [ ] Next.js standalone output if appropriate
- [ ] Non-root runtime
- [ ] Environment configuration
- [ ] Healthcheck

---

# 30.3 Worker Dockerfile

If worker is separate:

```text
worker/
└── Dockerfile
```

Tasks:

- [ ] Production Go build
- [ ] Minimal image
- [ ] Non-root user
- [ ] Environment configuration

---

# 30.4 Final Docker Compose

Eventually:

```text
docker compose
│
├── postgres
│
├── redis
│
├── api
│
├── worker
│
└── web
```

Example conceptual dependency graph:

```text
                 ┌────────────┐
                 │    web     │
                 └─────┬──────┘
                       │
                       ▼
                 ┌────────────┐
                 │    api     │
                 └──┬─────┬───┘
                    │     │
                    ▼     ▼
              PostgreSQL  Redis
                           │
                           ▼
                        Worker
                           │
                           ▼
                         AI
```

---

# 30.5 Docker Production Checklist

- [ ] Multi-stage builds
- [ ] `.dockerignore`
- [ ] Minimal images
- [ ] Non-root containers
- [ ] Healthchecks
- [ ] Proper container networking
- [ ] Environment variables
- [ ] Secrets not committed
- [ ] Persistent PostgreSQL volume
- [ ] Redis persistence strategy if needed
- [ ] Graceful shutdown
- [ ] Restart policies
- [ ] Resource limits
- [ ] Log handling
- [ ] API healthcheck
- [ ] Frontend healthcheck
- [ ] Worker healthcheck

---

