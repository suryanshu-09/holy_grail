import React, { useCallback, useEffect, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { EmptyState, ErrorState, LoadingState } from '../../components/ui'
import { listDocuments, type Document } from '../../lib/api'

const statusStyles: Record<string, string> = {
  uploaded: 'bg-gray-100 text-gray-700',
  processing: 'bg-yellow-100 text-yellow-800',
  processed: 'bg-green-100 text-green-800',
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

  useEffect(() => {
    load()
  }, [load])

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
      </div>
    </div>
  )
}
