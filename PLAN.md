# PYQ AI Quiz Platform

A local-first AI application that ingests stacks of Previous Year Question (PYQ) PDFs, extracts individual questions and associated images, classifies them by subject/topic, stores them in PostgreSQL with vector embeddings, and allows users to generate quizzes from specific topics.

The project should begin as a simple, reliable application and progressively evolve into a proper RAG/hybrid-retrieval system.

---

# Table of Contents

1. [Project Vision](#1-project-vision)
2. [Final Architecture](#2-final-architecture)
3. [Technology Stack](#3-technology-stack)
4. [Repository Structure](#4-repository-structure)
5. [Phase 0 — Project Setup](#5-phase-0--project-setup)
6. [Phase 1 — Database Foundation](#6-phase-1--database-foundation)
7. [Phase 2 — Go Backend Foundation](#7-phase-2--go-backend-foundation)
8. [Phase 3 — Next.js Frontend Foundation](#8-phase-3--nextjs-frontend-foundation)
9. [Phase 4 — Document Upload](#9-phase-4--document-upload)
10. [Phase 5 — PDF Text Extraction](#10-phase-5--pdf-text-extraction)
11. [Phase 6 — Question Extraction](#11-phase-6--question-extraction)
12. [Phase 7 — Image Extraction and Association](#12-phase-7--image-extraction-and-association)
13. [Phase 8 — Question Storage and Metadata](#13-phase-8--question-storage-and-metadata)
14. [Phase 9 — Topic Classification](#14-phase-9--topic-classification)
15. [Phase 10 — Embeddings](#15-phase-10--embeddings)
16. [Phase 11 — Vector Search](#16-phase-11--vector-search)
17. [Phase 12 — Hybrid Retrieval](#17-phase-12--hybrid-retrieval)
18. [Phase 13 — Quiz Generation](#18-phase-13--quiz-generation)
19. [Phase 14 — Quiz UI](#19-phase-14--quiz-ui)
20. [Phase 15 — Quiz Evaluation](#20-phase-15--quiz-evaluation)
21. [Phase 16 — Source Question Traceability](#21-phase-16--source-question-traceability)
22. [Phase 17 — Retrieval Evaluation](#22-phase-17--retrieval-evaluation)
23. [Phase 18 — Multimodal Improvements](#23-phase-18--multimodal-improvements)
24. [Phase 19 — Background Jobs](#24-phase-19--background-jobs)
25. [Phase 20 — Authentication and User Data](#25-phase-20--authentication-and-user-data)
26. [Phase 21 — UX and Product Polish](#26-phase-21--ux-and-product-polish)
27. [Phase 22 — Testing](#27-phase-22--testing)
28. [Phase 23 — Observability and Debugging](#28-phase-23--observability-and-debugging)
29. [Phase 24 — Performance](#29-phase-24--performance)
30. [Phase 25 — Proper Docker Build](#30-phase-25--proper-docker-build)
31. [Phase 26 — Deployment Preparation](#31-phase-26--deployment-preparation)
32. [Phase 27 — Optional Advanced Features](#32-phase-27--optional-advanced-features)
33. [Definition of Done](#33-definition-of-done)
34. [Recommended Development Order](#34-recommended-development-order)

---

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

# 5. Phase 0 — Project Setup

## Goal

Create a clean repository with the frontend, backend, database, and development workflow.

---

## Tasks

### Repository

- [ ] Create Git repository
- [ ] Create `README.md`
- [ ] Create `PLAN.md`
- [ ] Create `.gitignore`
- [ ] Create `.env.example`
- [ ] Create `Makefile`
- [ ] Establish directory structure

### Go

- [ ] Initialize Go module
- [ ] Create `cmd/api`
- [ ] Create `internal/`
- [ ] Create configuration package
- [ ] Create logging setup
- [ ] Create basic HTTP server
- [ ] Add health endpoint

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

- [ ] Initialize Next.js application
- [ ] Configure TypeScript
- [ ] Configure Tailwind
- [ ] Configure shadcn/ui
- [ ] Create base layout
- [ ] Create home page
- [ ] Create API client layer

### Docker

At this stage Docker Compose should contain **only PostgreSQL**.

- [ ] Create `docker-compose.yml`
- [ ] Add PostgreSQL + pgvector
- [ ] Add persistent volume
- [ ] Add healthcheck
- [ ] Configure database environment variables

Example architecture:

```text
docker compose
└── postgres
```

Do not add Redis yet.

---

## Completion Criteria

- [ ] `docker compose up` starts PostgreSQL
- [ ] Go API starts locally
- [ ] Next.js starts locally
- [ ] Frontend can reach Go `/health`
- [ ] Git repository is cleanly organized

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

- [ ] Design ER diagram
- [ ] Create migrations
- [ ] Enable pgvector
- [ ] Create indexes
- [ ] Create foreign keys
- [ ] Create uniqueness constraints
- [ ] Create timestamps
- [ ] Create migration rollback support
- [ ] Test clean database creation
- [ ] Test migration rollback
- [ ] Add seed data

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

# 8. Phase 3 — Next.js Frontend Foundation

## Goal

Create the basic application UI.

---

## Pages

Initial pages:

```text
/
 /documents
 /documents/[id]
 /topics
 /quiz
```

---

## Components

Create reusable components for:

- [ ] Navbar
- [ ] Sidebar
- [ ] Document card
- [ ] Upload component
- [ ] Topic selector
- [ ] Question card
- [ ] Quiz question
- [ ] Progress indicator
- [ ] Loading state
- [ ] Error state
- [ ] Empty state

---

## Tasks

- [ ] Build application layout
- [ ] Configure API client
- [ ] Configure environment variables
- [ ] Add loading states
- [ ] Add error handling
- [ ] Build dashboard
- [ ] Build documents page
- [ ] Build topics page

---

## Completion Criteria

Frontend can display database data through the Go API.

---

# 9. Phase 4 — Document Upload

## Goal

Allow users to upload PYQ PDFs.

---

## Initial Approach

For local development, do not introduce S3/R2 yet.

Use local storage:

```text
./data/
└── documents/
```

The backend stores:

```text
data/documents/<document-id>/original.pdf
```

---

## API

```text
POST /api/v1/documents
```

Multipart upload.

---

## Validation

- [ ] Only PDF files
- [ ] File size limit
- [ ] Filename sanitization
- [ ] Unique document ID
- [ ] Prevent path traversal
- [ ] Validate actual file type
- [ ] Store metadata
- [ ] Return document ID

---

## Frontend

Implement:

- [ ] File picker
- [ ] Drag-and-drop
- [ ] Upload progress
- [ ] Upload errors
- [ ] Document status
- [ ] Document list

---

## Completion Criteria

You can upload a PDF and see:

```text
Operating Systems PYQs.pdf
Status: Uploaded
```

---

# 10. Phase 5 — PDF Text Extraction

## Goal

Extract useful content from PDFs.

---

## Important Principle

Do not immediately send the entire PDF to an LLM.

First understand the document.

Pipeline:

```text
PDF
 ↓
Page
 ↓
Layout/text extraction
 ↓
Page representation
```

Represent each page internally as something like:

```json
{
  "page": 17,
  "text": "...",
  "images": []
}
```

---

## Tasks

- [ ] Integrate PDF parser
- [ ] Extract page count
- [ ] Extract text
- [ ] Preserve page numbers
- [ ] Detect empty/scanned pages
- [ ] Detect pages requiring OCR
- [ ] Normalize whitespace
- [ ] Preserve useful line breaks
- [ ] Preserve question numbering
- [ ] Save intermediate extraction results

---

## OCR

Only use OCR when required.

Detect:

```text
Extracted text length < threshold
```

Then:

```text
PDF page
 ↓
render image
 ↓
OCR
 ↓
text
```

---

## Debugging Requirement

Create a debug output:

```text
debug/
└── document-id/
    ├── page-001.txt
    ├── page-002.txt
    ├── page-003.txt
    └── ...
```

This makes extraction problems much easier to diagnose.

---

## Completion Criteria

Given a sample PYQ PDF:

- [ ] All pages are processed
- [ ] Text is extracted
- [ ] Page numbers remain known
- [ ] Scanned pages are detected
- [ ] OCR fallback works

---

# 11. Phase 6 — Question Extraction

## Goal

Convert raw PDF pages into individual question objects.

This is one of the most important phases.

---

## Do NOT think only in terms of chunks

The target representation is:

```text
Document
 │
 ├── Question 1
 ├── Question 2
 ├── Question 3
 ├── Question 4
 └── ...
```

Each question should contain enough information to stand alone.

---

## Initial Detection

Use deterministic rules first:

- [ ] Detect `Q1`
- [ ] Detect `1.`
- [ ] Detect `1)`
- [ ] Detect `Question 1`
- [ ] Detect section headings
- [ ] Detect common numbering patterns
- [ ] Detect multi-line questions

Do not immediately rely entirely on an LLM.

---

## LLM-Assisted Extraction

For ambiguous documents, provide page text to an LLM and request structured output.

Example conceptual schema:

```json
{
  "questions": [
    {
      "number": "17",
      "text": "...",
      "start_page": 16,
      "end_page": 17
    }
  ]
}
```

---

## Important

A question may span multiple pages.

Therefore:

```text
start_page
end_page
```

is preferable to only:

```text
page_number
```

---

## Tasks

- [ ] Create question extraction interface
- [ ] Implement deterministic parser
- [ ] Implement LLM fallback
- [ ] Handle multi-page questions
- [ ] Handle subquestions
- [ ] Handle MCQs
- [ ] Handle numerical questions
- [ ] Handle descriptive questions
- [ ] Handle questions without numbering
- [ ] Store extraction confidence
- [ ] Store extraction errors

---

## Completion Criteria

Upload a PYQ PDF and obtain:

```text
Question 1
Question 2
Question 3
...
Question N
```

with reliable source page information.

---

# 12. Phase 7 — Image Extraction and Association

## Goal

Preserve images belonging to questions.

---

## Pipeline

```text
PDF
 │
 ├── Text
 │
 └── Images
        │
        ▼
    Image positions
        │
        ▼
    Page association
        │
        ▼
    Question association
```

---

## Tasks

- [ ] Extract embedded images
- [ ] Save images locally
- [ ] Record page position if available
- [ ] Associate image with page
- [ ] Associate image with question
- [ ] Store image metadata
- [ ] Generate thumbnails
- [ ] Display extracted images in UI

---

## Association Heuristic

Initially:

```text
image on same page as question
        ↓
belongs to question
```

If multiple questions exist on the same page:

```text
image Y coordinate
        ↓
nearest question region
```

Use LLM/vision assistance only when heuristics are insufficient.

---

## Completion Criteria

Given:

```text
Q17

Consider the following graph:

[IMAGE]

Which option is correct?
```

the database must contain:

```text
Q17
 └── image_1
```

and the frontend must render it correctly.

---

# 13. Phase 8 — Question Storage and Metadata

## Goal

Turn extracted questions into high-quality structured records.

---

## Question Metadata

Store:

```text
question ID
document ID
question number
text
subject
year
start page
end page
difficulty
question type
source
extraction confidence
```

Possible question types:

```text
MCQ
MSQ
numerical
descriptive
true_false
unknown
```

---

## Document Metadata

Store:

```text
filename
subject
year
exam
university
course
semester
source
```

Do not require all metadata initially.

Allow unknown values.

---

## Completion Criteria

The application can show:

```text
Operating Systems
2024
Question 17
Pages 14–15
MCQ
Topics: Deadlock, Resource Allocation
```

---

# 14. Phase 9 — Topic Classification

## Goal

Automatically determine which topics each question belongs to.

---

## Example

Question:

> Explain the four necessary conditions for deadlock and discuss methods to prevent it.

Classification:

```text
Subject:
Operating Systems

Topics:
- Deadlock
- Deadlock Prevention
```

---

## Approach

Use an LLM to generate structured metadata.

Do not allow completely uncontrolled topic strings if consistency matters.

For example, these should not become three topics:

```text
Deadlocks
deadlock
Deadlock Problem
```

Instead normalize:

```text
Deadlock
```

---

## Topic Taxonomy

Eventually create:

```text
Operating Systems
├── Processes
├── Threads
├── CPU Scheduling
├── Synchronization
├── Deadlocks
├── Memory Management
│   ├── Paging
│   ├── Segmentation
│   └── Virtual Memory
├── File Systems
└── I/O
```

But do not manually build an enormous taxonomy at the beginning.

Start with flat topics.

---

## Tasks

- [ ] Define topic classification schema
- [ ] LLM classification
- [ ] Normalize topic names
- [ ] Create topic records
- [ ] Associate questions with topics
- [ ] Add confidence
- [ ] Allow manual topic correction
- [ ] Allow merging duplicate topics

---

## Completion Criteria

After processing a document, the user can see:

```text
Deadlocks        42 questions
Paging           31 questions
CPU Scheduling   28 questions
Synchronization  19 questions
```

---

# 15. Phase 10 — Embeddings

## Goal

Represent questions as vectors for semantic retrieval.

---

## Embedding Input

Prefer embedding a rich representation:

```text
Subject: Operating Systems

Topics:
Deadlock, Resource Allocation

Question:
Explain the four necessary conditions for deadlock...
```

rather than only:

```text
Explain the four necessary conditions...
```

---

## Tasks

- [ ] Create `Embedder` interface
- [ ] Select embedding model
- [ ] Generate embeddings
- [ ] Store vectors in pgvector
- [ ] Store embedding model name
- [ ] Handle embedding failures
- [ ] Add retry mechanism
- [ ] Batch embedding requests
- [ ] Avoid embedding duplicate questions

---

## Important

Embedding generation should be deterministic enough that the same question can be recognized as similar.

Do not silently change embedding models without considering re-indexing.

---

## Completion Criteria

Every processed question has an embedding.

---

# 16. Phase 11 — Vector Search

## Goal

Implement semantic question retrieval.

---

## Query

User:

> "Questions about avoiding deadlocks"

Pipeline:

```text
User query
 ↓
Embedding
 ↓
pgvector similarity search
 ↓
Top K questions
```

---

## Tasks

- [ ] Implement vector search query
- [ ] Configure similarity metric
- [ ] Implement top-K retrieval
- [ ] Add metadata filtering
- [ ] Add minimum similarity threshold
- [ ] Return similarity scores
- [ ] Test search quality

---

## Metadata Filters

Allow:

```text
subject
year
document
topic
question_type
difficulty
```

Example:

```text
topic = Deadlock
year >= 2020
```

combined with vector search.

---

# 17. Phase 12 — Hybrid Retrieval

## Goal

Combine structured filtering, keyword search, and semantic search.

---

## Why?

Vector search is excellent for:

```text
"questions involving circular wait"
```

But metadata search is better for:

```text
all questions tagged Deadlock
```

Keyword search is useful for:

```text
"Banker's algorithm"
```

---

## Retrieval Pipeline

```text
User Query
    │
    ├──────────────┐
    ▼              ▼
Metadata       Vector Search
Filtering
    │              │
    └──────┬───────┘
           ▼
       Candidate Set
           │
           ▼
        Reranker
           │
           ▼
     Final Questions
```

---

## Tasks

- [ ] Implement metadata filtering
- [ ] Implement keyword search
- [ ] Combine retrieval methods
- [ ] Deduplicate results
- [ ] Implement scoring
- [ ] Add optional reranking
- [ ] Return retrieval explanation/debug information

---

## Example

Query:

```text
"Give me questions about Banker's algorithm from 2020 onwards"
```

Extract:

```text
topic = Deadlock
keyword = Banker's algorithm
year >= 2020
```

Then retrieve semantically relevant questions.

---

# 18. Phase 13 — Quiz Generation

## Goal

Turn retrieved questions into a useful quiz.

---

## Important Rule

The LLM should **not invent source material**.

Retrieved PYQs should be the source of truth.

---

## Pipeline

```text
User
 │
 │ "Quiz me on deadlocks"
 ▼
Retrieval
 │
 ▼
Relevant PYQs
 │
 ▼
Quiz Generation Prompt
 │
 ▼
Structured LLM Output
 │
 ▼
Quiz
```

---

## Generated Quiz Schema

Conceptually:

```json
{
  "questions": [
    {
      "id": "...",
      "source_question_id": "...",
      "question": "...",
      "options": [
        "...",
        "...",
        "...",
        "..."
      ],
      "correct_answer": 2,
      "explanation": "..."
    }
  ]
}
```

---

## Generation Modes

Implement gradually.

### Mode 1 — Original PYQs

Show the original question directly.

### Mode 2 — MCQ conversion

Convert descriptive PYQ into an MCQ.

### Mode 3 — Similar question

Generate a new question based on a PYQ.

### Mode 4 — Mixed quiz

Combine multiple question types.

---

## Tasks

- [ ] Create quiz generation service
- [ ] Define strict output schema
- [ ] Validate LLM output
- [ ] Reject malformed responses
- [ ] Implement retries
- [ ] Preserve source question ID
- [ ] Prevent duplicate questions
- [ ] Generate explanations
- [ ] Support quiz length
- [ ] Support difficulty
- [ ] Support topic filtering

---

# 19. Phase 14 — Quiz UI

## Goal

Build a polished quiz experience.

---

## UI

```text
Operating Systems
Deadlocks

Question 4 / 10

Consider the following...

[A] ...
[B] ...
[C] ...
[D] ...

          Submit
```

---

## Tasks

- [ ] Question card
- [ ] Option selection
- [ ] Submit answer
- [ ] Disable modification after submission
- [ ] Show correct/incorrect
- [ ] Show explanation
- [ ] Next question
- [ ] Progress indicator
- [ ] Quiz completion page
- [ ] Score
- [ ] Accuracy
- [ ] Time spent
- [ ] Retry quiz

---

# 20. Phase 15 — Quiz Evaluation

## Goal

Track user performance.

---

## Store

```text
quiz session
question
selected answer
correct answer
is_correct
time_taken
```

---

## Metrics

Calculate:

```text
score
accuracy
questions attempted
questions correct
questions incorrect
average time
```

---

## Future Topic Metrics

```text
Deadlock
  Accuracy: 82%

Paging
  Accuracy: 61%

CPU Scheduling
  Accuracy: 91%
```

---

## Completion Criteria

The application can identify weak topics.

---

# 21. Phase 16 — Source Question Traceability

## Goal

Every generated quiz question should be traceable to the original PYQ.

---

## Example

```text
Generated Question

"Which of the following statements..."

Source:
Operating Systems 2023 PYQ
Question 42
Page 18
```

---

## Tasks

- [ ] Store source question ID
- [ ] Store source document ID
- [ ] Show source information
- [ ] Add "View Original"
- [ ] Display original image
- [ ] Display original wording
- [ ] Preserve page number
- [ ] Link back to document

---

## This is important

It makes hallucinations easier to detect.

The user can always ask:

> "Where did this question come from?"

and the application can answer.

---

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

- [ ] Vision model integration
- [ ] Image descriptions
- [ ] Image-aware question extraction
- [ ] Image-aware quiz generation
- [ ] Test diagrams
- [ ] Test graphs
- [ ] Test mathematical figures
- [ ] Test tables
- [ ] Test charts

---

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

- [ ] Add Redis container
- [ ] Add Asynq
- [ ] Create worker application
- [ ] Create processing queue
- [ ] Move PDF processing to worker
- [ ] Store job status
- [ ] Retry failed jobs
- [ ] Prevent duplicate processing
- [ ] Add job timeout
- [ ] Add progress reporting

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

- [ ] User registration/login
- [ ] Sessions
- [ ] User-specific documents
- [ ] User-specific quizzes
- [ ] Authorization middleware
- [ ] Prevent access to another user's documents
- [ ] User preferences
- [ ] Quiz history

---

## Security

- [ ] Password hashing if handling passwords directly
- [ ] Secure cookies
- [ ] CSRF considerations
- [ ] File upload validation
- [ ] Rate limiting
- [ ] Request size limits
- [ ] SQL injection prevention
- [ ] Path traversal prevention

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

That progression gives you a project that starts simple enough to actually finish, while leaving a clear path toward a genuinely sophisticated RAG-based learning platform.