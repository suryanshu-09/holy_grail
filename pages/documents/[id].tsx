import React, { useCallback, useEffect, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { EmptyState, ErrorState, LoadingState } from '../../components/ui'
import { QuestionCard } from '../../components/QuestionCard'
import { listDocuments, listQuestions, listTopicsForQuestion, classifyDocument, extractDocument, getBaseUrl, type Document, type Question, type Topic, type DocumentImage } from '../../lib/api'

const statusStyles: Record<string, string> = {
  uploaded: 'bg-gray-100 text-gray-700',
  processing: 'bg-yellow-100 text-yellow-800',
  processed: 'bg-green-100 text-green-800',
  extracted: 'bg-green-100 text-green-800',
  failed: 'bg-red-100 text-red-800',
}

function DetailRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between gap-4 py-2 text-sm">
      <dt className="shrink-0 font-medium text-gray-500">{label}</dt>
      <dd className="text-right text-gray-800 break-words">{value}</dd>
    </div>
  )
}

function formatDate(value?: string | null): string {
  return value ? new Date(value).toLocaleString() : '—'
}

export default function DocumentDetailPage() {
  const router = useRouter()
  const { id } = router.query
  const [document, setDocument] = useState<Document | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [questions, setQuestions] = useState<Question[]>([])
  const [qLoading, setQLoading] = useState(false)
  const [images, setImages] = useState<DocumentImage[]>([])
  const [extracting, setExtracting] = useState(false)
  const [extractMsg, setExtractMsg] = useState<string | null>(null)
  const [topicsByQuestion, setTopicsByQuestion] = useState<Record<string, Topic[]>>({})
  const [classifying, setClassifying] = useState(false)
  const [classifyMsg, setClassifyMsg] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (typeof id !== 'string') return
    setLoading(true)
    setError(null)
    try {
      const documents = await listDocuments({ limit: 100 })
      setDocument(documents.find((doc) => doc.id === id) ?? null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load document.')
    } finally {
      setLoading(false)
    }
  }, [id])

  const loadQuestions = useCallback(async () => {
    if (typeof id !== 'string') return
    setQLoading(true)
    try {
      const qs = await listQuestions({ document_id: id, limit: 100 })
      setQuestions(qs)
      // Fetch per-question topics in parallel; failures are per-question non-fatal.
      const entries: [string, Topic[]][] = await Promise.all(
        qs.map(async (q): Promise<[string, Topic[]]> => {
          try {
            const ts = await listTopicsForQuestion(q.id)
            return [q.id, ts]
          } catch {
            return [q.id, []]
          }
        })
      )
      const map: Record<string, Topic[]> = {}
      for (const entry of entries) {
        map[entry[0]] = entry[1]
      }
      setTopicsByQuestion(map)
    } catch {
      // ignore
    } finally {
      setQLoading(false)
    }
  }, [id])

  const loadImages = useCallback(async () => {
    if (typeof id !== 'string') return
    try {
      const res = await fetch(`${getBaseUrl()}/documents/${id}/images`)
      if (res.ok) {
        const data = (await res.json()) as DocumentImage[]
        setImages(Array.isArray(data) ? data : [])
      }
    } catch {
      // ignore
    }
  }, [id])

  useEffect(() => {
    load()
  }, [load])

  useEffect(() => {
    if (document) {
      loadQuestions()
      loadImages()
    }
  }, [document, loadQuestions, loadImages])

  const handleExtract = async () => {
    if (typeof id !== 'string') return
    setExtracting(true)
    setExtractMsg(null)
    try {
      const summary = await extractDocument(id)
      setExtractMsg(`Extracted ${summary.page_count} pages, ${summary.extracted_pages} ok, ${summary.pages_needing_ocr} need OCR`)
      await load()
      await loadQuestions()
      await loadImages()
    } catch (err) {
      setExtractMsg(err instanceof Error ? err.message : 'Extraction failed')
    } finally {
      setExtracting(false)
    }
  }

  const handleClassify = async () => {
    if (typeof id !== 'string') return
    setClassifying(true)
    setClassifyMsg(null)
    try {
      const res = await classifyDocument(id)
      setClassifyMsg(`Classified ${res.classified} questions`)
      await loadQuestions()
    } catch (err) {
      setClassifyMsg(err instanceof Error ? err.message : 'Classification failed')
    } finally {
      setClassifying(false)
    }
  }

  if (loading) return <LoadingState label="Loading document..." />
  if (error) return <ErrorState message={error} onRetry={load} />
  if (!document)
    return (
      <div>
        <EmptyState message="Document not found." />
        <div className="mt-4 text-center">
          <Link href="/documents" className="text-sm text-blue-600 hover:underline">
            Back to documents
          </Link>
        </div>
      </div>
    )

  const status = (document.status ?? 'uploaded').toLowerCase()

  return (
    <div>
      <Link href="/documents" className="text-sm text-blue-600 hover:underline">
        ← Back to documents
      </Link>

      <div className="mt-4 rounded-lg border border-gray-200 bg-white p-6">
        <div className="flex items-start justify-between gap-4">
          <h1 className="break-all text-xl font-bold text-gray-900">{document.filename}</h1>
          <span
            className={`shrink-0 rounded-full px-2.5 py-0.5 text-xs font-medium capitalize ${statusStyles[status] ?? statusStyles.uploaded}`}
          >
            {document.status ?? 'uploaded'}
          </span>
        </div>

        <dl className="mt-4 divide-y divide-gray-100">
          <DetailRow label="ID" value={document.id} />
          <DetailRow label="Subject" value={document.subject ?? '—'} />
          <DetailRow label="Year" value={document.year ?? '—'} />
          <DetailRow label="Original filename" value={document.original_filename ?? '—'} />
          <DetailRow label="Storage path" value={document.storage_path ?? '—'} />
          <DetailRow label="Created" value={formatDate(document.created_at)} />
          <DetailRow label="Updated" value={formatDate(document.updated_at)} />
        </dl>

        <div className="mt-6 flex gap-3">
          <button
            onClick={handleExtract}
            disabled={extracting}
            className="rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
          >
            {extracting ? 'Extracting…' : 'Run extraction'}
          </button>
          <button
            onClick={handleClassify}
            disabled={classifying}
            className="rounded bg-purple-600 px-4 py-2 text-sm font-medium text-white hover:bg-purple-700 disabled:opacity-50"
          >
            {classifying ? 'Classifying…' : 'Classify topics'}
          </button>
          <button
            onClick={() => {
              loadQuestions()
              loadImages()
            }}
            className="rounded border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
          >
            Refresh
          </button>
        </div>
        {extractMsg && <p className="mt-3 text-sm text-gray-600">{extractMsg}</p>}
        {classifyMsg && <p className="mt-2 text-sm text-gray-600">{classifyMsg}</p>}
      </div>

      <div className="mt-8">
        <h2 className="text-lg font-semibold text-gray-900">Extracted Images</h2>
        <p className="mt-1 text-sm text-gray-500">Images embedded in the PDF, associated by page and question.</p>
        {images.length === 0 ? (
          <div className="mt-4">
            <EmptyState message="No images extracted yet. Run extraction to detect images." />
          </div>
        ) : (
          <div className="mt-4 grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-4">
            {images.map((img) => (
              <div key={img.name} className="overflow-hidden rounded-lg border border-gray-200 bg-white">
                <img src={img.url} alt={img.name} className="h-40 w-full object-contain bg-gray-50" loading="lazy" />
                <div className="p-2">
                  <p className="truncate text-xs font-medium text-gray-700">{img.name}</p>
                  <a href={img.url} target="_blank" rel="noreferrer" className="text-xs text-blue-600 hover:underline">
                    Open original
                  </a>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="mt-8">
        <h2 className="text-lg font-semibold text-gray-900">Questions ({questions.length})</h2>
        {qLoading ? (
          <LoadingState label="Loading questions..." />
        ) : questions.length === 0 ? (
          <div className="mt-4">
            <EmptyState message="No questions extracted yet." />
          </div>
        ) : (
          <div className="mt-4 space-y-4">
            {questions.map((q) => (
              <QuestionCard
                key={q.id}
                id={q.id}
                questionText={q.question_text ?? undefined}
                subject={q.subject ?? undefined}
                year={q.year ?? undefined}
                pageNumber={q.page_number ?? undefined}
                startPage={q.start_page ?? undefined}
                endPage={q.end_page ?? undefined}
                difficulty={q.difficulty ?? undefined}
                questionType={q.question_type ?? undefined}
                options={q.options}
                images={q.images}
                documentId={q.document_id ?? document.id}
                confidence={q.confidence ?? undefined}
                topics={topicsByQuestion[q.id]}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
