import React from 'react';
import { ProgressIndicator } from '../ProgressIndicator';

type Props = {
  /** Zero-based index of the current question. */
  currentIndex: number;
  totalQuestions: number;
  answeredCount?: number;
  label?: string;
  className?: string;
};

/**
 * Thin wrapper around the shared ProgressIndicator, tailored for quizzes.
 * `currentIndex` is zero-based; the bar shows answered/current progress.
 */
export function QuizProgress({
  currentIndex,
  totalQuestions,
  answeredCount,
  label,
  className = '',
}: Props) {
  const safeTotal = Math.max(totalQuestions, 1);
  const step = Math.min(Math.max(currentIndex + 1, 0), safeTotal);
  const resolvedLabel =
    label ?? (answeredCount !== undefined ? `Question ${step} of ${safeTotal} · ${answeredCount} answered` : 'Question');

  return (
    <ProgressIndicator
      currentStep={step}
      totalSteps={safeTotal}
      label={resolvedLabel}
      className={className}
    />
  );
}

export default QuizProgress;
