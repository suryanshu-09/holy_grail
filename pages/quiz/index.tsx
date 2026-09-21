import React, { useCallback, useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { QuizProgress, QuizQuestionCard, QuizResults, QuizSetupForm, formatQuizTime } from '../../components/quiz'
import type { QuizResultDetail } from '../../components/quiz'
import { Button, EmptyState, ErrorState, LoadingState } from '../../components/ui'
import { generateQuiz, type QuizFilters, type QuizQuestion } from '../../lib/api'

type RunnerPhase = 'setup' | 'runner' | 'results'

export default function QuizPage() {
  const [phase, setPhase] = useState<RunnerPhase>('setup')
  const [filters, setFilters] = useState<QuizFilters | null>(null)
  const [questions, setQuestions] = useState<QuizQuestion[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [currentIndex, setCurrentIndex] = useState(0)
  const [selections, setSelections] = useState<(number | null)[]>([])
  const [submitted, setSubmitted] = useState<boolean[]>([])

  // Timing: wall-clock quiz start/end + per-question start timestamps.
  const [quizStart, setQuizStart] = useState<number | null>(null)
  const [quizEnd, setQuizEnd] = useState<number | null>(null)
  const [elapsedSeconds, setElapsedSeconds] = useState(0)
  const questionStartRef = useRef<number[]>([])

  const startQuiz = useCallback(async (nextFilters: QuizFilters) => {
    setLoading(true)
    setError(null)
    setFilters(nextFilters)
    try {
      const res = await generateQuiz(nextFilters)
      const qs = res.questions ?? []
      if (qs.length === 0) {
        setQuestions([])
        setPhase('runner')
      } else {
        const now = Date.now()
        setQuestions(qs)
        setSelections(Array.from({ length: qs.length }, () => null))
        setSubmitted(Array.from({ length: qs.length }, () => false))
        questionStartRef.current = Array.from({ length: qs.length }, () => now)
        setQuizStart(now)
        setQuizEnd(null)
        setElapsedSeconds(0)
        setCurrentIndex(0)
        setPhase('runner')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to generate quiz.')
      setQuestions([])
      setPhase('runner')
    } finally {
      setLoading(false)
    }
  }, [])

  // Stamp per-question start time when navigating to a question for the first time.
  useEffect(() => {
    if (phase !== 'runner' || questions.length === 0) return
    const now = Date.now()
    if (!questionStartRef.current[currentIndex]) {
      questionStartRef.current[currentIndex] = now
    }
  }, [currentIndex, phase, questions.length])

  // Live elapsed timer while running.
  useEffect(() => {
    if (phase !== 'runner' || quizStart === null || quizEnd !== null) return
    const id = setInterval(() => {
      setElapsedSeconds(Math.floor((Date.now() - (quizStart ?? Date.now())) / 1000))
    }, 1000)
    return () => clearInterval(id)
  }, [phase, quizStart, quizEnd])

  const handleSelect = useCallback(
    (index: number) => {
      if (submitted[currentIndex]) return
      setSelections((prev) => prev.map((sel, i) => (i === currentIndex ? index : sel)))
    },
    [currentIndex, submitted]
  )

  const handleSubmitAnswer = useCallback(() => {
    if (selections[currentIndex] === null || submitted[currentIndex]) return
    setSubmitted((prev) => prev.map((s, i) => (i === currentIndex ? true : s)))
  }, [currentIndex, selections, submitted])

  const goTo = useCallback(
    (index: number) => {
      const clamped = Math.min(Math.max(index, 0), questions.length - 1)
      if (!questionStartRef.current[clamped]) {
        questionStartRef.current[clamped] = Date.now()
      }
      setCurrentIndex(clamped)
    },
    [questions.length]
  )

  const handleNext = useCallback(() => {
    if (currentIndex === questions.length - 1) {
      setQuizEnd(Date.now())
      setPhase('results')
    } else {
      goTo(currentIndex + 1)
    }
  }, [currentIndex, goTo, questions.length])

  const handlePrevious = useCallback(() => {
    goTo(currentIndex - 1)
  }, [currentIndex, goTo])

  const handleRetry = useCallback(() => {
    if (filters) {
      startQuiz(filters)
    } else {
      setPhase('setup')
    }
  }, [filters, startQuiz])

  const handleNewSettings = useCallback(() => {
    setPhase('setup')
    setError(null)
  }, [])

  // ---- Setup screen ----
  if (phase === 'setup' && !loading) {
    return (
      <div className="mx-auto max-w-2xl">
        <div className="mb-6">
          <h1 className="text-2xl font-bold text-gray-900">Quiz</h1>
          <p className="mt-1 text-sm text-gray-500">
            Generate a quiz from your documents with mode, length, and filters.
          </p>
        </div>
        <QuizSetupForm
          initial={filters ?? undefined}
          loading={loading}
          error={error}
          onSubmit={startQuiz}
        />
      </div>
    )
  }

  if (loading) return <LoadingState label="Generating quiz..." />

  if (error && questions.length === 0 && phase !== 'setup') {
    return <ErrorState message={error} onRetry={() => (filters ? startQuiz(filters) : setPhase('setup'))} />
  }

  if (phase === 'runner' && questions.length === 0 && !loading && !error) {
    return (
      <div className="mx-auto max-w-2xl">
        <div className="mb-6">
          <h1 className="text-2xl font-bold text-gray-900">Quiz</h1>
        </div>
        <EmptyState message="No questions were generated for these filters. Try different settings.">
          <div className="mt-4 flex justify-center gap-3">
            <Button onClick={handleNewSettings}>Change settings</Button>
            <Link href="/" className="rounded border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50">
              Back to dashboard
            </Link>
          </div>
        </EmptyState>
      </div>
    )
  }

  // ---- Results ----
  if (phase === 'results') {
    const totalQuestions = questions.length
    const correctCount = questions.filter(
      (q, i) => submitted[i] && selections[i] !== null && selections[i] === q.correct_answer
    ).length
    const attemptedCount = selections.filter((s) => s !== null).length
    const accuracy = totalQuestions > 0 ? Math.round((correctCount / totalQuestions) * 100) : 0
    const totalSeconds =
      quizStart !== null && quizEnd !== null
        ? Math.max(0, Math.floor((quizEnd - quizStart) / 1000))
        : elapsedSeconds
    const details: QuizResultDetail[] = questions.map((q, i) => ({
      question: q.question,
      correct: submitted[i] && selections[i] !== null && selections[i] === q.correct_answer,
      selectedIndex: selections[i],
      correctAnswer: q.correct_answer,
    }))

    return (
      <div className="mx-auto max-w-2xl">
        <QuizResults
          totalQuestions={totalQuestions}
          correctCount={correctCount}
          timeSpentSeconds={totalSeconds}
          onRetry={handleRetry}
          details={details}
        />
        <div className="mx-auto mt-4 grid max-w-xl grid-cols-2 gap-3 text-center">
          <div className="rounded-lg border border-gray-200 bg-white px-3 py-3">
            <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">Attempted</p>
            <p className="mt-1 text-lg font-bold text-gray-900">
              {attemptedCount}/{totalQuestions}
            </p>
          </div>
          <div className="rounded-lg border border-gray-200 bg-white px-3 py-3">
            <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">Accuracy</p>
            <p className="mt-1 text-lg font-bold text-gray-900">{accuracy}%</p>
          </div>
        </div>
        <p className="mt-3 text-center text-sm text-gray-500">
          Time spent: {formatQuizTime(totalSeconds)} · Questions attempted: {attemptedCount} of {totalQuestions}
        </p>
        <div className="mt-6 flex flex-wrap justify-center gap-3">
          <Button onClick={handleRetry}>Retry quiz</Button>
          <Button onClick={handleNewSettings}>Start new quiz with different settings</Button>
          <Link
            href="/"
            className="rounded border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50"
          >
            Back to dashboard
          </Link>
        </div>
      </div>
    )
  }

  // ---- Runner ----
  const current = questions[currentIndex]
  if (!current) return <LoadingState label="Loading quiz..." />
  const selected = selections[currentIndex] ?? null
  const isSubmitted = submitted[currentIndex] ?? false
  const answeredCount = submitted.filter(Boolean).length
  const activeSubject = filters?.subject?.trim() || null
  const activeTopic =
    filters?.topic?.trim() || (filters?.topics && filters.topics.length > 0 ? filters.topics[0] : null) || null

  return (
    <div className="mx-auto max-w-2xl">
      <div className="mb-2 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">
            Question {currentIndex + 1}/{questions.length}
          </h1>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs">
            {activeSubject && (
              <span className="rounded-full bg-blue-50 px-2.5 py-0.5 font-medium text-blue-700">
                {activeSubject}
              </span>
            )}
            {activeTopic && (
              <span className="rounded-full bg-purple-50 px-2.5 py-0.5 font-medium text-purple-700">
                {activeTopic}
              </span>
            )}
            {!activeSubject && !activeTopic && (
              <span className="text-gray-400">No subject/topic filter</span>
            )}
            <span className="text-gray-400" title="Total time spent on this quiz">
              ⏱ {formatQuizTime(elapsedSeconds)}
            </span>
          </div>
        </div>
        <QuizProgress
          currentIndex={currentIndex}
          totalQuestions={questions.length}
          answeredCount={answeredCount}
          className="w-48 shrink-0 pt-1"
        />
      </div>

      <QuizQuestionCard
        question={current}
        questionNumber={currentIndex + 1}
        totalQuestions={questions.length}
        selectedIndex={selected}
        submitted={isSubmitted}
        onSelect={handleSelect}
        onSubmit={handleSubmitAnswer}
        onNext={isSubmitted ? handleNext : undefined}
        isLast={currentIndex === questions.length - 1}
      />

      <div className="mt-6 flex items-center justify-between">
        <button
          type="button"
          disabled={currentIndex === 0}
          onClick={handlePrevious}
          className="rounded border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50"
        >
          Previous
        </button>
        <span className="text-xs text-gray-400">
          {answeredCount} of {questions.length} answered
        </span>
        {!isSubmitted ? (
          <span className="text-xs text-gray-400">Submit an answer to continue</span>
        ) : currentIndex === questions.length - 1 ? (
          <Button onClick={handleNext}>Finish</Button>
        ) : (
          <Button onClick={handleNext}>Next</Button>
        )}
      </div>
    </div>
  )
}
