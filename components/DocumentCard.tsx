import React from 'react'
import Link from 'next/link'

export interface DocumentData {
  id: string
  filename: string
  subject?: string | null
  year?: number | null
  status?: string | null
  created_at?: string | null
}

const statusStyles: Record<string, string> = {
  uploaded: 'bg-gray-100 text-gray-700',
  processing: 'bg-yellow-100 text-yellow-800',
  processed: 'bg-green-100 text-green-800',
  failed: 'bg-red-100 text-red-800',
}

export function DocumentCard({ document }: { document: DocumentData }) {
  const status = (document.status ?? 'uploaded').toLowerCase()
  return (
    <Link
      href={`/documents/${document.id}`}
      className="block rounded-lg border border-gray-200 p-4 transition hover:border-blue-500 hover:shadow-sm"
    >
      <div className="flex items-start justify-between gap-2">
        <h3 className="truncate font-medium text-gray-900">{document.filename}</h3>
        <span className={`shrink-0 rounded-full px-2 py-0.5 text-xs font-medium capitalize ${statusStyles[status] ?? statusStyles.uploaded}`}>
          {document.status ?? 'uploaded'}
        </span>
      </div>
      <dl className="mt-3 flex flex-wrap gap-x-6 gap-y-1 text-sm text-gray-600">
        <div>
          <dt className="inline">Subject: </dt>
          <dd className="inline">{document.subject ?? '—'}</dd>
        </div>
        <div>
          <dt className="inline">Year: </dt>
          <dd className="inline">{document.year ?? '—'}</dd>
        </div>
      </dl>
      <p className="mt-2 text-xs text-gray-400">
        Created {document.created_at ? new Date(document.created_at).toLocaleDateString() : '—'}
      </p>
    </Link>
  )
}
