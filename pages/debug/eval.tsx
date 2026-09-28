import React, { useCallback, useState } from 'react'
import { Button, EmptyState, ErrorState, LoadingState } from '../../components/ui'
import {
  getDebugEval,
  type DebugEvalResponse,
  type RetrievalStrategy,
} from '../../lib/api'

const STRATEGIES: RetrievalStrategy[] = ['vector', 'metadata', 'keyword', 'hybrid', 'hybrid+reranker']

function pct(v: number): string {
  return `${(v * 100).toFixed(1)}%`
}

export default function RetrievalEvalPage() {
  const [result, setResult] = useState<DebugEvalResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const run = useCallback(async () => {
    if (loading) return
    setLoading(true)
    setError(null)
    try {
      const res = await getDebugEval()
      setResult(res)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Evaluation failed.')
      setResult(null)
    } finally {
      setLoading(false)
    }
  }, [loading])

  return (
    <div className="mx-auto max-w-3xl">
      <h1 className="text-2xl font-bold text-gray-900">Retrieval Evaluation</h1>
      <p className="mt-1 text-sm text-gray-500">
        Runs the bundled 60-query evaluation dataset ({result?.num_queries ?? 60} queries) over every
        strategy and reports precision / recall / top-K accuracy. Best strategy by recall is the tuning
        verdict.
      </p>

      <div className="mt-4">
        <Button onClick={run} disabled={loading}>
          {loading ? 'Evaluating...' : result ? 'Re-run evaluation' : 'Run evaluation'}
        </Button>
      </div>

      <div className="mt-6">
        {loading ? (
          <LoadingState label="Running evaluation..." />
        ) : error ? (
          <ErrorState message={error} onRetry={run} />
        ) : !result ? (
          <EmptyState message="Run the evaluation to compare vector, metadata, keyword, hybrid and hybrid+reranker." />
        ) : (
          <>
            <p className="text-sm text-gray-500">
              {result.num_queries} queries · limit {result.config.limit} · K = {result.config.ks.join(', ')} ·
              best by recall: <span className="font-semibold text-gray-900">{result.best_by_recall}</span>
            </p>
            <div className="mt-3 overflow-x-auto rounded-lg border border-gray-200 bg-white">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-200 text-left text-gray-500">
                    <th className="px-4 py-2 font-medium">Strategy</th>
                    <th className="px-4 py-2 font-medium">Precision</th>
                    <th className="px-4 py-2 font-medium">Recall</th>
                    {result.config.ks.map((k) => (
                      <th key={k} className="px-4 py-2 font-medium">
                        Top-{k}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {STRATEGIES.map((s) => {
                    const m = result.results[s]
                    if (!m) return null
                    const best = s === result.best_by_recall
                    return (
                      <tr key={s} className={`border-b border-gray-100 last:border-0 ${best ? 'bg-green-50' : ''}`}>
                        <td className="px-4 py-2 font-medium text-gray-900">
                          {s}
                          {best && (
                            <span className="ml-2 rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-800">
                              best
                            </span>
                          )}
                        </td>
                        <td className="px-4 py-2 text-gray-700">{pct(m.avg_precision)}</td>
                        <td className="px-4 py-2 text-gray-700">{pct(m.avg_recall)}</td>
                        {result.config.ks.map((k) => (
                          <td key={k} className="px-4 py-2 text-gray-700">
                            {m.top_k_accuracy?.[k] != null ? pct(m.top_k_accuracy[k]) : '—'}
                          </td>
                        ))}
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
