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

- [x] Navbar
- [x] Sidebar
- [x] Document card
- [x] Upload component
- [x] Topic selector
- [x] Question card
- [x] Quiz question
- [x] Progress indicator
- [x] Loading state
- [x] Error state
- [x] Empty state

---

## Tasks

- [x] Build application layout
- [x] Configure API client
- [x] Configure environment variables
- [x] Add loading states
- [x] Add error handling
- [x] Build dashboard
- [x] Build documents page
- [x] Build topics page

---

## Completion Criteria

Frontend can display database data through the Go API.

Completed: 2026-09-02T00:00:00+05:30

Verified: Next.js layout (Navbar, Sidebar, Layout), all 11 reusable components, API client with DOCUMENT/TOPIC/QUESTION fetchers, env config via NEXT_PUBLIC_API_URL, loading/error/empty states wired in dashboard, documents, topics and document detail pages; `npm run build` succeeds and pages render via live Go API.

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

- [x] Only PDF files
- [x] File size limit
- [x] Filename sanitization
- [x] Unique document ID
- [x] Prevent path traversal
- [x] Validate actual file type
- [x] Store metadata
- [x] Return document ID

---

## Frontend

Implement:

- [x] File picker
- [x] Drag-and-drop
- [x] Upload progress
- [x] Upload errors
- [x] Document status
- [x] Document list

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

- [x] Integrate PDF parser
- [x] Extract page count
- [x] Extract text
- [x] Preserve page numbers
- [x] Detect empty/scanned pages
- [x] Detect pages requiring OCR
- [x] Normalize whitespace
- [x] Preserve useful line breaks
- [x] Preserve question numbering
- [x] Save intermediate extraction results

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

- [x] All pages are processed
- [x] Text is extracted
- [x] Page numbers remain known
- [x] Scanned pages are detected
- [x] OCR fallback works

Completed: 2026-09-02T00:00:00+05:30

Verified: `internal/extraction.Service.ExtractFile` uses ledongthuc/pdf, preserves page numbers, detects scanned pages via `needsOCR` (<20 chars), Tesseract OCR fallback via `OCRer` interface, whitespace normalization, debug writer emits `page-001.txt` and `summary.json`, `pages.json` under `data/documents/<id>/extraction/`; integration tests cover OCR-skipped, page count, and empty page cases.

---

# 11. Phase 6 — Question Extraction

## Goal

Convert raw PDF pages into individual, standalone question objects with reliable source metadata (start/end pages, extraction confidence, question type and numbering). This phase is critical for downstream QA, search, and UI rendering.

---

## Representation (final target)

Each question object should be a small JSON record that can be rendered, searched, and linked to images and source pages:

```json
{
  "question_id": "docid-q17",
  "document_id": "docid",
  "number": "17",
  "type": "MCQ|numerical|descriptive|unknown",
  "text": "Full question text including subquestions",
  "options": ["A...","B..."],          
  "answer_hint": null,
  "start_page": 14,
  "end_page": 15,
  "start_offset": 123,      
  "end_offset": 456,        
  "images": ["image_1.png"],
  "confidence": 0.87,
  "extraction_notes": ["deterministic:matched-1."]
}
```

- start_offset/end_offset: optional character offsets into the page text for fine-grained region linking.

---

## Design principles

- Deterministic rules first: fast, explainable, testable.
- LLM only as fallback for ambiguous or badly formatted pages.
- Preserve source provenance: keep start/end pages and offsets.
- Provide confidence and extraction notes for triage and incremental improvement.

---

## Deterministic parser (algorithm sketch)

1. Normalize text per page (unicode, whitespace, consistent line endings).
2. Tokenize lines and detect numbering using regex patterns in priority order:
   - explicit markers: `^Q\\s*\\d+`, `^Question\\s+\\d+`
   - numbered lines: `^\\d+\\.` , `^\\d+\\)` , `^[A-Z]\\)` for options
   - common exam patterns (year-prefix, section headers)
3. Group contiguous lines into candidate question blocks until next numbering marker or clear separator.
4. Post-process each block to detect question type: presence of option markers → MCQ/MSQ, numeric-only answers → numerical, long paragraphs → descriptive.
5. Detect subquestions by nested numbering (i.a., (a), (i)).
6. Merge blocks across pages when trailing context suggests continuation (no terminal punctuation, incomplete sentence, or explicit "continued on next page").
7. Emit question records with source pages and offsets and a deterministic confidence score (rule-match strength).

Include unit tests for each regex and grouping heuristic using a small corpus of annotated pages.

---

## LLM fallback strategy

When deterministic parsing fails or confidence < threshold (e.g., 0.6):

- Build a compact prompt containing: document id, page range (max 2–3 pages per call), the extracted normalized text, and explicit instructions to return only JSON matching the schema below.
- Request structured JSON: questions array with number, text, start_page, end_page, options (if any), type, and confidence (0–1).
- Validate returned JSON strictly; fall back to manual QA for invalid responses.

Example output schema (strict):

```json
{ "questions": [ { "number": "17", "text": "...", "start_page": 16, "end_page": 17, "options": [], "type": "descriptive", "confidence": 0.92 } ] }
```

Record model prompt hash and response for reproducibility and debugging.

---

## Interfaces

Internal service interface (Go):

- ExtractQuestions(documentID string) ([]Question, error)
- ExtractFromPages(documentID string, pages []int) ([]Question, error)
- ValidateQuestion(q Question) error

API (optional) to preview extraction on the frontend:

POST /api/v1/documents/:id/extract-preview
Body: { pages: [1,2] }
Response: { questions: [...] }

---

## Storage

- Save canonical question JSON to database table `questions` with columns matching the JSON fields.
- Save raw extraction artifacts to `debug/<document-id>/extraction.json` and per-question JSON for replay.
- Store images and page-text debug files already defined in Phase 5.

---

## Debugging and QA

- Save human-review flags and corrected question JSON in `debug/` so corrections feed training data.
- Provide a lightweight UI to mark question extraction as `accepted`, `needs-fix`, or `skip` and capture corrected text.
- Track metrics: questions-per-page, pages-with-ambiguous-detection, LLM-fallback-rate, average confidence.

---

## Tasks (finished plan)

- [x] Create question extraction interface (API signatures + tests)
- [x] Implement deterministic parser (unit tests for regexes & grouping)
- [x] Implement LLM fallback (prompt templates, response validation, rate limiting)
- [x] Handle multi-page questions (merge heuristics + tests)
- [x] Handle subquestions (nested numbering support)
- [x] Handle MCQs (option detection and normalization)
- [x] Handle numerical questions (answer pattern detection)
- [x] Handle descriptive questions (long-form body extraction)
- [x] Handle questions without numbering (heuristic detection + LLM fallback)
- [x] Store extraction confidence and notes (DB schema + debug output)
- [x] Store extraction errors and raw artifacts (debug/ directory)
- [x] Provide extraction-preview API for the frontend
- [x] Add end-to-end integration test: upload sample PDF → run extraction → assert expected question count and page ranges

---

## Completion Criteria (expanded)

Given a representative PYQ PDF:

- [x] All pages are processed (no silent page drops)
- [x] Questions are produced as individual JSON records with start_page and end_page
- [x] Multi-page questions are merged correctly in >90% of test samples
- [x] MCQs have options extracted into the `options` array
- [x] Confidence scores exist and LLM fallback is only used when deterministic confidence < threshold
- [x] Debug artifacts produced for every document so failures can be replayed
- [x] An integration test exists that validates extraction for at least 3 sample PDFs (MCQ, descriptive, mixed)

---

Notes: keep the deterministic parser and LLM prompt templates under source control; corrections from the debug UI should be saved to a training corpus for incremental improvement.

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

- [x] Extract embedded images
- [x] Save images locally
- [x] Record page position if available
- [x] Associate image with page
- [x] Associate image with question
- [x] Store image metadata
- [x] Generate thumbnails
- [x] Display extracted images in UI

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

Completed: 2026-09-02T00:00:00+05:30

Verified: `questions` table extended via `002_add_question_extraction_metadata.sql` (start_page, end_page, start_offset, end_offset, confidence, question_type, options_json, extraction_notes_json, images_json) with index `idx_questions_document_start_end`; `questions.Repository.Insert` and `List`/`GetByID` updated; `QuestionCard` renders start/end pages, question_type chip, options list, confidence percentage, and per-question images; `lib/api.ts` parses JSON fields; document detail page shows `Pages 14–15` and image association.

---

