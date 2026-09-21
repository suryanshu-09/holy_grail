import React, { useCallback, useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { QuizProgress, QuizQuestionCard, QuizResults, QuizSetupForm, formatQuizTime } from '../../components/quiz'
import type { QuizResultDetail, QuizTopicMetric } from '../../components/quiz'
import { Button, EmptyState, ErrorState, LoadingState } from '../../components/ui'
import { generateQuiz, createQuizSession, submitQuizAttempts, getQuizSession, type QuizFilters, type QuizQuestion, type QuizAttemptInput, type QuizSessionDetail } from '../../lib/api'

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

  // Persistence: server-side quiz session + metrics fetched after completion.
  const [persistStatus, setPersistStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')
  const [persistError, setPersistError] = useState<string | null>(null)
  const [sessionId, setSessionId] = useState<string | null>(null)
  const [serverDetail, setServerDetail] = useState<QuizSessionDetail | null>(null)
  const [runId, setRunId] = useState(0)
  const persistedRunRef = useRef<number>(-1)

  const startQuiz = useCallback(async (nextFilters: QuizFilters) => {
    setLoading(true)
    setError(null)
    setFilters(nextFilters)
    // Reset persistence for the new run.
    setPersistStatus('idle')
    setPersistError(null)
    setSessionId(null)
    setServerDetail(null)
    setRunId((r) => r + 1)
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

  // Topic attribution shared by the attempt payload and the local fallback
  // metrics: quiz items carry no topic, so attribute via the active filters.
  const resolveTopicNames = useCallback((): string[] => {
    const filterTopics = (filters?.topics ?? []).map((t) => t.trim()).filter(Boolean)
    if (filters?.topic?.trim()) filterTopics.unshift(filters.topic.trim())
    const deduped = Array.from(new Set(filterTopics))
    return deduped.length > 0 ? deduped : [filters?.subject?.trim() || 'General']
  }, [filters])

  // Persist completed quiz: create session, bulk-submit answered attempts,
  // then fetch server-computed metrics + per-topic breakdown + weak topics.
  // Server metrics take precedence; local computation remains as fallback.
  const persistResults = useCallback(async () => {
    if (questions.length === 0) return
    setPersistStatus('saving')
    setPersistError(null)
    try {
      const topicNames = resolveTopicNames()
      const totalSeconds =
        quizStart !== null && quizEnd !== null
          ? Math.max(0, Math.floor((quizEnd - quizStart) / 1000))
          : elapsedSeconds
      const perQuestionTime = questions.length > 0 ? totalSeconds / questions.length : 0
      const subject = filters?.subject?.trim() || undefined

      const session = await createQuizSession({
        mode: filters?.mode,
        num_questions: questions.length,
        subject,
        topic: filters?.topic?.trim() || undefined,
        topics: (filters?.topics ?? []).map((t) => t.trim()).filter(Boolean) || undefined,
      })
      setSessionId(session.id)

      const attempts: QuizAttemptInput[] = []
      questions.forEach((q, i) => {
        const selected = selections[i]
        if (selected === null || selected === undefined) return
        attempts.push({
          question_id: q.id || q.source_question_id || `question-${i}`,
          source_question_id: q.source_question_id || undefined,
          question_text: q.question || undefined,
          selected_answer: selected,
          correct_answer: q.correct_answer,
          topic: topicNames[i % topicNames.length] || undefined,
          subject: subject ?? undefined,
          time_taken_seconds: Math.max(perQuestionTime, 0),
        })
      })
      if (attempts.length > 0) {
        await submitQuizAttempts(session.id, attempts)
      }
      const detail = await getQuizSession(session.id)
      setServerDetail(detail)
      setPersistStatus('saved')
    } catch (err) {
      setPersistError(err instanceof Error ? err.message : 'Failed to save quiz results.')
      setPersistStatus('error')
    }
  }, [questions, selections, filters, quizStart, quizEnd, elapsedSeconds, resolveTopicNames])

  // Fire persistence once per completed run.
  useEffect(() => {
    if (phase !== 'results') return
    if (persistedRunRef.current === runId) return
    persistedRunRef.current = runId
    void persistResults()
  }, [phase, runId, persistResults])

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
    const incorrectCount = Math.max(attemptedCount - correctCount, 0)
    const totalSeconds =
      quizStart !== null && quizEnd !== null
        ? Math.max(0, Math.floor((quizEnd - quizStart) / 1000))
        : elapsedSeconds
    const averageTimeSeconds = totalQuestions > 0 ? totalSeconds / totalQuestions : 0
    const details: QuizResultDetail[] = questions.map((q, i) => ({
      question: q.question,
      correct: submitted[i] && selections[i] !== null && selections[i] === q.correct_answer,
      selectedIndex: selections[i],
      correctAnswer: q.correct_answer,
    }))

    // Per-topic accuracy: quiz items carry no topic, so attribute via the
    // active filters. Multiple topics are distributed round-robin so each
    // topic gets a deterministic share; single topic/subject collapses to one row.
    // Local fallback — replaced by server metrics once persistence succeeds.
    const topicNames = resolveTopicNames()
    const buckets = new Map<string, { attempted: number; correct: number }>()
    for (const name of topicNames) buckets.set(name, { attempted: 0, correct: 0 })
    questions.forEach((q, i) => {
      const name = topicNames[i % topicNames.length]
      const b = buckets.get(name)
      if (!b) return
      if (selections[i] !== null) {
        b.attempted += 1
        if (submitted[i] && selections[i] === q.correct_answer) b.correct += 1
      }
    })
    const topicMetrics: QuizTopicMetric[] = Array.from(buckets.entries()).map(([topic, b]) => ({
      topic,
      attempted: b.attempted,
      correct: b.correct,
      incorrect: Math.max(b.attempted - b.correct, 0),
      accuracy: b.attempted > 0 ? (b.correct / b.attempted) * 100 : 0,
    }))
    const weakTopics = topicMetrics
      .filter((t) => t.attempted > 0 && t.accuracy < 70)
      .sort((a, b) => a.accuracy - b.accuracy)
      .map((t) => t.topic)

    // Prefer server-computed metrics once persisted; fall back to local.
    const serverMetrics = serverDetail?.metrics
    const displayCorrect = serverMetrics ? serverMetrics.questions_correct : correctCount
    const displayAttempted = serverMetrics ? serverMetrics.questions_attempted : attemptedCount
    const displayIncorrect = serverMetrics ? serverMetrics.questions_incorrect : incorrectCount
    const displayAvgTime =
      serverMetrics && persistStatus === 'saved'
        ? serverMetrics.average_time_seconds
        : averageTimeSeconds
    const displayTopics: QuizTopicMetric[] = serverDetail
      ? serverDetail.topics.map((t) => ({
          topic: t.topic,
          attempted: t.attempted,
          correct: t.correct,
          incorrect: t.incorrect,
          accuracy: t.accuracy,
        }))
      : topicMetrics
    const displayWeakTopics =
      serverDetail && persistStatus === 'saved' ? serverDetail.weak_topics : weakTopics

    const handleRetryPersist = () => {
      persistedRunRef.current = -1
      setPersistStatus('idle')
      setPersistError(null)
      persistedRunRef.current = runId
      void persistResults()
    }

    return (
      <div className="mx-auto max-w-2xl">
        {persistStatus === 'saving' && (
          <p role="status" className="mb-3 text-center text-sm text-gray-500">
            Saving results and computing metrics…
          </p>
        )}
        <QuizResults
          totalQuestions={totalQuestions}
          correctCount={displayCorrect}
          timeSpentSeconds={totalSeconds}
          onRetry={handleRetry}
          details={details}
          attemptedCount={displayAttempted}
          incorrectCount={displayIncorrect}
          averageTimeSeconds={displayAvgTime}
          topics={displayTopics}
          weakTopics={displayWeakTopics}
          sessionId={sessionId}
          persistStatus={persistStatus}
          persistError={persistError}
          onRetryPersist={handleRetryPersist}
        />
        <p className="mt-3 text-center text-sm text-gray-500">
          Time spent: {formatQuizTime(totalSeconds)} · Avg {formatQuizTime(displayAvgTime)}
          /question · Attempted {displayAttempted} of {totalQuestions} · Correct{' '}
          {displayCorrect} · Incorrect {displayIncorrect}
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
