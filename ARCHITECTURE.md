# 2. Final Architecture

The target architecture is:

```text
                         ┌─────────────────┐
                         │    Browser      │
                         └────────┬────────┘
                                  │
                                  │ HTTP
                                  ▼
                         ┌─────────────────┐
                         │    Next.js      │
                         │    Frontend     │
                         └────────┬────────┘
                                  │
                                  │ REST API
                                  ▼
                         ┌─────────────────┐
                         │      Go         │
                         │     API         │
                         └────────┬────────┘
                                  │
              ┌───────────────────┼──────────────────┐
              │                   │                  │
              ▼                   ▼                  ▼
       ┌─────────────┐     ┌─────────────┐    ┌─────────────┐
       │ PostgreSQL  │     │    Redis    │    │ Object      │
       │ + pgvector  │     │             │    │ Storage     │
       └─────────────┘     └─────────────┘    └─────────────┘
                                  │
                                  ▼
                            ┌───────────┐
                            │  Worker   │
                            │   Go /    │
                            │  Python   │
                            └─────┬─────┘
                                  │
                                  ▼
                           ┌─────────────┐
                           │ AI APIs /   │
                           │ Local AI    │
                           └─────────────┘
```

However, this is **not** the architecture to build on day one.

---

# 3. Technology Stack

## Frontend

- [ ] Next.js
- [ ] TypeScript
- [ ] React
- [ ] Tailwind CSS
- [ ] shadcn/ui
- [ ] TanStack Query or equivalent data-fetching layer
- [ ] Zod for client-side/API validation where useful

---

## Backend

- [ ] Go
- [ ] Chi / Gin / Echo
- [ ] `pgx` for PostgreSQL
- [ ] `sqlc` or handwritten SQL
- [ ] `golang-migrate` or equivalent migration system
- [ ] `slog` for structured logging
- [ ] REST API initially

Prefer keeping the Go backend relatively framework-light.

---

## Database

- [ ] PostgreSQL
- [ ] pgvector extension

PostgreSQL should handle:

- Relational data
- Metadata
- Questions
- Documents
- Topics
- Quiz sessions
- Embeddings

Do not introduce a dedicated vector database initially.

---

## Local Infrastructure

Initially:

```text
Docker Compose
└── PostgreSQL + pgvector
```

Later:

```text
Docker Compose
├── PostgreSQL + pgvector
└── Redis
```

Eventually:

```text
Docker Compose
├── PostgreSQL
├── Redis
├── Go API
├── Go worker
├── Next.js
└── optional supporting services
```

Do not containerize everything at the beginning.

---

## AI

The system should be designed behind an internal AI abstraction.

Possible capabilities:

- [ ] Text generation
- [ ] Structured JSON generation
- [ ] Embeddings
- [ ] Vision/image understanding

Do not hard-code the entire application directly to one provider.

Create interfaces such as:

```go
type LLM interface {
    Generate(...)
}

type Embedder interface {
    Embed(...)
}

type VisionModel interface {
    Analyze(...)
}
```

This makes changing providers much easier.

---

## PDF Processing

Initial preference:

- [ ] PyMuPDF / equivalent PDF parser
- [ ] OCR when necessary
- [ ] Image extraction
- [ ] Layout-aware processing

Python can be a separate processing component because the Python ecosystem is significantly better for document processing and OCR.

The Go backend should orchestrate the process rather than trying to recreate the entire Python document-processing ecosystem.

---

# 4. Repository Structure

Recommended monorepo:

```text
pyq-ai/
│
├── apps/
│   ├── web/
│   │   └── Next.js application
│   │
│   └── api/
│       └── Go application
│
├── workers/
│   └── document-processor/
│
├── packages/
│   └── shared/
│
├── db/
│   ├── migrations/
│   └── seeds/
│
├── scripts/
│
├── docs/
│
├── testdata/
│   └── sample PDFs
│
├── docker/
│
├── docker-compose.yml
├── .env.example
├── Makefile
├── README.md
└── PLAN.md
```

A simpler alternative is:

```text
pyq-ai/
├── frontend/
├── backend/
├── processor/
├── db/
├── testdata/
├── docker-compose.yml
└── PLAN.md
```

Either is acceptable.

Prefer the structure that keeps the project understandable rather than maximizing architectural complexity.

---

