# 1. Project Vision

## 1.1 Core Problem

Users have large collections of PYQ PDFs and want to study specific topics without manually searching through hundreds of pages.

Example:

> Upload 20 Operating Systems PYQ PDFs.

Then:

> Select `Deadlocks`.

The application should find relevant questions and generate a quiz.

The system should preserve:

- Original question text
- Question number
- Source document
- Page number
- Year
- Subject
- Topic
- Associated images
- Original question formatting where possible

---

## 1.2 Core User Flow

```text
User
 │
 ├── Upload PYQ PDFs
 │
 ▼
Document Processing
 │
 ├── Extract text
 ├── Extract images
 ├── Detect questions
 ├── Classify topics
 └── Generate embeddings
 │
 ▼
Question Database
 │
 ▼
User selects topic
 │
 ▼
Retrieval
 │
 ├── Metadata filtering
 ├── Semantic search
 └── Optional reranking
 │
 ▼
Relevant Questions
 │
 ▼
LLM Quiz Generation
 │
 ▼
Quiz
 │
 ├── Answer
 ├── Get result
 ├── View explanation
 └── View original PYQ
```

---


## Architecture Reference

Architecture, stack, and repository layout were moved to `ARCHITECTURE.md`.

# 5. Phase 0 — Project Setup (Completed)

## Goal

Create a clean repository with the frontend, backend, database, and development workflow.

---

## Tasks

### Repository

- [x] Create Git repository
- [x] Create `README.md`
- [x] Create `PLAN.md`
- [x] Create `.gitignore`
- [x] Create `.env.example`
- [x] Create `Makefile`
- [x] Establish directory structure

### Go

- [x] Initialize Go module
- [x] Create `cmd/api`
- [x] Create `internal/`
- [x] Create configuration package
- [x] Create logging setup
- [x] Create basic HTTP server
- [x] Add health endpoint

Example:

```text
GET /health
```

Expected:

```json
{
  "status": "ok"
}
```

### Next.js

- [x] Initialize Next.js application
- [x] Configure TypeScript
- [x] Configure Tailwind
- [x] Configure shadcn/ui
- [x] Create base layout
- [x] Create home page
- [x] Create API client layer

### Docker

At this stage Docker Compose should contain **only PostgreSQL**.

- [x] Create `docker-compose.yml`
- [x] Add PostgreSQL + pgvector
- [x] Add persistent volume
- [x] Add healthcheck
- [x] Configure database environment variables

Example architecture:

```text
docker compose
└── postgres
```

Do not add Redis yet.

---

## Completion Criteria

- [x] `docker compose up` starts PostgreSQL
- [x] Go API starts locally
- [x] Next.js starts locally
- [x] Frontend can reach Go `/health`
- [x] Git repository is cleanly organized

Completed: 2026-08-19T12:09:02+05:30

Verified: all Phase 0 checklist items present and services started (Postgres with pgvector via docker compose, Next.js dev server, Go API with /health).

---

# 6. Phase 1 — Database Foundation

## Goal

Create the database schema and migration system.

---

## Initial Entities

Start with:

```text
documents
questions
topics
question_topics
question_images
embeddings
```

Later:

```text
users
quiz_sessions
quiz_questions
quiz_answers
```

---

## Documents

Fields:

```text
id
filename
original_filename
storage_path
subject
year
status
created_at
updated_at
```

Possible statuses:

```text
uploaded
processing
processed
failed
```

---

## Questions

Fields:

```text
id
document_id
question_number
question_text
page_number
year
subject
difficulty
created_at
updated_at
```

---

## Topics

```text
id
name
subject
created_at
```

---

## Question Topics

Many-to-many relationship:

```text
question_id
topic_id
```

A question can belong to multiple topics.

Example:

```text
Question 17
├── Deadlock
├── Resource Allocation
└── Operating Systems
```

---

## Images

```text
id
question_id
storage_path
page_number
image_index
caption
created_at
```

---

## Embeddings

If using pgvector:

```text
question_id
embedding
model
created_at
```

The embedding dimension depends on the embedding model.

Do not hard-code a dimension until the embedding model has been chosen.

---

## Tasks

- [x] Design ER diagram
- [x] Create migrations
- [x] Enable pgvector
- [x] Create indexes
- [x] Create foreign keys
- [x] Create uniqueness constraints
- [x] Create timestamps
- [x] Create migration rollback support
- [x] Test clean database creation
- [x] Test migration rollback
- [x] Add seed data

---

## Completion Criteria

You can run:

```bash
make db-up
make db-migrate
make db-seed
```

and get a fully usable development database.

---

# 7. Phase 2 — Go Backend Foundation

## Goal

Build a clean backend architecture before adding AI functionality.

---

## Suggested Structure

```text
backend/
├── cmd/
│   └── api/
│       └── main.go
│
├── internal/
│   ├── config/
│   ├── http/
│   ├── database/
│   ├── documents/
│   ├── questions/
│   ├── topics/
│   ├── quizzes/
│   ├── ai/
│   └── storage/
│
└── migrations/
```

---

## Layers

Prefer:

```text
HTTP Handler
     ↓
Service
     ↓
Repository
     ↓
Database
```

Example:

```text
QuestionHandler
      ↓
QuestionService
      ↓
QuestionRepository
      ↓
PostgreSQL
```

Don't allow HTTP handlers to directly contain SQL/business logic.

---

## Tasks

- [ ] HTTP server
- [ ] Configuration
- [ ] Database connection pool
- [ ] Graceful shutdown
- [ ] Request logging
- [ ] Error handling
- [ ] JSON response helpers
- [ ] Request validation
- [ ] CORS configuration
- [ ] Health endpoint
- [ ] Readiness endpoint
- [ ] Basic API versioning

Suggested API prefix:

```text
/api/v1
```

---

## Initial Endpoints

```text
GET /api/v1/health

GET /api/v1/documents

GET /api/v1/questions

GET /api/v1/topics
```

---

## Completion Criteria

The Go backend can:

- [ ] Connect to PostgreSQL
- [ ] Query data
- [ ] Return JSON
- [ ] Handle errors consistently
- [ ] Shut down gracefully

---
