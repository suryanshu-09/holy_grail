import React, { useCallback, useEffect, useState } from 'react'
import Link from 'next/link'
import { EmptyState, ErrorState, LoadingState } from '../components/ui'
import {
  listDocuments,
  listQuestions,
  listQuizHistory,
  listTopics,
  type Document,
  type Question,
  type QuizSession,
  type Topic,
} from '../lib/api'

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

function quizTitle(s: QuizSession): string {
  const topic = (s.topic ?? '').trim()
  if (topic) return topic
  if (s.topics && s.topics.length > 0) {
    const first = (s.topics[0] ?? '').trim()
    if (first) return s.topics.length > 1 ? `${first} +${s.topics.length - 1}` : first
  }
  const subject = (s.subject ?? '').trim()
  if (subject) return subject
  const mode = (s.mode ?? '').trim()
  if (mode) return `${mode.charAt(0).toUpperCase()}${mode.slice(1)} quiz`
  return 'Quiz'
}

function quizSubtitle(s: QuizSession): string {
  const parts: string[] = []
  const subject = (s.subject ?? '').trim()
  const topic = (s.topic ?? '').trim()
  if (subject && subject !== quizTitle(s)) parts.push(subject)
  if (topic && s.topics && s.topics.length > 1) parts.push(`${s.topics.length} topics`)
  const mode = (s.mode ?? '').trim()
  if (mode) parts.push(mode)
  parts.push(`${s.total_questions} Q`)
  if (s.created_at) {
    const d = new Date(s.created_at)
    if (!Number.isNaN(d.getTime())) parts.push(d.toLocaleDateString())
  }
  return parts.join(' · ') || '—'
}

function quizScoreLabel(s: QuizSession): string | null {
  if (typeof s.accuracy === 'number' && Number.isFinite(s.accuracy)) {
    return `${Math.round(s.accuracy)}%`
  }
  if (
    typeof s.score === 'number' &&
    Number.isFinite(s.score) &&
    s.total_questions > 0
  ) {
    return `${s.score}/${s.total_questions}`
  }
  if (
    typeof s.questions_correct === 'number' &&
    typeof s.questions_attempted === 'number' &&
    s.questions_attempted > 0
  ) {
    return `${Math.round((s.questions_correct / s.questions_attempted) * 100)}%`
  }
  return null
}

export default function Dashboard() {
  const [summary, setSummary] = useState<Summary | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [history, setHistory] = useState<QuizSession[] | null>(null)
  const [historyLoading, setHistoryLoading] = useState(true)
  const [historyError, setHistoryError] = useState<string | null>(null)

  const loadLibrary = useCallback(async () => {
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

  const loadHistory = useCallback(async () => {
    setHistoryLoading(true)
    setHistoryError(null)
    try {
      // Anonymous callers get [] (never another user's sessions).
      const sessions = await listQuizHistory(5)
      setHistory(sessions ?? [])
    } catch (err) {
      setHistoryError(err instanceof Error ? err.message : 'Failed to load quiz history.')
    } finally {
      setHistoryLoading(false)
    }
  }, [])

  useEffect(() => {
    loadLibrary()
    loadHistory()
  }, [loadLibrary, loadHistory])

  if (loading) return <LoadingState label="Loading dashboard..." />
  if (error) return <ErrorState message={error} onRetry={loadLibrary} />

  const isEmpty =
    !summary ||
    (summary.documents.length === 0 &&
      summary.questions.length === 0 &&
      summary.topics.length === 0)

  return (
    <div>
      <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
      <p className="mt-1 text-sm text-gray-500">Overview of your question bank.</p>

      {isEmpty ? (
        <div className="mt-6">
          <EmptyState message="No data yet. Upload documents and generate questions to get started." />
        </div>
      ) : (
        summary && (
          <>
            <h2 className="mt-6 text-lg font-semibold text-gray-900">Your Library</h2>
            <div className="mt-3 grid grid-cols-1 gap-4 sm:grid-cols-3">
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

            <h2 className="mt-8 text-lg font-semibold text-gray-900">Recent documents</h2>
            {summary.documents.length === 0 ? (
              <div className="mt-3">
                <EmptyState message="No documents yet. Upload a PDF to get started." />
              </div>
            ) : (
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
            )}

            <div className="mt-8 flex items-center justify-between">
              <h2 className="text-lg font-semibold text-gray-900">Recent Quizzes</h2>
              <Link href="/quiz" className="text-sm font-medium text-blue-600 hover:text-blue-700">
                Take a quiz
              </Link>
            </div>
            {historyLoading ? (
              <div className="mt-3">
                <LoadingState label="Loading recent quizzes..." />
              </div>
            ) : historyError ? (
              <div className="mt-3">
                <ErrorState message={historyError} onRetry={loadHistory} />
              </div>
            ) : !history || history.length === 0 ? (
              <div className="mt-3">
                <EmptyState message="No quizzes yet. Take a quiz to see recent results here." />
              </div>
            ) : (
              <ul className="mt-3 divide-y divide-gray-100 rounded-lg border border-gray-200 bg-white">
                {history.slice(0, 5).map((s) => {
                  const score = quizScoreLabel(s)
                  return (
                    <li
                      key={s.id}
                      className="flex items-center justify-between px-4 py-3 text-sm"
                    >
                      <span className="min-w-0">
                        <span className="block truncate font-medium text-gray-800">
                          {quizTitle(s)}
                        </span>
                        <span className="block truncate text-xs text-gray-500">
                          {quizSubtitle(s)}
                        </span>
                      </span>
                      <span
                        className={`ml-4 shrink-0 rounded-full px-3 py-1 text-xs font-semibold ${
                          score ? 'bg-emerald-50 text-emerald-700' : 'bg-gray-100 text-gray-500'
                        }`}
                      >
                        {score ?? '—'}
                      </span>
                    </li>
                  )
                })}
              </ul>
            )}
          </>
        )
      )}

      {/* Quiz history is also shown when the library is empty so returning
          users with quizzes but no documents still see recent results. */}
      {isEmpty && (
        <>
          <div className="mt-8 flex items-center justify-between">
            <h2 className="text-lg font-semibold text-gray-900">Recent Quizzes</h2>
            <Link href="/quiz" className="text-sm font-medium text-blue-600 hover:text-blue-700">
              Take a quiz
            </Link>
          </div>
          {historyLoading ? (
            <div className="mt-3">
              <LoadingState label="Loading recent quizzes..." />
            </div>
          ) : historyError ? (
            <div className="mt-3">
              <ErrorState message={historyError} onRetry={loadHistory} />
            </div>
          ) : !history || history.length === 0 ? (
            <div className="mt-3">
              <EmptyState message="No quizzes yet. Take a quiz to see recent results here." />
            </div>
          ) : (
            <ul className="mt-3 divide-y divide-gray-100 rounded-lg border border-gray-200 bg-white">
              {history.slice(0, 5).map((s) => {
                const score = quizScoreLabel(s)
                return (
                  <li key={s.id} className="flex items-center justify-between px-4 py-3 text-sm">
                    <span className="min-w-0">
                      <span className="block truncate font-medium text-gray-800">{quizTitle(s)}</span>
                      <span className="block truncate text-xs text-gray-500">{quizSubtitle(s)}</span>
                    </span>
                    <span
                      className={`ml-4 shrink-0 rounded-full px-3 py-1 text-xs font-semibold ${
                        score ? 'bg-emerald-50 text-emerald-700' : 'bg-gray-100 text-gray-500'
                      }`}
                    >
                      {score ?? '—'}
                    </span>
                  </li>
                )
              })}
            </ul>
          )}
        </>
      )}
    </div>
  )
}
