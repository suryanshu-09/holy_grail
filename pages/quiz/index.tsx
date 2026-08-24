import React, { useCallback, useEffect, useMemo, useState } from 'react'
import Link from 'next/link'
import { ProgressIndicator } from '../../components/ProgressIndicator'
import { QuizQuestion } from '../../components/QuizQuestion'
import { Button, EmptyState, ErrorState, LoadingState } from '../../components/ui'
import { listQuestions, type Question } from '../../lib/api'

const QUIZ_SIZE = 10
const OPTION_COUNT = 4

function buildOptions(): string[] {
  return Array.from({ length: OPTION_COUNT }, (_, i) => `Option ${String.fromCharCode(65 + i)}`)
}

export default function QuizPage() {
  const [questions, setQuestions] = useState<Question[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [currentIndex, setCurrentIndex] = useState(0)
  const [selections, setSelections] = useState<(number | null)[]>([])
  const [finished, setFinished] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const fetched = await listQuestions({ limit: QUIZ_SIZE })
      setQuestions(fetched.slice(0, QUIZ_SIZE))
      setSelections(Array.from({ length: Math.min(fetched.length, QUIZ_SIZE) }, () => null))
      setCurrentIndex(0)
      setFinished(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load questions.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const current = questions[currentIndex]
  const options = useMemo(buildOptions, [])
  const selected = selections[currentIndex] ?? null
  const answeredCount = selections.filter((s) => s !== null).length

  const handleAnswer = (_option: string, index: number) => {
    setSelections((prev) => prev.map((sel, i) => (i === currentIndex ? index : sel)))
  }

  if (loading) return <LoadingState label="Loading quiz..." />
  if (error) return <ErrorState message={error} onRetry={load} />
  if (questions.length === 0)
    return (
      <EmptyState message="No questions available yet. Generate questions from documents to start a quiz.">
        <Link href="/documents" className="text-sm text-blue-600 hover:underline">
          Go to documents
        </Link>
      </EmptyState>
    )

  if (finished)
    return (
      <div className="mx-auto max-w-xl rounded-lg border border-gray-200 bg-white p-8 text-center">
        <h1 className="text-2xl font-bold text-gray-900">Quiz complete</h1>
        <p className="mt-2 text-sm text-gray-500">
          You answered {answeredCount} of {questions.length} questions.
        </p>
        <p className="mt-1 text-xs text-gray-400">
          Scoring will be enabled once answers are generated in Phase 4.
        </p>
        <div className="mt-6 flex justify-center gap-3">
          <Button onClick={load}>Restart quiz</Button>
          <Link
            href="/"
            className="rounded border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50"
          >
            Back to dashboard
          </Link>
        </div>
      </div>
    )

  return (
    <div className="mx-auto max-w-2xl">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold text-gray-900">Quiz</h1>
        <ProgressIndicator
          currentStep={currentIndex + 1}
          totalSteps={questions.length}
          label="Question"
          className="w-48"
        />
      </div>

      <QuizQuestion
        id={current.id}
        questionText={current.question_text ?? undefined}
        subject={current.subject ?? undefined}
        year={current.year ?? undefined}
        pageNumber={current.page_number ?? undefined}
        options={options}
        onAnswer={handleAnswer}
        selectedIndex={selected}
      />

      <div className="mt-6 flex items-center justify-between">
        <button
          type="button"
          disabled={currentIndex === 0}
          onClick={() => setCurrentIndex((i) => Math.max(i - 1, 0))}
          className="rounded border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50"
        >
          Previous
        </button>
        {currentIndex === questions.length - 1 ? (
          <Button onClick={() => setFinished(true)}>Finish</Button>
        ) : (
          <Button onClick={() => setCurrentIndex((i) => i + 1)}>Next</Button>
        )}
      </div>
    </div>
  )
}
