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

