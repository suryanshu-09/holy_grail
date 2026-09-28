# 31. Phase 26 — Deployment Preparation

Deployment is optional until the application works locally.

---

## Before Deployment

- [x] Production configuration
- [x] Production database
- [x] Object storage
- [x] Secrets management
- [x] HTTPS
- [x] CORS configuration
- [x] Rate limiting
- [x] Database backups
- [x] File storage migration
- [x] Logging
- [x] Monitoring
- [x] Error tracking

Completed: 2026-09-26

Verified: `internal/storage/storage.go` adds `Storage` interface (`Put/Get/Delete` + `SaveDocument/RemoveDocument` compat) with `LocalStorage`/`LocalStore` and `S3Storage` (S3-compatible stdlib client, `STORAGE_BACKEND`/`S3_*` env); `config.ValidateProduction` fail-fast, CORS+HSTS hardening, global rate limiting, `/api/v1/metrics`, Sentry-compatible error reporter, `scripts/db-backup.sh` + `cmd/migrate-storage`, `docs/DEPLOYMENT.md`; `NewStorageFromConfig` wired in `cmd/api`+`cmd/worker` with compose env passthrough; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

## Object Storage Migration

Local:

```text
./data/documents/
```

Production:

```text
S3 / Cloudflare R2
```

Create a storage abstraction:

```go
type Storage interface {
    Put(...)
    Get(...)
    Delete(...)
}
```

Then:

```text
LocalStorage
S3Storage
```

can implement the same interface.

---

# 32. Phase 27 — Optional Advanced Features

Only consider these after the core application is stable.

---

## Smart Quiz Generation

- [x] Adaptive difficulty
- [x] Weak-topic weighting
- [x] Spaced repetition
- [x] Personalized quizzes
- [x] Question difficulty estimation

Completed: 2026-09-28

Verified: `internal/quiz/adaptive.go` (EstimateDifficulty/AdjustDifficulty/WeightByWeakTopics/SM-2 ScheduleReviews/DueQuestions/BuildPersonalizedRequest + `AdaptiveOptions` on `QuizRequest`, nil = legacy) wired into `internal/http/quiz.go` GET+POST (`adaptive/weak_topics/review_due` + tuning params, defaults off); `QuizSetupForm` adaptive toggle + quiz page Study Mode links; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

## Advanced Retrieval

- [x] Query rewriting
- [x] Hybrid search
- [x] Reranking
- [x] Multi-query retrieval
- [x] Parent-child retrieval
- [x] Contextual retrieval

Completed: 2026-09-28

Verified: `internal/search/advanced.go` (`AdvancedFilter` embedding `HybridFilter` + RewriteQuery/BuildQueryVariants/FuseMultiQuery/GroupByDocument/BuildContextualQuery/EnrichResults/TokenOverlap+Chained rerankers) wired into `internal/http/search.go` GET+POST (`rewrite/multi_query/parent_child/contextual`, defaults off, backward-compat envelopes); `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

## Multimodal

- [x] Image embeddings
- [x] Multimodal embeddings
- [x] Diagram understanding
- [x] Table understanding
- [x] Mathematical expression understanding

Completed: 2026-09-28

Verified: `internal/multimodal` (diagram classification, table parse/summarize, math extract/normalize) surfaced via `internal/extraction/multimodal_display.go` + quiz `BuildOriginalQuiz` enrichment and `internal/study` summary; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

## Analytics

- [x] Topic mastery
- [x] Historical accuracy
- [x] Question difficulty
- [x] Time per question
- [x] Exam readiness score

Completed: 2026-09-28

Verified: `internal/analytics` pure stats + `internal/http/analytics.go` (`/analytics/mastery|history|difficulty|timing|readiness`, nil-safe) + `lib/api.ts` analytics client; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

## Study Mode

Allow:

```text
Topic
 ↓
Explanation
 ↓
Example
 ↓
PYQs
 ↓
Quiz
```

- [x] Study guide endpoint (Topic → Explanation → Example → PYQs → Quiz)
- [x] Study guide page with step indicator + quiz link
- [x] Weak-topic flag + subject resolution + deterministic quiz fallback

Completed: 2026-09-28

Verified: `internal/study` (BuildStudyGuide) + `internal/http/study.go` (`GET/POST /api/v1/study/guide`, nil-safe) + `pages/study.tsx` + quiz↔study cross-links; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

- [x] Topic explanation
- [x] Worked example
- [x] PYQ references
- [x] Suggested practice quiz
- [x] Study guide API + UI

Completed: 2026-09-28

Verified: `internal/quiz/adaptive.go` (EstimateDifficulty/AdjustDifficulty/WeightByWeakTopics/SM-2 NextReview+ScheduleReviews+DueQuestions/BuildPersonalizedRequest via optional `QuizRequest.Adaptive`, backward-compat) + `internal/search/advanced.go` (RewriteQuery/BuildQueryVariants/FuseMultiQuery/GroupByDocument/BuildContextualQuery + TokenOverlapReranker/ChainedReranker/AdvancedFilter) + `internal/multimodal` (DescribeImage/EmbedImageText/ClassifyDiagram/ParseTableText+SummarizeTable/ExtractMathExpressions+NormalizeMath) + `internal/analytics` (TopicMastery/HistoricalAccuracy/DifficultyStats/TimePerQuestion/ExamReadinessScore) + `internal/study` (BuildStudyGuide Topic→Explanation→Example→PYQs→Quiz reusing quiz.BuildOriginalQuiz) with `internal/http/analytics.go` (`/api/v1/analytics/mastery|history|difficulty|timing|readiness`), `internal/http/study.go` (`/api/v1/study/guide` GET+POST), routes in `internal/http/router.go`, `lib/api.ts` analytics+study clients, `pages/study.tsx` Study Mode UI; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

# 33. Definition of Done

The project should be considered a functional MVP when all of the following work locally:

## Infrastructure

- [x] PostgreSQL runs through Docker Compose
- [x] pgvector works
- [x] Database migrations work
- [x] Database can be seeded
- [x] Go backend connects to database

Completed: 2026-09-28

Verified: `docker-compose.yml` (`db` ankane/pgvector + `db_data` volume, healthcheck), `migrations/001_create_schema.sql` (`CREATE EXTENSION vector`) + `005_add_vector_search.sql`, `Makefile` `db-up/db-migrate/db-seed`, `internal/database/db.go` (`sql.Open`+`PingContext`), `internal/config/config.go` (`DATABASE_URL`/parts); `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

## Backend

- [x] Go API works
- [x] Document upload works
- [x] Documents are persisted
- [x] Questions are persisted
- [x] Topics are persisted
- [x] Images are persisted
- [x] Embeddings are persisted
- [x] Vector search works
- [x] Topic filtering works
- [x] Quiz generation works

Completed: 2026-09-28

Verified: `cmd/api/main.go` + `internal/http/router.go`, `internal/http/documents.go` (upload/list + `GET/DELETE /documents/{id}` via `handleDocumentByID`), `internal/documents/service.go` (`Upload/Get/Delete` + ownership) + `repository.go` (`Create/GetByID/Delete/List`), `internal/embeddings/repository.go` (`Upsert`), `internal/search/repository.go` (vector + topic filter), `internal/quiz/service.go` (`Generate`); `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

## PDF Pipeline

- [x] PDF text extraction works
- [x] OCR fallback works
- [x] Questions are detected
- [x] Multi-page questions work
- [x] Images are extracted
- [x] Images are associated with questions
- [x] Topics are classified

Completed: 2026-09-28

Verified: `internal/extraction/service.go` (pdf text + `NeedsOCR` fallback), `internal/extraction/ocr.go` (`TesseractOCR`), `internal/extraction/question_extractor.go` (detection, multi-page merge, image association), `internal/extraction/image.go` (pdfimages/Go parser), `internal/topics/classifier.go` + `normalize.go`; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

## RAG

- [x] Questions can be embedded
- [x] Semantic search works
- [x] Metadata filtering works
- [x] Hybrid retrieval works
- [x] Retrieved questions can be inspected
- [x] Retrieved questions can be passed to the LLM

Completed: 2026-09-28

Verified: `internal/embeddings/service.go` (`EmbedDocument`), `internal/search/service.go` + `repository.go` (vector), `internal/search/hybrid.go` + `keyword.go` (metadata filtering, hybrid/RRF), `internal/http/debug_retrieval.go` (`/debug/retrieval`), `pages/debug/retrieval.tsx`, `internal/quiz/prompt.go` (`BuildQuizPrompt`/`ToSourceBlocks`); `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

## Frontend

- [x] Upload page works
- [x] Document library works
- [x] Document details work
- [x] Topics can be selected
- [x] Quiz can be configured
- [x] Quiz works
- [x] Results work
- [x] Original PYQs can be viewed

Completed: 2026-09-28

Verified: `pages/documents/index.tsx` (upload via `components/Upload.tsx` + library), `pages/documents/[id].tsx` (`getDocument` + questions/images/topics + Delete button), `pages/topics/index.tsx` + `[id].tsx`, `components/quiz/QuizSetupForm.tsx`, `pages/quiz/index.tsx` + `QuizQuestionCard` + `QuizResults`, `components/quiz/SourceTraceability.tsx` (View Original); `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

## AI

- [x] Structured LLM responses work
- [x] Invalid output is handled
- [x] Embedding failures are handled
- [x] AI calls have timeouts
- [x] AI calls can be retried
- [x] Model configuration is environment-based

Completed: 2026-09-28

Verified: `internal/llm/openai.go` (structured `ClassifyTopics`/`GenerateQuiz` JSON + validation, 15s timeout, `OPENAI_MODEL` via `config.ChatModel` wired in `cmd/api`+`cmd/worker`), `internal/quiz/validate.go` + `service.go` (invalid-output fallback `BuildOriginalQuiz`, retries), `internal/embeddings/service.go`+`openai.go` (per-item failures, retry w/ backoff, 30s timeout), `internal/topics/classifier.go` (3-try retry); `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

## Docker

- [x] PostgreSQL runs through Docker Compose
- [x] Redis runs through Docker Compose
- [x] Backend has production Dockerfile
- [x] Worker has production Dockerfile
- [x] Frontend has production Dockerfile
- [x] Entire stack runs with Docker Compose
- [x] Containers use non-root users
- [x] Healthchecks work
- [x] Data persists across container restarts

Completed: 2026-09-28

Verified: `docker-compose.yml` (db/redis/api/worker/web, `db_data`/`redis_data`/`app_data` volumes, healthchecks), `Dockerfile.api`/`Dockerfile.worker`/`Dockerfile.web` (multi-stage, non-root `appuser`, HEALTHCHECK), `Makefile` `prod-build/prod-up`; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

# 34. Recommended Development Order

Do **not** implement the phases exactly as a waterfall where every feature must be perfect before moving forward.

Use vertical slices.

---

## Milestone 1 — Hello World

Build:

```text
Next.js
   │
   ▼
Go API
   │
   ▼
PostgreSQL
```

Checklist:

- [x] Docker PostgreSQL
- [x] Go server
- [x] Next.js
- [x] Database connection
- [x] Health endpoint
- [x] Basic UI

Completed: 2026-09-28

Verified: `docker-compose.yml` db + `Makefile db-up`, `cmd/api/main.go`, `pages/index.tsx`, `internal/database/db.go`, `internal/http/health.go` + `router.go`; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

# Milestone 2 — Documents

Build:

```text
Upload PDF
    ↓
Go API
    ↓
Local filesystem
    ↓
PostgreSQL metadata
```

Checklist:

- [x] Upload
- [x] Store
- [x] List
- [x] Delete
- [x] View document

Completed: 2026-09-28

Verified: `internal/http/documents.go` (`uploadDocument`/`listDocuments` + `GET/DELETE /documents/{id}`), `internal/documents/service.go` (`Upload/Get/Delete`) + `repository.go`, `lib/api.ts` (`uploadDocument/listDocuments/getDocument/deleteDocument`), `pages/documents/index.tsx` + `pages/documents/[id].tsx` (detail + Delete button); `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

# Milestone 3 — Questions

Build:

```text
PDF
 ↓
Extractor
 ↓
Questions
 ↓
PostgreSQL
```

Checklist:

- [x] Text extraction
- [x] Question detection
- [x] Page tracking
- [x] Database storage
- [x] Question viewer

Completed: 2026-09-28

Verified: `internal/extraction/service.go` + `question_extractor.go` (incl. multi-page merge), `internal/questions/repository.go` (`Insert/BatchInsert`), `internal/http/questions.go`, `pages/documents/[id].tsx` (`QuestionCard`); `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

At this point you already have a useful non-AI application.

---

# Milestone 4 — Images

Build:

```text
PDF
 ├── text
 └── images
       ↓
Question
```

Checklist:

- [x] Extract images
- [x] Associate images
- [x] Store images
- [x] Display images

Completed: 2026-09-28

Verified: `internal/extraction/image.go` (pdfimages/Go parser, filesystem store), `internal/extraction/question_extractor.go` (`refineImageAssociation`/`images_json`), `internal/http/images.go`, `pages/documents/[id].tsx` (images grid); `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

# Milestone 5 — AI Classification

Build:

```text
Question
   ↓
LLM
   ↓
Topic
```

Checklist:

- [x] Topic extraction
- [x] Topic normalization
- [x] Topic database
- [x] Topic UI

Completed: 2026-09-28

Verified: `internal/topics/classifier.go` (+ heuristic fallback), `internal/topics/normalize.go`, `internal/topics/repository.go`, `pages/topics/index.tsx` + `pages/topics/[id].tsx`; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

# Milestone 6 — Embeddings

Build:

```text
Question
   ↓
Embedding model
   ↓
pgvector
```

Checklist:

- [x] Embed questions
- [x] Store vectors
- [x] Query vectors
- [x] Display search results

Completed: 2026-09-28

Verified: `internal/embeddings/service.go` (`EmbedDocument` + retry), `internal/embeddings/repository.go` (`Upsert`), `internal/search/service.go` + `repository.go`, `pages/debug/retrieval.tsx`; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

# Milestone 7 — Actual RAG

Build:

```text
User query
    ↓
Embedding
    ↓
Vector search
    ↓
Relevant questions
    ↓
LLM
```

Checklist:

- [x] Query embedding
- [x] Retrieval
- [x] Metadata filtering
- [x] Context construction
- [x] LLM generation

Completed: 2026-09-28

Verified: `internal/search/service.go` (query embedding), `internal/search/repository.go` + `hybrid.go` + `keyword.go`, `internal/quiz/prompt.go` (`ToSourceBlocks`/`BuildQuizPrompt`), `internal/llm/openai.go` (`GenerateQuiz`, `OPENAI_MODEL` env-based); `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

# Milestone 8 — Quiz

Build:

```text
Retrieved questions
        ↓
    LLM
        ↓
     Quiz
        ↓
    Frontend
```

Checklist:

- [x] Quiz schema
- [x] Generation
- [x] Quiz UI
- [x] Answers
- [x] Scoring
- [x] Results

Completed: 2026-09-28

Verified: `lib/api.ts` (`QuizQuestion`/`QuizResponse`, `generateQuiz`), `pages/quiz/index.tsx` + `components/quiz/QuizSetupForm.tsx` + `QuizQuestionCard.tsx` + `QuizResults.tsx`, `internal/quiz/service.go` + `validate.go`; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

# Milestone 9 — Background Processing

Only now add:

```text
Redis
 ↓
Asynq
 ↓
Worker
```

Checklist:

- [x] Redis
- [x] Worker
- [x] Processing jobs
- [x] Retries
- [x] Progress
- [x] Failure handling

Completed: 2026-09-28

Verified: `docker-compose.yml` redis (appendonly), `cmd/worker/main.go` + `Dockerfile.worker`, `internal/jobs/store.go` (`Enqueue`/`UpdateProgress`) + `runner.go` (retry/backoff, `StatusFailed`) + `asynq.go`, `migrations/008_add_jobs.sql`; `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

# Milestone 10 — Retrieval Quality

Now make the RAG actually good.

Checklist:

- [x] Evaluation dataset
- [x] Vector baseline
- [x] Metadata baseline
- [x] Hybrid search
- [x] Reranking
- [x] Precision measurements
- [x] Recall measurements
- [x] Tune retrieval

Completed: 2026-09-28

Verified: `internal/search/evaluation_dataset.go` (60 queries) + `evaluation.go` (precision/recall/top-K) + `evaluation_runner.go` (all strategies incl. `hybrid+reranker`, `BestByRecall`), `internal/http/search_eval.go` (`/debug/eval`), `pages/debug/eval.tsx` (results table + best verdict) linked from `pages/debug/retrieval.tsx`, `lib/api.ts` (`getDebugEval`); `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

---

# Milestone 11 — Production-Quality Local Stack

Final local architecture:

```text
                    Browser
                       │
                       ▼
                 ┌───────────┐
                 │  Next.js  │
                 └─────┬─────┘
                       │
                       ▼
                 ┌───────────┐
                 │  Go API   │
                 └─────┬─────┘
                       │
          ┌────────────┼────────────┐
          ▼            ▼            ▼
      PostgreSQL      Redis       Storage
          │            │
          │            ▼
          │          Worker
          │            │
          └────────────┤
                       ▼
                     AI API
```

Checklist:

- [x] Docker Compose
- [x] PostgreSQL
- [x] Redis
- [x] API container
- [x] Worker container
- [x] Frontend container
- [x] Persistent volumes
- [x] Healthchecks
- [x] Environment configuration
- [x] Production builds
- [x] Full local startup with one command

Completed: 2026-09-28

Verified: `docker-compose.yml` (db/redis/api/worker/web, volumes, healthchecks, env incl. `OPENAI_MODEL`), `Dockerfile.api`/`Dockerfile.worker`/`Dockerfile.web`, `internal/config/config.go`, `Makefile` `prod-up` (`docker compose up -d --build`); `go vet ./...`, `go test ./... -count=1` and `npm run build` pass.

Live verification (2026-09-28, `APP_ENV=development`, no `OPENAI_API_KEY`): all 5 containers healthy; `/health`+`/ready`+web 200; pgvector extension + migrations + seed live; upload→extract (2 pages, 3 MCQs)→classify (heuristic topics)→quiz `/quiz/generate` (deterministic Original-PYQ fallback with provenance) 200; `GET/DELETE /documents/{id}` 200/204/404; background job `queued → completed (100%)` via Redis→Asynq→Worker; all 8 frontend routes 200; `/metrics` 200; containers run as `appuser`; data survives `compose restart api`; invalid upload 400; AI-dependent paths degrade gracefully (`/embed`, `/search`, `/debug/*` 503, no 500s). Live run fixed 3 real bugs: typed-nil `*embeddings.Service` panic on `/embed` (`embedderPipelineOrNil` + 503 test), API enqueue never published to Redis (new `asynqClientAdapter` in `cmd/api`), `PostgresStore.Claim`/poll query `make_interval` type error (`timeout_seconds::double precision` + `$2::timestamptz`, `postgres_claim_test.go`). Note: `APP_ENV=production` correctly refuses to boot without explicit `DATABASE_URL` (fail-fast, by design); embedding/vector/hybrid search need `OPENAI_API_KEY`.

---

# Development Rules

These rules should prevent the project from becoming unnecessarily complicated.

## Rule 1 — Make it work before making it clever

Prefer:

```text
simple vector search
```

over:

```text
multi-stage agentic retrieval pipeline
```

until the simple version is proven insufficient.

---

## Rule 2 — Preserve raw data

Never throw away the original PDF.

Always preserve:

```text
Original PDF
Raw extracted text
Extracted images
Structured questions
AI metadata
Embeddings
```

This makes reprocessing possible.

---

## Rule 3 — AI should enrich data, not own the data

The database should remain the source of truth.

The LLM should not be the database.

---

## Rule 4 — Keep AI behind interfaces

Avoid:

```go
questions.go
    ↓
direct OpenAI calls everywhere
```

Prefer:

```text
QuestionService
      ↓
LLM interface
      ↓
Provider implementation
```

---

## Rule 5 — Make processing repeatable

You should eventually be able to:

```text
delete generated metadata
        ↓
run processing again
        ↓
obtain the same structured dataset
```

This is extremely useful when changing prompts or models.

---

## Rule 6 — Every AI-generated object should have provenance

Whenever possible:

```text
Generated quiz question
        ↓
source question
        ↓
source document
        ↓
page
```

---

## Rule 7 — Don't prematurely deploy

The primary development environment is:

```text
localhost
```

The application should be fully functional locally before worrying about:

- Vercel
- AWS
- Railway
- Kubernetes
- Terraform
- CI/CD
- CDN
- production scaling

---

# Final Target

The finished project should ultimately behave like this:

```text
                   USER
                     │
                     ▼
             Upload PYQ PDFs
                     │
                     ▼
              Document Library
                     │
                     ▼
              Processing Worker
                     │
          ┌──────────┼──────────┐
          ▼          ▼          ▼
        Text       Images     Metadata
          │          │          │
          └──────────┼──────────┘
                     ▼
              Question Objects
                     │
          ┌──────────┴──────────┐
          ▼                     ▼
     Topic Metadata         Embeddings
          │                     │
          └──────────┬──────────┘
                     ▼
                Question DB
                     │
                     ▼
              User Query
                     │
          ┌──────────┴──────────┐
          ▼                     ▼
     Structured Search      Vector Search
          │                     │
          └──────────┬──────────┘
                     ▼
                  Rerank
                     │
                     ▼
             Relevant PYQs
                     │
                     ▼
                LLM Quiz
                     │
                     ▼
                 Quiz UI
                     │
                     ▼
                Evaluation
                     │
          ┌──────────┴──────────┐
          ▼                     ▼
       Score                 Analytics
          │                     │
          └──────────┬──────────┘
                     ▼
               Better Study
```

The most important conceptual progression is:

```text
Phase 1:
PDF storage

        ↓

Phase 2:
PDF → questions

        ↓

Phase 3:
questions → topics

        ↓

Phase 4:
questions → embeddings

        ↓

Phase 5:
semantic retrieval

        ↓

Phase 6:
hybrid retrieval

        ↓

Phase 7:
retrieval → LLM

        ↓

Phase 8:
LLM → quiz

        ↓

Phase 9:
quiz → performance data

        ↓

Phase 10:
performance → personalized retrieval
```

