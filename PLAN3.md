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

- [x] Define topic classification schema
- [x] LLM classification
- [x] Normalize topic names
- [x] Create topic records
- [x] Associate questions with topics
- [x] Add confidence
- [x] Allow manual topic correction
- [x] Allow merging duplicate topics

---

## Completion Criteria

After processing a document, the user can see:

```text
Deadlocks        42 questions
Paging           31 questions
CPU Scheduling   28 questions
Synchronization  19 questions
```

Completed: 2026-09-02T00:00:00+05:30

Verified: topics table with `question_topics` join (confidence, created_at) via `003_add_topic_classification.sql`; `topics.Classifier` (LLM + heuristic fallback) with `NormalizeTopicName`/`CanonicalTopicKey` dedup and confidence clamping; `ExtractionService.ClassifyDocument` persists via `FindOrCreate`+`AddQuestionTopic` and writes `classification.json`; HTTP `GET /api/v1/topics?include_counts=1` returns `TopicWithCount`, `PATCH /api/v1/questions/{id}/topics` corrects and `POST /api/v1/topics/merge` merges duplicates; frontend `pages/topics/index.tsx` uses `listTopicsWithCounts`, `TopicSelector` shows counts, document detail shows per-question topics via `GET /api/v1/questions/{id}/topics`; integration test `TestPhase9_FullFlow_ExtractClassifyCountsCorrectionMerge` extracts mock PDF→classify fake LLM→verifies counts and correction/merge; `go vet`/`go test`/`npm run build` pass.

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

- [x] Create `Embedder` interface
- [x] Select embedding model
- [x] Generate embeddings
- [x] Store vectors in pgvector
- [x] Store embedding model name
- [x] Handle embedding failures
- [x] Add retry mechanism
- [x] Batch embedding requests
- [x] Avoid embedding duplicate questions

---

## Important

Embedding generation should be deterministic enough that the same question can be recognized as similar.

Do not silently change embedding models without considering re-indexing.

---

## Completion Criteria

Every processed question has an embedding.

Completed: 2026-09-06T00:00:00+05:30

Verified: `internal/embeddings` provides a model-pinned OpenAI `Embedder`, retrying batch service, rich Subject/Topics/Question input, content-hash skipping, and database-side vector reuse for duplicates. Migration `004_add_embedding_metadata.sql` records input hashes and updates. Extraction invokes embeddings after classification when `OPENAI_API_KEY` is configured; `POST /api/v1/documents/{id}/embed` and the document UI support safe manual retries. `OPENAI_EMBEDDING_MODEL` defaults to `text-embedding-3-small`; changing models requires a vector schema migration and full re-index.

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

- [x] Implement vector search query
- [x] Configure similarity metric
- [x] Implement top-K retrieval
- [x] Add metadata filtering
- [x] Add minimum similarity threshold
- [x] Return similarity scores
- [x] Test search quality

---

Completed: 2026-09-06T00:00:00+05:30

Verified: `internal/search` provides model-pinned cosine (`<=>`) repository with `vectorLiteral` and threshold-to-distance mapping, dynamic metadata filtering for subject/year/year_min/year_max/document/topic/topic_id/question_type/difficulty via `EXISTS` subqueries, and top-K pagination with similarity=`1-distance` scoring. Migration `005_add_vector_search.sql` adds `vector_cosine_ops` ivfflat/HNSW indexes and filtering indexes; `Service.Search` embeds query via OpenAI `text-embedding-3-small` (1536 dims) and delegates to repository with clamping and threshold validation. HTTP `GET|POST /api/v1/search` via `handleSearch` supports `q`/`query`, all metadata query params and JSON body (camelCase/snake_case), validates threshold/limit/offset, returns `{query, results[{question, similarity, distance}], count, metric, model}` with 503 when not configured; wired in `cmd/api/main.go` and `internal/http/router.go`. Frontend `lib/api.ts` adds `searchQuestions`/`searchQuestionsPost` with `SearchFilters`/`SearchResponse` types. Tests `internal/search/service_test.go` verify cosine top-K ordering, metadata filters, threshold, pagination, and metric operators; `internal/http/search_test.go` covers GET/POST handler validation, camelCase, method and router integration; `go vet`/`go test`/`npm run build` pass.

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

- [x] Implement metadata filtering
- [x] Implement keyword search
- [x] Combine retrieval methods
- [x] Deduplicate results
- [x] Implement scoring
- [x] Add optional reranking
- [x] Return retrieval explanation/debug information

---

Completed: 2026-09-16

Verified: hybrid retrieval combines metadata filtering (shared Filter pushed to both branches), postgres FTS keyword search (006_add_hybrid_retrieval.sql, KeywordRepository with ts_rank), and vector search via HybridService fusion with dedupe by question ID, weighted-sum + RRF scoring, optional ExactMatchReranker boost, and DebugInfo (vector/keyword/merged counts, weights, scoring, rerank flag); HTTP GET|POST /api/v1/search preserves Phase 11 vector API (default mode=vector, fallback when hybrid unavailable) and supports mode/keyword/vector_weight/keyword_weight/rerank/debug params; frontend lib/api.ts adds HybridFilters/HybridResponse types and hybridRetrieve/hybridRetrievePost (GET+POST) helpers; go vet/go test/npm run build pass.

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

- [x] Create quiz generation service
- [x] Define strict output schema
- [x] Validate LLM output
- [x] Reject malformed responses
- [x] Implement retries
- [x] Preserve source question ID
- [x] Prevent duplicate questions
- [x] Generate explanations
- [x] Support quiz length
- [x] Support difficulty
- [x] Support topic filtering

---

Completed: 2026-09-16

Verified: `internal/quiz` provides the quiz generation service (`service.go` with `QuizGenerator` backed by hybrid retrieval, `model.go` strict QuizRequest/QuizResponse schema, `validate.go` strict parsing that rejects malformed LLM output, `prompt.go` generation prompts); all 4 modes supported (original/mcq/similar/mixed) with per-question explanations and `source_question_id` traceability preserved on every item; deterministic dedupe by source question ID plus in-memory difficulty/subject filtering; retries up to `DefaultMaxAttempts` (3 attempts) with fallback to deterministic Original PYQs, never silently repairing bad output; quiz length via `NumQuestions` (length/limit/num_questions aliases) and difficulty/topic/subject filtering pushed to retrieval and re-checked in-memory; HTTP `GET|POST /api/v1/quiz/generate` via `internal/http/quiz.go` (`handleQuizGenerate`, wired in router) with alias-tolerant body/query params and validation; frontend `lib/api.ts` adds `QuizMode`/`QuizQuestion`/`QuizResponse`/`QuizFilters` types and `generateQuiz`/`generateQuizGet` (GET+POST) helpers; tests `internal/quiz/service_test.go` + `quiz_test.go` and `internal/http/quiz_test.go` cover schema validation, retries, dedupe, explanations, length/difficulty/topic filtering and handler validation; `go vet`/`go test`/`npm run build` pass.

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

- [x] Question card
- [x] Option selection
- [x] Submit answer
- [x] Disable modification after submission
- [x] Show correct/incorrect
- [x] Show explanation
- [x] Next question
- [x] Progress indicator
- [x] Quiz completion page
- [x] Score
- [x] Accuracy
- [x] Time spent
- [x] Retry quiz

---

Completed: 2026-09-21

Verified: quiz setup form (`QuizSetupForm`) collects mode/length/difficulty/topic/subject/query and `pages/quiz/index.tsx` drives the full flow via `generateQuiz` from `lib/api.ts`; `QuizQuestionCard` renders options with selection, submit locks answers (options disabled after submission), and shows correct/incorrect feedback plus explanation; `QuizProgress` shows current position/answered count with a live elapsed timer; completion page (`QuizResults`) shows score, accuracy, time spent with per-question breakdown and retry (same filters) plus new-settings action; files touched: `pages/quiz/index.tsx`, `components/quiz/{index.ts,QuizSetupForm.tsx,QuizQuestionCard.tsx,QuizProgress.tsx,QuizResults.tsx}`; backend untouched (Phase 13 `GET|POST /api/v1/quiz/generate` reused as-is, no go vet/test needed); `npm run build` passes.

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
