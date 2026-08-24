import React, { useCallback, useEffect, useState } from 'react'
import { DocumentCard, type DocumentData } from '../../components/DocumentCard'
import { EmptyState, ErrorState, LoadingState } from '../../components/ui'
import { Upload } from '../../components/Upload'
import { listDocuments } from '../../lib/api'

export default function DocumentsPage() {
  const [documents, setDocuments] = useState<DocumentData[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setDocuments(await listDocuments({ limit: 100 }))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load documents.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  return (
    <div>
      <h1 className="text-2xl font-bold text-gray-900">Documents</h1>
      <p className="mt-1 text-sm text-gray-500">Uploaded past-year question papers.</p>

      <div className="mt-6">
        <Upload onUploaded={load} />
      </div>

      {loading ? (
        <LoadingState label="Loading documents..." />
      ) : error ? (
        <ErrorState message={error} onRetry={load} />
      ) : documents.length === 0 ? (
        <div className="mt-6">
          <EmptyState message="No documents found. Upload a document to get started." />
        </div>
      ) : (
        <div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
          {documents.map((doc) => (
            <DocumentCard key={doc.id} document={doc} />
          ))}
        </div>
      )}
    </div>
  )
}
