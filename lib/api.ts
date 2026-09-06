export interface Document {
  id: string;
  filename: string;
  original_filename?: string | null;
  storage_path?: string | null;
  subject?: string | null;
  year?: number | null;
  status?: string | null;
  created_at?: string | null;
  updated_at?: string | null;
}

export interface Topic {
  id: string;
  name: string;
  subject?: string | null;
  created_at?: string | null;
}

export interface TopicWithCount extends Topic {
  question_count: number;
}

export interface QuestionTopicInfo {
  id: string;
  name: string;
  subject?: string | null;
  confidence?: number | null;
  created_at?: string | null;
}

export interface ClassifyResult {
  document_id: string;
  classified: number;
  status: string;
}

export interface EmbeddingResult {
  document_id: string;
  embedded: number;
  reused: number;
  skipped: number;
  failed: number;
  status: 'embedded' | 'partial';
  failures?: Array<{ question_id: string; error: string }>;
}

export interface Question {
  id: string;
  document_id?: string | null;
  question_number?: string | null;
  question_text?: string | null;
  page_number?: number | null;
  start_page?: number | null;
  end_page?: number | null;
  start_offset?: number | null;
  end_offset?: number | null;
  confidence?: number | null;
  question_type?: string | null;
  options_json?: string | null;
  images_json?: string | null;
  extraction_notes_json?: string | null;
  year?: number | null;
  subject?: string | null;
  difficulty?: string | null;
  created_at?: string | null;
  updated_at?: string | null;
  // Parsed helpers (frontend convenience, not from API)
  options?: string[];
  images?: string[];
}

export interface DocumentImage {
  name: string;
  url: string;
}

export interface ExtractionSummary {
  document_id: string;
  page_count: number;
  extracted_pages: number;
  pages_needing_ocr: number;
  error_pages: number;
}

export interface DocumentFilters {
  limit?: number;
  offset?: number;
  subject?: string;
  status?: string;
  year?: number;
}

export interface TopicFilters {
  limit?: number;
  offset?: number;
  subject?: string;
}

export interface QuestionFilters {
  limit?: number;
  offset?: number;
  subject?: string;
  year?: number;
  document_id?: string;
  topic_id?: string;
}

export interface SearchFilters {
  subject?: string;
  year?: number;
  year_min?: number;
  year_max?: number;
  document_id?: string;
  topic?: string;
  topic_id?: string;
  question_type?: string;
  difficulty?: string;
  threshold?: number;
  limit?: number;
  offset?: number;
}

export interface SearchResult {
  question: Question;
  similarity: number;
  distance: number;
}

export interface SearchResponse {
  query: string;
  results: SearchResult[];
  count: number;
  metric: string;
  model: string;
}

const BASE_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080/api/v1';

export function getBaseUrl(): string {
  return BASE_URL;
}

function buildUrl(path: string, params?: Record<string, string | number | undefined>): string {
  const url = new URL(`${BASE_URL}${path}`);
  if (params) {
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined && value !== '') {
        url.searchParams.set(key, String(value));
      }
    }
  }
  return url.toString();
}

async function request<T>(url: string): Promise<T> {
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`Request failed: ${res.status} ${res.statusText} for ${url}`);
  }
  return (await res.json()) as T;
}

export async function listDocuments(filters?: DocumentFilters): Promise<Document[]> {
  return request<Document[]>(
    buildUrl('/documents', {
      limit: filters?.limit,
      offset: filters?.offset,
      subject: filters?.subject,
      status: filters?.status,
      year: filters?.year,
    })
  );
}

export async function listTopics(filters?: TopicFilters): Promise<Topic[]> {
  return request<Topic[]>(
    buildUrl('/topics', {
      limit: filters?.limit,
      offset: filters?.offset,
      subject: filters?.subject,
    })
  );
}

export async function listTopicsWithCounts(filters?: TopicFilters): Promise<TopicWithCount[]> {
  return request<TopicWithCount[]>(
    buildUrl('/topics', {
      limit: filters?.limit,
      offset: filters?.offset,
      subject: filters?.subject,
      include_counts: 1,
    })
  );
}

export async function classifyDocument(documentId: string): Promise<ClassifyResult> {
  if (!documentId || !documentId.trim()) {
    throw new Error('documentId is required');
  }
  const res = await fetch(`${BASE_URL}/documents/${documentId}/classify`, {
    method: 'POST',
  });
  if (!res.ok) {
    const body = await res.text();
    try {
      const parsed = JSON.parse(body) as ApiErrorBody;
      if (parsed.error?.message) throw new Error(parsed.error.message);
    } catch (e) {
      if (e instanceof Error && (e.message.includes('not found') || e.message.includes('missing') || e.message.includes('failed'))) {
        throw e;
      }
      // ignore parse errors, use generic
    }
    throw new Error(`Classification failed: ${res.status} ${res.statusText}`);
  }
  return (await res.json()) as ClassifyResult;
}

export async function embedDocument(documentId: string): Promise<EmbeddingResult> {
  if (!documentId || !documentId.trim()) {
    throw new Error('documentId is required');
  }
  const res = await fetch(`${BASE_URL}/documents/${documentId}/embed`, { method: 'POST' });
  if (!res.ok) {
    const body = await res.text();
    try {
      const parsed = JSON.parse(body) as ApiErrorBody;
      if (parsed.error?.message) throw new Error(parsed.error.message);
    } catch (error) {
      if (error instanceof Error) throw error;
    }
    throw new Error(`Embedding failed: ${res.status} ${res.statusText}`);
  }
  return (await res.json()) as EmbeddingResult;
}

export async function listQuestionsByTopic(
  topicId: string,
  filters?: { limit?: number; offset?: number }
): Promise<Question[]> {
  if (!topicId || !topicId.trim()) {
    throw new Error('topicId is required');
  }
  const qs = await request<Question[]>(
    buildUrl(`/topics/${topicId}/questions`, {
      limit: filters?.limit,
      offset: filters?.offset,
    })
  );
  return qs.map((q) => {
    let options: string[] | undefined;
    let images: string[] | undefined;
    try {
      if (q.options_json) options = JSON.parse(q.options_json);
    } catch {}
    try {
      if (q.images_json) images = JSON.parse(q.images_json);
    } catch {}
    return { ...q, options, images };
  });
}

export async function listTopicsForQuestion(questionId: string): Promise<Topic[]> {
  if (!questionId || !questionId.trim()) {
    throw new Error('questionId is required');
  }
  return request<Topic[]>(buildUrl(`/questions/${questionId}/topics`, {}));
}

export async function correctQuestionTopics(questionId: string, topicIds: string[]): Promise<Topic[]> {
  if (!questionId || !questionId.trim()) {
    throw new Error('questionId is required');
  }
  const res = await fetch(`${BASE_URL}/questions/${questionId}/topics`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ topic_ids: topicIds }),
  });
  if (!res.ok) {
    const body = await res.text();
    try {
      const parsed = JSON.parse(body) as ApiErrorBody;
      if (parsed.error?.message) throw new Error(parsed.error.message);
    } catch (e) {
      if (e instanceof Error && e.message.includes('not found')) throw e;
    }
    throw new Error(`Correct topics failed: ${res.status} ${res.statusText}`);
  }
  return (await res.json()) as Topic[];
}

export async function mergeTopics(targetId: string, sourceIds: string[]): Promise<{ status: string }> {
  if (!targetId || !targetId.trim()) {
    throw new Error('targetId is required');
  }
  if (!sourceIds || sourceIds.length === 0) {
    throw new Error('sourceIds is required');
  }
  const res = await fetch(`${BASE_URL}/topics/merge`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ target_id: targetId, source_ids: sourceIds }),
  });
  if (!res.ok) {
    const body = await res.text();
    try {
      const parsed = JSON.parse(body) as ApiErrorBody;
      if (parsed.error?.message) throw new Error(parsed.error.message);
    } catch (e) {
      if (e instanceof Error && e.message.includes('not found')) throw e;
    }
    throw new Error(`Merge failed: ${res.status} ${res.statusText}`);
  }
  return (await res.json()) as { status: string };
}

export async function searchQuestions(query: string, filters?: SearchFilters): Promise<SearchResponse> {
  if (!query || !query.trim()) throw new Error('query is required');
  const res = await fetch(buildUrl('/search', { q: query, ...filters } as Record<string, string | number | undefined>));
  if (!res.ok) {
    const body = await res.text();
    try {
      const parsed = JSON.parse(body) as ApiErrorBody;
      if (parsed.error?.message) throw new Error(parsed.error.message);
    } catch (e) {
      if (e instanceof Error && e.message.includes('not found')) throw e;
    }
    throw new Error(`Search failed: ${res.status} ${res.statusText}`);
  }
  const data = (await res.json()) as SearchResponse;
  // Parse nested question JSON strings
  data.results = data.results.map((r) => {
    let options: string[] | undefined;
    let images: string[] | undefined;
    try {
      if (r.question.options_json) options = JSON.parse(r.question.options_json);
    } catch {}
    try {
      if (r.question.images_json) images = JSON.parse(r.question.images_json);
    } catch {}
    return { ...r, question: { ...r.question, options, images } };
  });
  return data;
}

export async function searchQuestionsPost(query: string, filters?: SearchFilters): Promise<SearchResponse> {
  if (!query || !query.trim()) throw new Error('query is required');
  const res = await fetch(`${BASE_URL}/search`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query, ...filters }),
  });
  if (!res.ok) {
    const body = await res.text();
    try {
      const parsed = JSON.parse(body) as ApiErrorBody;
      if (parsed.error?.message) throw new Error(parsed.error.message);
    } catch (e) {
      if (e instanceof Error && e.message.includes('not found')) throw e;
    }
    throw new Error(`Search failed: ${res.status} ${res.statusText}`);
  }
  const data = (await res.json()) as SearchResponse;
  data.results = data.results.map((r) => {
    let options: string[] | undefined;
    let images: string[] | undefined;
    try {
      if (r.question.options_json) options = JSON.parse(r.question.options_json);
    } catch {}
    try {
      if (r.question.images_json) images = JSON.parse(r.question.images_json);
    } catch {}
    return { ...r, question: { ...r.question, options, images } };
  });
  return data;
}

export async function listQuestions(filters?: QuestionFilters): Promise<Question[]> {
  const qs = await request<Question[]>(
    buildUrl('/questions', {
      limit: filters?.limit,
      offset: filters?.offset,
      subject: filters?.subject,
      year: filters?.year,
      document_id: filters?.document_id,
      topic_id: filters?.topic_id,
    })
  );
  // Parse JSON string fields into arrays for convenience
  return qs.map((q) => {
    let options: string[] | undefined;
    let images: string[] | undefined;
    try {
      if (q.options_json) options = JSON.parse(q.options_json);
    } catch {}
    try {
      if (q.images_json) images = JSON.parse(q.images_json);
    } catch {}
    return { ...q, options, images };
  });
}

export async function listDocumentImages(documentId: string): Promise<DocumentImage[]> {
  return request<DocumentImage[]>(buildUrl(`/documents/${documentId}/images`, {}));
}

export async function extractDocument(documentId: string): Promise<ExtractionSummary> {
  const res = await fetch(`${BASE_URL}/documents/${documentId}/extract`, { method: 'POST' });
  if (!res.ok) {
    const body = await res.text();
    try {
      const parsed = JSON.parse(body);
      if (parsed.error?.message) throw new Error(parsed.error.message);
    } catch {}
    throw new Error(`Extraction failed: ${res.status} ${res.statusText}`);
  }
  return (await res.json()) as ExtractionSummary;
}

export async function previewExtraction(documentId: string, pages?: number[]): Promise<any[]> {
  const res = await fetch(`${BASE_URL}/documents/${documentId}/extract-preview`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(pages && pages.length ? { pages } : {}),
  });
  if (!res.ok) throw new Error(`Preview failed: ${res.status}`);
  return await res.json();
}

export function getDocumentImageUrl(documentId: string, imageName: string, thumb = false): string {
  const name = thumb ? `thumb_${imageName}` : imageName;
  // Thumbnail is stored as thumb_<name> with png conversion; try thumb first but fallback to original
  return `${BASE_URL}/documents/${documentId}/images/${name}`;
}

export async function healthCheck(): Promise<{ status: string }> {
  return request<{ status: string }>('/health');
}

interface ApiErrorBody {
  error?: { code?: string; message?: string };
}

function parseUploadError(status: number, statusText: string, body: string): Error {
  try {
    const parsed = JSON.parse(body) as ApiErrorBody;
    if (parsed.error?.message) {
      return new Error(parsed.error.message);
    }
  } catch {
    // Fall through to generic message below.
  }
  return new Error(`Upload failed: ${status} ${statusText || 'Unknown error'}`);
}

export function uploadDocument(
  file: File,
  onProgress?: (percent: number) => void
): Promise<Document> {
  return new Promise<Document>((resolve, reject) => {
    const form = new FormData();
    form.append('file', file);

    const xhr = new XMLHttpRequest();
    xhr.open('POST', `${BASE_URL}/documents`);

    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable && onProgress) {
        onProgress(Math.round((event.loaded / event.total) * 100));
      }
    };

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText) as Document);
        } catch {
          reject(new Error('Upload succeeded but the server returned an invalid response.'));
        }
        return;
      }
      reject(parseUploadError(xhr.status, xhr.statusText, xhr.responseText));
    };

    xhr.onerror = () => {
      reject(new Error(`Network error: could not reach the server at ${BASE_URL}.`));
    };

    xhr.send(form);
  });
}
