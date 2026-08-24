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

export interface Question {
  id: string;
  document_id?: string | null;
  question_number?: string | null;
  question_text?: string | null;
  page_number?: number | null;
  year?: number | null;
  subject?: string | null;
  difficulty?: string | null;
  created_at?: string | null;
  updated_at?: string | null;
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

export async function listQuestions(filters?: QuestionFilters): Promise<Question[]> {
  return request<Question[]>(
    buildUrl('/questions', {
      limit: filters?.limit,
      offset: filters?.offset,
      subject: filters?.subject,
      year: filters?.year,
    })
  );
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
