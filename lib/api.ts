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

// SourceQuestion is the original PYQ a quiz question was derived from.
// Fetched via GET /api/v1/questions/{id} for traceability display
// (original wording/pages/images + document backlink).
export type SourceQuestion = Question;

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

export type RetrievalMode = 'vector' | 'keyword' | 'hybrid';

export interface HybridFilters extends SearchFilters {
  mode?: RetrievalMode;
  keyword?: string;
  vector_weight?: number;
  keyword_weight?: number;
  rerank?: boolean;
  debug?: boolean;
  include_debug?: boolean;
}

export interface HybridResult {
  question: Question;
  vector_score: number;
  keyword_score: number;
  combined_score: number;
  sources: string[];
  rerank_boost?: number;
}

export interface HybridDebugInfo {
  query: string;
  vector_candidates: number;
  keyword_candidates: number;
  merged_candidates: number;
  vector_weight: number;
  keyword_weight: number;
  scoring: string;
  rerank_enabled: boolean;
}

export interface HybridResponse {
  query: string;
  results: HybridResult[];
  count: number;
  metric: string;
  model: string;
  debug?: HybridDebugInfo | null;
}

// Phase 17 retrieval evaluation (GET /api/v1/debug/eval).
// Mirrors internal/search/evaluation.go + evaluation_runner.go and
// internal/http/search_eval.go debugEvalResponse payload.
export type RetrievalStrategy = 'vector' | 'metadata' | 'keyword' | 'hybrid' | 'hybrid+reranker';

export interface EvalRunnerConfig {
  limit: number;
  ks: number[];
}

export interface EvalQueryMetrics {
  query: string;
  strategy: RetrievalStrategy;
  num_relevant: number;
  num_retrieved: number;
  num_relevant_retrieved: number;
  recall: number;
  precision: number;
  hits: Record<number, boolean>;
}

export interface EvalAggregateMetrics {
  strategy: RetrievalStrategy;
  num_queries: number;
  avg_recall: number;
  avg_precision: number;
  top_k_accuracy: Record<number, number>;
}

export interface DebugEvalResponse {
  config: EvalRunnerConfig;
  per_query: Record<RetrievalStrategy, EvalQueryMetrics[]>;
  results: Record<RetrievalStrategy, EvalAggregateMetrics>;
  best_by_recall: RetrievalStrategy;
  num_queries: number;
}

export type QuizMode = 'original' | 'mcq' | 'similar' | 'mixed';

export interface QuizQuestion {
  id?: string;
  source_question_id: string;
  document_id: string;
  question: string;
  options: string[];
  correct_answer: number;
  explanation: string;
}

export interface QuizResponse {
  questions: QuizQuestion[];
}

export interface QuizFilters {
  mode?: QuizMode;
  num_questions?: number;
  length?: number;
  limit?: number;
  difficulty?: string;
  topics?: string[];
  topic?: string;
  subject?: string;
  query?: string;
  q?: string;
}

const BASE_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080/api/v1';

export function getBaseUrl(): string {
  return BASE_URL;
}

function buildUrl(path: string, params?: Record<string, string | number | boolean | undefined>): string {
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

function parseHybridQuestions(data: HybridResponse): HybridResponse {
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

export async function hybridRetrieve(query: string, filters?: HybridFilters): Promise<HybridResponse> {
  if (!query || !query.trim()) throw new Error('query is required');
  const res = await fetch(buildUrl('/search', { q: query, ...filters } as Record<string, string | number | boolean | undefined>));
  if (!res.ok) {
    const body = await res.text();
    try {
      const parsed = JSON.parse(body) as ApiErrorBody;
      if (parsed.error?.message) throw new Error(parsed.error.message);
    } catch (e) {
      if (e instanceof Error && e.message.includes('not found')) throw e;
    }
    throw new Error(`Hybrid retrieval failed: ${res.status} ${res.statusText}`);
  }
  return parseHybridQuestions((await res.json()) as HybridResponse);
}

export async function hybridRetrievePost(query: string, filters?: HybridFilters): Promise<HybridResponse> {
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
    throw new Error(`Hybrid retrieval failed: ${res.status} ${res.statusText}`);
  }
  return parseHybridQuestions((await res.json()) as HybridResponse);
}

export async function getDebugEval(): Promise<DebugEvalResponse> {
  return request<DebugEvalResponse>(buildUrl('/debug/eval', {}));
}

function parseQuizError(status: number, statusText: string, body: string): Error {
  try {
    const parsed = JSON.parse(body) as ApiErrorBody;
    if (parsed.error?.message) {
      return new Error(parsed.error.message);
    }
  } catch {
    // Fall through to generic message below.
  }
  return new Error(`Quiz generation failed: ${status} ${statusText || 'Unknown error'}`);
}

function buildQuizBody(filters?: QuizFilters): Record<string, unknown> {
  const body: Record<string, unknown> = {};
  if (!filters) return body;
  if (filters.mode !== undefined) body.mode = filters.mode;
  if (filters.num_questions !== undefined) body.num_questions = filters.num_questions;
  else if (filters.length !== undefined) body.length = filters.length;
  else if (filters.limit !== undefined) body.limit = filters.limit;
  if (filters.difficulty !== undefined) body.difficulty = filters.difficulty;
  if (filters.topics !== undefined) body.topics = filters.topics;
  if (filters.topic !== undefined) body.topic = filters.topic;
  if (filters.subject !== undefined) body.subject = filters.subject;
  if (filters.query !== undefined) body.query = filters.query;
  if (filters.q !== undefined) body.q = filters.q;
  return body;
}

export async function generateQuiz(filters?: QuizFilters): Promise<QuizResponse> {
  const res = await fetch(`${BASE_URL}/quiz/generate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(buildQuizBody(filters)),
  });
  if (!res.ok) {
    throw parseQuizError(res.status, res.statusText, await res.text());
  }
  return (await res.json()) as QuizResponse;
}

export async function generateQuizGet(filters?: QuizFilters): Promise<QuizResponse> {
  const params: Record<string, string | number | undefined> = {};
  if (filters?.mode) params.mode = filters.mode;
  const length = filters?.num_questions ?? filters?.length ?? filters?.limit;
  if (length !== undefined) params.length = length;
  if (filters?.difficulty) params.difficulty = filters.difficulty;
  if (filters?.subject) params.subject = filters.subject;
  const query = filters?.query ?? filters?.q;
  if (query) params.query = query;
  const url = new URL(`${BASE_URL}/quiz/generate`);
  if (filters?.topics) {
    for (const t of filters.topics) {
      if (t && t.trim()) url.searchParams.append('topics', t);
    }
  }
  if (filters?.topic) url.searchParams.set('topic', filters.topic);
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') {
      url.searchParams.set(key, String(value));
    }
  }
  const res = await fetch(url.toString());
  if (!res.ok) {
    throw parseQuizError(res.status, res.statusText, await res.text());
  }
  return (await res.json()) as QuizResponse;
}

export interface CreateQuizSessionRequest {
  mode?: QuizMode;
  num_questions?: number;
  subject?: string;
  topic?: string;
  topics?: string[];
}

export interface QuizSession {
  id: string;
  mode?: string | null;
  subject?: string | null;
  total_questions: number;
  created_at?: string | null;
}

export interface QuizAttemptInput {
  question_id: string;
  source_question_id?: string | null;
  question_text?: string | null;
  selected_answer: number | null;
  correct_answer: number;
  topic?: string | null;
  subject?: string | null;
  time_taken_seconds: number;
}

export interface QuizAttempt extends QuizAttemptInput {
  id: string;
  session_id: string;
  is_correct: boolean;
}

export interface QuizMetrics {
  score: number;
  accuracy: number;
  questions_attempted: number;
  questions_correct: number;
  questions_incorrect: number;
  average_time_seconds: number;
  total_questions: number;
}

export interface TopicMetric {
  topic: string;
  subject?: string | null;
  attempted: number;
  correct: number;
  incorrect: number;
  accuracy: number;
}

export interface QuizSessionDetail {
  session: QuizSession;
  attempts: QuizAttempt[];
  metrics: QuizMetrics;
  topics: TopicMetric[];
  weak_topics: string[];
}

export interface SubmitQuizAttemptsRequest {
  attempts: QuizAttemptInput[];
}

function parseQuizSessionError(action: string, status: number, statusText: string, body: string): Error {
  try {
    const parsed = JSON.parse(body) as ApiErrorBody;
    if (parsed.error?.message) {
      return new Error(parsed.error.message);
    }
  } catch {
    // Fall through to generic message below.
  }
  return new Error(`${action} failed: ${status} ${statusText || 'Unknown error'}`);
}

async function postJson<T>(url: string, body: unknown): Promise<T> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body ?? {}),
  });
  if (!res.ok) {
    throw parseQuizSessionError('Quiz session request', res.status, res.statusText, await res.text());
  }
  return (await res.json()) as T;
}

export async function createQuizSession(req?: CreateQuizSessionRequest): Promise<QuizSession> {
  const body: Record<string, unknown> = {};
  if (req?.mode !== undefined) body.mode = req.mode;
  if (req?.num_questions !== undefined) body.num_questions = req.num_questions;
  if (req?.subject !== undefined) body.subject = req.subject;
  if (req?.topic !== undefined) body.topic = req.topic;
  if (req?.topics !== undefined) body.topics = req.topics;
  return postJson<QuizSession>(`${BASE_URL}/quiz/sessions`, body);
}

export async function submitQuizAttempt(
  sessionId: string,
  attempt: QuizAttemptInput
): Promise<QuizAttempt> {
  if (!sessionId || !sessionId.trim()) throw new Error('sessionId is required');
  if (!attempt || !attempt.question_id || !attempt.question_id.trim()) {
    throw new Error('attempt.question_id is required');
  }
  return postJson<QuizAttempt>(
    `${BASE_URL}/quiz/sessions/${encodeURIComponent(sessionId)}/attempts`,
    attempt
  );
}

export async function submitQuizAttempts(
  sessionId: string,
  attempts: QuizAttemptInput[]
): Promise<QuizAttempt[]>;
export async function submitQuizAttempts(
  sessionId: string,
  req: SubmitQuizAttemptsRequest
): Promise<QuizAttempt[]>;
export async function submitQuizAttempts(
  sessionId: string,
  attemptsOrReq: QuizAttemptInput[] | SubmitQuizAttemptsRequest
): Promise<QuizAttempt[]> {
  if (!sessionId || !sessionId.trim()) throw new Error('sessionId is required');
  const attempts = Array.isArray(attemptsOrReq) ? attemptsOrReq : attemptsOrReq?.attempts;
  if (!attempts || attempts.length === 0) throw new Error('attempts must be a non-empty array');
  for (const a of attempts) {
    if (!a || !a.question_id || !a.question_id.trim()) {
      throw new Error('each attempt requires question_id');
    }
  }
  return postJson<QuizAttempt[]>(
    `${BASE_URL}/quiz/sessions/${encodeURIComponent(sessionId)}/attempts/bulk`,
    { attempts }
  );
}

export async function getQuizSession(sessionId: string): Promise<QuizSessionDetail> {
  if (!sessionId || !sessionId.trim()) throw new Error('sessionId is required');
  const url = `${BASE_URL}/quiz/sessions/${encodeURIComponent(sessionId)}`;
  const res = await fetch(url);
  if (!res.ok) {
    throw parseQuizSessionError('Fetch quiz session', res.status, res.statusText, await res.text());
  }
  return (await res.json()) as QuizSessionDetail;
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

export async function getQuestionById(questionId: string): Promise<SourceQuestion> {
  if (!questionId || !questionId.trim()) {
    throw new Error('questionId is required');
  }
  const q = await request<Question>(
    buildUrl(`/questions/${encodeURIComponent(questionId.trim())}`, {})
  );
  let options: string[] | undefined;
  let images: string[] | undefined;
  try {
    if (q.options_json) options = JSON.parse(q.options_json);
  } catch {}
  try {
    if (q.images_json) images = JSON.parse(q.images_json);
  } catch {}
  return { ...q, options, images };
}

export function getSourceQuestionImageUrls(question: SourceQuestion, thumb = false): string[] {
  if (!question.document_id || !question.images || question.images.length === 0) {
    return [];
  }
  return question.images.map((name) =>
    getDocumentImageUrl(question.document_id as string, name, thumb)
  );
}

export function getSourceQuestionImageUrl(
  question: SourceQuestion,
  imageName: string,
  thumb = false
): string | null {
  if (!question.document_id || !imageName) {
    return null;
  }
  return getDocumentImageUrl(question.document_id, imageName, thumb);
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
