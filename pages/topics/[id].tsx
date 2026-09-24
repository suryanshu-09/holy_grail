import React, { useCallback, useEffect, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/router'
import { EmptyState, ErrorState, LoadingState } from '../../components/ui'
import { listQuestionsByTopic, listTopicsWithCounts, type Question, type TopicWithCount } from '../../lib/api'

export default function TopicDetailPage() {
  const router = useRouter()
  const { id } = router.query
  const topicId = typeof id === 'string' ? id : ''

  const [topic, setTopic] = useState<TopicWithCount | null>(null)
  const [questions, setQuestions] = useState<Question[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!topicId) return
    setLoading(true)
    setError(null)
    try {
      const [topics, qs] = await Promise.all([
        listTopicsWithCounts({ limit: 100 }),
        listQuestionsByTopic(topicId, { limit: 100 }),
      ])
      const found = topics.find((t) => t.id === topicId) ?? null
      setTopic(found)
      setQuestions(qs)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load topic.')
    } finally {
      setLoading(false)
    }
  }, [topicId])

  useEffect(() => {
    if (topicId) {
      load()
    }
  }, [topicId, load])

  if (!topicId) {
    return <LoadingState label="Loading topic..." />
  }

  if (loading) return <LoadingState label="Loading topic..." />
  if (error) return <ErrorState message={error} onRetry={load} />

  if (!topic && questions.length === 0) {
    return (
      <div>
        <Link href="/topics" className="text-sm text-blue-600 hover:underline">
          ← Back to topics
        </Link>
        <div className="mt-6">
          <EmptyState message="Topic not found." />
        </div>
      </div>
    )
  }

  const topicName = topic?.name ?? 'Topic'
  const total = questions.length
  const easy = questions.filter((q) => (q.difficulty ?? '').toLowerCase() === 'easy').length
  const medium = questions.filter((q) => (q.difficulty ?? '').toLowerCase() === 'medium').length
  const hard = questions.filter((q) => (q.difficulty ?? '').toLowerCase() === 'hard').length

  if (total === 0) {
    return (
      <div>
        <Link href="/topics" className="text-sm text-blue-600 hover:underline">
          ← Back to topics
        </Link>
        <h1 className="mt-4 text-2xl font-bold text-gray-900">{topicName}</h1>
        {topic?.subject && <p className="mt-1 text-sm text-gray-500">{topic.subject}</p>}
        <div className="mt-6">
          <EmptyState message="No questions found for this topic." />
        </div>
      </div>
    )
  }

  return (
    <div>
      <Link href="/topics" className="text-sm text-blue-600 hover:underline">
        ← Back to topics
      </Link>

      <h1 className="mt-4 text-2xl font-bold text-gray-900">{topicName}</h1>
      {topic?.subject && <p className="mt-1 text-sm text-gray-500">{topic.subject}</p>}

      <p className="mt-2 text-sm font-semibold text-gray-700">
        {total} {total === 1 ? 'question' : 'questions'}
      </p>

      <div className="mt-4 grid grid-cols-3 gap-3">
        <div className="rounded-lg border border-gray-200 bg-white p-4 text-center">
          <p className="text-sm font-medium text-green-700">Easy</p>
          <p className="mt-1 text-2xl font-bold text-gray-900">{easy}</p>
        </div>
        <div className="rounded-lg border border-gray-200 bg-white p-4 text-center">
          <p className="text-sm font-medium text-yellow-700">Medium</p>
          <p className="mt-1 text-2xl font-bold text-gray-900">{medium}</p>
        </div>
        <div className="rounded-lg border border-gray-200 bg-white p-4 text-center">
          <p className="text-sm font-medium text-red-700">Hard</p>
          <p className="mt-1 text-2xl font-bold text-gray-900">{hard}</p>
        </div>
      </div>

      <div className="mt-6">
        <Link
          href={`/quiz?topic=${encodeURIComponent(topicName)}`}
          className="inline-block rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
        >
          Start Quiz
        </Link>
      </div>
    </div>
  )
}
