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
