import React, { useCallback, useState } from 'react'
import { Button, EmptyState, ErrorState, LoadingState } from '../../components/ui'
import {
  debugRetrieval,
  type DebugRetrievalMode,
  type DebugRetrievalResponse,
} from '../../lib/api'

const MODES: DebugRetrievalMode[] = ['vector', 'keyword', 'hybrid']

export default function RetrievalDebugPage() {
  const [query, setQuery] = useState('questions about deadlock prevention')
  const [mode, setMode] = useState<DebugRetrievalMode>('vector')
  const [limit, setLimit] = useState(10)
  const [result, setResult] = useState<DebugRetrievalResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const run = useCallback(async () => {
    if (!query.trim() || loading) return
    setLoading(true)
    setError(null)
    try {
      const res = await debugRetrieval(query.trim(), { mode, limit })
      setResult(res)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Debug retrieval failed.')
      setResult(null)
    } finally {
      setLoading(false)
    }
  }, [query, mode, limit, loading])

  const handleSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault()
      void run()
    },
    [run]
  )

  return (
    <div className="mx-auto max-w-2xl">
      <h1 className="text-2xl font-bold text-gray-900">Retrieval Debugger</h1>
      <p className="mt-1 text-sm text-gray-500">
        Development page for <code className="font-mono">/debug/retrieval</code>: enter a query to
        inspect top hits as Q/score/topic.
      </p>

      <form onSubmit={handleSubmit} className="mt-6 space-y-4 rounded-lg border border-gray-200 bg-white p-4">
        <div>
          <label htmlFor="debug-retrieval-query" className="block text-sm font-medium text-gray-700">
            Query
          </label>
          <input
            id="debug-retrieval-query"
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="questions about deadlock prevention"
            className="mt-1 w-full rounded border border-gray-300 px-3 py-2 text-sm text-gray-900 placeholder:text-gray-400 focus:border-blue-500 focus:outline-none"
          />
        </div>

        <div className="flex flex-wrap items-end gap-4">
          <div>
            <label htmlFor="debug-retrieval-mode" className="block text-sm font-medium text-gray-700">
              Mode
            </label>
            <select
              id="debug-retrieval-mode"
              value={mode}
              onChange={(e) => setMode(e.target.value as DebugRetrievalMode)}
              className="mt-1 rounded border border-gray-300 px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:outline-none"
            >
              {MODES.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label htmlFor="debug-retrieval-limit" className="block text-sm font-medium text-gray-700">
              Limit
            </label>
            <input
              id="debug-retrieval-limit"
              type="number"
              min={1}
              max={100}
              value={limit}
              onChange={(e) => setLimit(Number(e.target.value) || 10)}
              className="mt-1 w-24 rounded border border-gray-300 px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:outline-none"
            />
          </div>

          <Button type="submit" disabled={loading || !query.trim()}>
            {loading ? 'Searching...' : 'Debug retrieval'}
          </Button>
        </div>
      </form>

      <div className="mt-6">
        {loading ? (
          <LoadingState label="Running retrieval..." />
        ) : error ? (
          <ErrorState message={error} onRetry={run} />
        ) : !result ? (
          <EmptyState message="Run a query to see Q/score/topic hits." />
        ) : result.results.length === 0 ? (
          <EmptyState message={`No hits for "${result.query}".`} />
        ) : (
          <>
            <p className="text-sm text-gray-500">
              {result.count} hit{result.count === 1 ? '' : 's'} for &ldquo;{result.query}&rdquo;
              {' '}({result.mode}, limit {result.limit})
            </p>
            <ul className="mt-3 divide-y divide-gray-100 rounded-lg border border-gray-200 bg-white">
              {result.results.map((hit) => (
                <li key={hit.id} className="px-4 py-3 text-sm">
                  <p className="font-medium text-gray-900">{hit.id}</p>
                  <p className="mt-1 text-gray-600">
                    score: {hit.score.toFixed(2)}
                  </p>
                  <p className="text-gray-600">topic: {hit.topic || '—'}</p>
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </div>
  )
}
