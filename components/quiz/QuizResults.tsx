import React from 'react';

export function formatQuizTime(totalSeconds: number): string {
  const safe = Math.max(0, Math.floor(totalSeconds || 0));
  const mins = Math.floor(safe / 60);
  const secs = safe % 60;
  if (mins === 0) return `${secs}s`;
  return `${mins}m ${String(secs).padStart(2, '0')}s`;
}

export type QuizResultDetail = {
  question: string;
  correct: boolean;
  selectedIndex: number | null;
  correctAnswer: number;
};

export type QuizTopicMetric = {
  topic: string;
  attempted: number;
  correct: number;
  incorrect: number;
  accuracy: number;
};

export const WEAK_TOPIC_THRESHOLD = 70;

export function getWeakTopics(topics: QuizTopicMetric[], threshold = WEAK_TOPIC_THRESHOLD): QuizTopicMetric[] {
  return (topics ?? [])
    .filter((t) => (t?.attempted ?? 0) > 0 && (t?.accuracy ?? 0) < threshold)
    .sort((a, b) => a.accuracy - b.accuracy || a.topic.localeCompare(b.topic));
}

type Props = {
  totalQuestions: number;
  correctCount: number;
  /** Elapsed wall-clock time in seconds. */
  timeSpentSeconds: number;
  onRetry: () => void;
  /** Optional per-question breakdown rendered below the summary. */
  details?: QuizResultDetail[];
  retryLabel?: string;
  className?: string;
  /** Number of questions with a recorded answer. Defaults to totalQuestions. */
  attemptedCount?: number;
  /** Number of incorrect answers. Defaults to attempted - correct. */
  incorrectCount?: number;
  /** Mean seconds per question. Defaults to timeSpentSeconds / totalQuestions. */
  averageTimeSeconds?: number;
  /** Per-topic accuracy rows (e.g. Deadlock 82%, Paging 61%). */
  topics?: QuizTopicMetric[];
  /** Topic names to highlight as weak. Defaults to topics below threshold. */
  weakTopics?: string[];
  /** Accuracy % below which a topic counts as weak. Defaults to 70. */
  weakThreshold?: number;
  /** Backend session id the results were persisted under (when available). */
  sessionId?: string | null;
  /** Persistence lifecycle for the server-side quiz session. */
  persistStatus?: 'idle' | 'saving' | 'saved' | 'error';
  /** Persistence failure message (shown when persistStatus is 'error'). */
  persistError?: string | null;
  /** Retry callback for failed persistence. */
  onRetryPersist?: () => void;
};

export type QuizPersistStatus = 'idle' | 'saving' | 'saved' | 'error';

export function QuizResults({
  totalQuestions,
  correctCount,
  timeSpentSeconds,
  onRetry,
  details,
  retryLabel = 'Retry quiz',
  className = '',
  attemptedCount,
  incorrectCount,
  averageTimeSeconds,
  topics,
  weakTopics,
  weakThreshold = WEAK_TOPIC_THRESHOLD,
  sessionId = null,
  persistStatus = 'idle',
  persistError = null,
  onRetryPersist,
}: Props) {
  const safeTotal = Math.max(totalQuestions, 0);
  const safeCorrect = Math.min(Math.max(correctCount, 0), Math.max(safeTotal, 0));
  const safeAttempted = Math.min(
    Math.max(attemptedCount ?? safeTotal, 0),
    Math.max(safeTotal, 0)
  );
  const safeIncorrect =
    incorrectCount !== undefined
      ? Math.min(Math.max(incorrectCount, 0), Math.max(safeAttempted, 0))
      : Math.max(safeAttempted - safeCorrect, 0);
  const accuracy = safeAttempted > 0 ? Math.round((safeCorrect / safeAttempted) * 100) : 0;
  const avgTime =
    averageTimeSeconds !== undefined
      ? Math.max(averageTimeSeconds, 0)
      : safeTotal > 0
        ? Math.max(timeSpentSeconds, 0) / safeTotal
        : 0;

  const topicRows = (topics ?? []).map((t) => ({
    topic: t.topic,
    attempted: Math.max(t.attempted, 0),
    correct: Math.max(Math.min(t.correct, Math.max(t.attempted, 0)), 0),
    incorrect: Math.max(t.incorrect, 0),
    accuracy: Math.min(Math.max(t.accuracy, 0), 100),
  }));
  const weakList =
    weakTopics ??
    getWeakTopics(topicRows, weakThreshold).map((t) => t.topic);
  const weakSet = new Set(weakList);

  return (
    <div
      className={`mx-auto max-w-xl rounded-lg border border-gray-200 bg-white p-8 text-center shadow-sm ${className}`}
    >
      <h2 className="text-2xl font-bold text-gray-900">Quiz complete</h2>
      <p className="mt-1 text-sm text-gray-500">
        You got {safeCorrect} of {safeTotal} correct.
      </p>

      {persistStatus === 'saving' && (
        <p role="status" className="mt-3 text-sm text-gray-500">
          Saving results…
        </p>
      )}
      {persistStatus === 'saved' && sessionId && (
        <p role="status" className="mt-3 text-xs text-gray-400">
          Results saved · Session {sessionId}
        </p>
      )}
      {persistStatus === 'error' && (
        <div
          role="alert"
          className="mt-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-left"
        >
          <p className="text-sm font-semibold text-red-800">
            Could not save results{persistError ? `: ${persistError}` : '.'} Showing local
            metrics.
          </p>
          {onRetryPersist && (
            <button
              type="button"
              onClick={onRetryPersist}
              className="mt-2 rounded border border-red-300 px-3 py-1 text-sm font-medium text-red-700 hover:bg-red-100"
            >
              Retry save
            </button>
          )}
        </div>
      )}

      <div className="mt-6 grid grid-cols-2 gap-3 sm:grid-cols-3">
        <div className="rounded-lg bg-gray-50 px-3 py-4">
          <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">Score</p>
          <p className="mt-1 text-2xl font-bold text-gray-900">
            {safeCorrect}/{safeTotal}
          </p>
        </div>
        <div className="rounded-lg bg-gray-50 px-3 py-4">
          <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">Accuracy</p>
          <p className="mt-1 text-2xl font-bold text-gray-900">{accuracy}%</p>
        </div>
        <div className="rounded-lg bg-gray-50 px-3 py-4">
          <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">Attempted</p>
          <p className="mt-1 text-2xl font-bold text-gray-900">
            {safeAttempted}/{safeTotal}
          </p>
        </div>
        <div className="rounded-lg bg-gray-50 px-3 py-4">
          <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">Correct</p>
          <p className="mt-1 text-2xl font-bold text-green-700">{safeCorrect}</p>
        </div>
        <div className="rounded-lg bg-gray-50 px-3 py-4">
          <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">Incorrect</p>
          <p className="mt-1 text-2xl font-bold text-red-600">{safeIncorrect}</p>
        </div>
        <div className="rounded-lg bg-gray-50 px-3 py-4">
          <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">Avg time</p>
          <p className="mt-1 text-2xl font-bold text-gray-900">{formatQuizTime(avgTime)}</p>
        </div>
      </div>

      <div
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={accuracy}
        aria-label="Accuracy"
        className="mt-4 h-2 w-full overflow-hidden rounded-full bg-gray-200"
      >
        <div
          className={`h-full rounded-full transition-all duration-300 ${
            accuracy >= 70 ? 'bg-green-600' : accuracy >= 40 ? 'bg-yellow-500' : 'bg-red-500'
          }`}
          style={{ width: `${accuracy}%` }}
        />
      </div>

      <p className="mt-2 text-xs text-gray-500">
        Total time {formatQuizTime(timeSpentSeconds)} · Avg {formatQuizTime(avgTime)}/question
      </p>

      {topicRows.length > 0 && (
        <div className="mt-6 text-left">
          <h3 className="text-sm font-bold text-gray-900">Per-topic accuracy</h3>
          <ul className="mt-2 space-y-2">
            {topicRows.map((t) => {
              const isWeak = weakSet.has(t.topic);
              const pct = Math.round(t.accuracy);
              return (
                <li
                  key={t.topic}
                  className={`rounded-lg border px-3 py-2 ${
                    isWeak ? 'border-amber-300 bg-amber-50' : 'border-gray-200 bg-gray-50'
                  }`}
                >
                  <div className="flex items-center justify-between gap-2 text-sm">
                    <span className="font-semibold text-gray-900">
                      {t.topic}
                      {isWeak && (
                        <span className="ml-2 rounded-full bg-amber-200 px-2 py-0.5 text-[11px] font-bold uppercase tracking-wide text-amber-900">
                          Weak
                        </span>
                      )}
                    </span>
                    <span className="text-gray-600">
                      {t.correct}/{t.attempted} · {pct}%
                    </span>
                  </div>
                  <div
                    role="progressbar"
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-valuenow={pct}
                    aria-label={`${t.topic} accuracy`}
                    className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-gray-200"
                  >
                    <div
                      className={`h-full rounded-full ${
                        pct >= 70 ? 'bg-green-600' : pct >= 40 ? 'bg-yellow-500' : 'bg-red-500'
                      }`}
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                </li>
              );
            })}
          </ul>
        </div>
      )}

      {weakList.length > 0 && (
        <div
          role="alert"
          className="mt-4 rounded-lg border border-amber-300 bg-amber-50 px-4 py-3 text-left"
        >
          <p className="text-sm font-bold text-amber-900">
            Weak topics to review ({weakList.length})
          </p>
          <p className="mt-1 text-sm text-amber-800">
            Focus on: {weakList.join(', ')}. Accuracy below {weakThreshold}% — retry these topics.
          </p>
        </div>
      )}

      {details && details.length > 0 && (
        <ul className="mt-6 space-y-2 text-left">
          {details.map((d, i) => (
            <li
              key={i}
              className={`rounded-lg border px-3 py-2 text-sm ${
                d.correct ? 'border-green-200 bg-green-50 text-green-900' : 'border-red-200 bg-red-50 text-red-900'
              }`}
            >
              <span className="font-semibold">Q{i + 1}.</span> {d.question}{' '}
              <span className="font-medium">{d.correct ? '✓' : '✕'}</span>
            </li>
          ))}
        </ul>
      )}

      <div className="mt-6 flex justify-center">
        <button
          type="button"
          onClick={onRetry}
          className="rounded-lg bg-blue-600 px-5 py-2 text-sm font-semibold text-white hover:bg-blue-700"
        >
          {retryLabel}
        </button>
      </div>
    </div>
  );
}

export default QuizResults;
