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
};

export function QuizResults({
  totalQuestions,
  correctCount,
  timeSpentSeconds,
  onRetry,
  details,
  retryLabel = 'Retry quiz',
  className = '',
}: Props) {
  const safeTotal = Math.max(totalQuestions, 0);
  const safeCorrect = Math.min(Math.max(correctCount, 0), Math.max(safeTotal, 0));
  const accuracy = safeTotal > 0 ? Math.round((safeCorrect / safeTotal) * 100) : 0;

  return (
    <div
      className={`mx-auto max-w-xl rounded-lg border border-gray-200 bg-white p-8 text-center shadow-sm ${className}`}
    >
      <h2 className="text-2xl font-bold text-gray-900">Quiz complete</h2>
      <p className="mt-1 text-sm text-gray-500">
        You got {safeCorrect} of {safeTotal} correct.
      </p>

      <div className="mt-6 grid grid-cols-3 gap-3">
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
          <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">Time spent</p>
          <p className="mt-1 text-2xl font-bold text-gray-900">{formatQuizTime(timeSpentSeconds)}</p>
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
