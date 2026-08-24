import React, { useCallback, useEffect, useState } from 'react'
import { EmptyState, ErrorState, LoadingState } from '../../components/ui'
import { listTopics, type Topic } from '../../lib/api'

export default function TopicsPage() {
  const [topics, setTopics] = useState<Topic[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setTopics(await listTopics({ limit: 100 }))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load topics.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  return (
    <div>
      <h1 className="text-2xl font-bold text-gray-900">Topics</h1>
      <p className="mt-1 text-sm text-gray-500">Subjects and topics covered by your questions.</p>

      {loading ? (
        <LoadingState label="Loading topics..." />
      ) : error ? (
        <ErrorState message={error} onRetry={load} />
      ) : topics.length === 0 ? (
        <div className="mt-6">
          <EmptyState message="No topics found." />
        </div>
      ) : (
        <ul className="mt-6 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {topics.map((topic) => (
            <li
              key={topic.id}
              className="rounded-lg border border-gray-200 bg-white p-4 transition hover:border-blue-400 hover:shadow-sm"
            >
              <h3 className="font-medium text-gray-900">{topic.name}</h3>
              <p className="mt-1 text-sm text-gray-500">{topic.subject ?? '—'}</p>
              {topic.created_at && (
                <p className="mt-2 text-xs text-gray-400">
                  Added {new Date(topic.created_at).toLocaleDateString()}
                </p>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
