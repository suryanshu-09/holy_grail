# 31. Phase 26 — Deployment Preparation

Deployment is optional until the application works locally.

---

## Before Deployment

- [ ] Production configuration
- [ ] Production database
- [ ] Object storage
- [ ] Secrets management
- [ ] HTTPS
- [ ] CORS configuration
- [ ] Rate limiting
- [ ] Database backups
- [ ] File storage migration
- [ ] Logging
- [ ] Monitoring
- [ ] Error tracking

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

- [ ] Adaptive difficulty
- [ ] Weak-topic weighting
- [ ] Spaced repetition
- [ ] Personalized quizzes
- [ ] Question difficulty estimation

---

## Advanced Retrieval

- [ ] Query rewriting
- [ ] Hybrid search
- [ ] Reranking
- [ ] Multi-query retrieval
- [ ] Parent-child retrieval
- [ ] Contextual retrieval

---

## Multimodal

- [ ] Image embeddings
- [ ] Multimodal embeddings
- [ ] Diagram understanding
- [ ] Table understanding
- [ ] Mathematical expression understanding

---

## Analytics

- [ ] Topic mastery
- [ ] Historical accuracy
- [ ] Question difficulty
- [ ] Time per question
- [ ] Exam readiness score

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

---

# 33. Definition of Done

The project should be considered a functional MVP when all of the following work locally:

## Infrastructure

- [ ] PostgreSQL runs through Docker Compose
- [ ] pgvector works
- [ ] Database migrations work
- [ ] Database can be seeded
- [ ] Go backend connects to database

---

## Backend

- [ ] Go API works
- [ ] Document upload works
- [ ] Documents are persisted
- [ ] Questions are persisted
- [ ] Topics are persisted
- [ ] Images are persisted
- [ ] Embeddings are persisted
- [ ] Vector search works
- [ ] Topic filtering works
- [ ] Quiz generation works

---

## PDF Pipeline

- [ ] PDF text extraction works
- [ ] OCR fallback works
- [ ] Questions are detected
- [ ] Multi-page questions work
- [ ] Images are extracted
- [ ] Images are associated with questions
- [ ] Topics are classified

---

## RAG

- [ ] Questions can be embedded
- [ ] Semantic search works
- [ ] Metadata filtering works
- [ ] Hybrid retrieval works
- [ ] Retrieved questions can be inspected
- [ ] Retrieved questions can be passed to the LLM

---

## Frontend

- [ ] Upload page works
- [ ] Document library works
- [ ] Document details work
- [ ] Topics can be selected
- [ ] Quiz can be configured
- [ ] Quiz works
- [ ] Results work
- [ ] Original PYQs can be viewed

---

## AI

- [ ] Structured LLM responses work
- [ ] Invalid output is handled
- [ ] Embedding failures are handled
- [ ] AI calls have timeouts
- [ ] AI calls can be retried
- [ ] Model configuration is environment-based

---

## Docker

- [ ] PostgreSQL runs through Docker Compose
- [ ] Redis runs through Docker Compose
- [ ] Backend has production Dockerfile
- [ ] Worker has production Dockerfile
- [ ] Frontend has production Dockerfile
- [ ] Entire stack runs with Docker Compose
- [ ] Containers use non-root users
- [ ] Healthchecks work
- [ ] Data persists across container restarts

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

- [ ] Docker PostgreSQL
- [ ] Go server
- [ ] Next.js
- [ ] Database connection
- [ ] Health endpoint
- [ ] Basic UI

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

- [ ] Upload
- [ ] Store
- [ ] List
- [ ] Delete
- [ ] View document

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

- [ ] Text extraction
- [ ] Question detection
- [ ] Page tracking
- [ ] Database storage
- [ ] Question viewer

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

- [ ] Extract images
- [ ] Associate images
- [ ] Store images
- [ ] Display images

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

- [ ] Topic extraction
- [ ] Topic normalization
- [ ] Topic database
- [ ] Topic UI

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

- [ ] Embed questions
- [ ] Store vectors
- [ ] Query vectors
- [ ] Display search results

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

- [ ] Query embedding
- [ ] Retrieval
- [ ] Metadata filtering
- [ ] Context construction
- [ ] LLM generation

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

- [ ] Quiz schema
- [ ] Generation
- [ ] Quiz UI
- [ ] Answers
- [ ] Scoring
- [ ] Results

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

- [ ] Redis
- [ ] Worker
- [ ] Processing jobs
- [ ] Retries
- [ ] Progress
- [ ] Failure handling

---

# Milestone 10 — Retrieval Quality

Now make the RAG actually good.

Checklist:

- [ ] Evaluation dataset
- [ ] Vector baseline
- [ ] Metadata baseline
- [ ] Hybrid search
- [ ] Reranking
- [ ] Precision measurements
- [ ] Recall measurements
- [ ] Tune retrieval

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

- [ ] Docker Compose
- [ ] PostgreSQL
- [ ] Redis
- [ ] API container
- [ ] Worker container
- [ ] Frontend container
- [ ] Persistent volumes
- [ ] Healthchecks
- [ ] Environment configuration
- [ ] Production builds
- [ ] Full local startup with one command

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

