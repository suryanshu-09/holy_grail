import React, { useCallback, useEffect, useState } from 'react'
import Link from 'next/link'
import { EmptyState, ErrorState, LoadingState } from '../components/ui'
import { listDocuments, listQuestions, listTopics, type Document, type Question, type Topic } from '../lib/api'

interface Summary {
  documents: Document[]
  questions: Question[]
  topics: Topic[]
}

const stats = [
  { key: 'documents', label: 'Documents', href: '/documents', accent: 'text-blue-600' },
  { key: 'questions', label: 'Questions', href: '/quiz', accent: 'text-emerald-600' },
  { key: 'topics', label: 'Topics', href: '/topics', accent: 'text-purple-600' },
] as const

export default function Dashboard() {
  const [summary, setSummary] = useState<Summary | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [documents, questions, topics] = await Promise.all([
        listDocuments({ limit: 100 }),
        listQuestions({ limit: 100 }),
        listTopics({ limit: 100 }),
      ])
      setSummary({ documents, questions, topics })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load summary.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  if (loading) return <LoadingState label="Loading dashboard..." />
  if (error) return <ErrorState message={error} onRetry={load} />

  return (
    <div>
      <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
      <p className="mt-1 text-sm text-gray-500">Overview of your question bank.</p>

      {!summary || (summary.documents.length === 0 && summary.questions.length === 0 && summary.topics.length === 0) ? (
        <div className="mt-6">
          <EmptyState message="No data yet. Upload documents and generate questions to get started." />
        </div>
      ) : (
        <>
          <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-3">
            {stats.map((stat) => (
              <Link
                key={stat.key}
                href={stat.href}
                className="rounded-lg border border-gray-200 bg-white p-5 transition hover:border-blue-400 hover:shadow-sm"
              >
                <p className="text-sm font-medium text-gray-500">{stat.label}</p>
                <p className={`mt-2 text-3xl font-bold ${stat.accent}`}>{summary[stat.key].length}</p>
              </Link>
            ))}
          </div>

          {summary.documents.length > 0 && (
            <>
              <h2 className="mt-8 text-lg font-semibold text-gray-900">Recent documents</h2>
              <ul className="mt-3 divide-y divide-gray-100 rounded-lg border border-gray-200 bg-white">
                {summary.documents.slice(0, 5).map((doc) => (
                  <li key={doc.id}>
                    <Link
                      href={`/documents/${doc.id}`}
                      className="flex items-center justify-between px-4 py-3 text-sm hover:bg-gray-50"
                    >
                      <span className="truncate font-medium text-gray-800">{doc.filename}</span>
                      <span className="ml-4 shrink-0 text-gray-500">
                        {[doc.subject, doc.year].filter(Boolean).join(' · ') || '—'}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            </>
          )}
        </>
      )}
    </div>
  )
}
