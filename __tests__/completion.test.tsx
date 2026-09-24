import React from 'react';
import { describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { QuizResults } from '../components/quiz/QuizResults';

vi.mock('next/link', () => ({
  default: ({ href, children, className }: any) => (
    <a href={typeof href === 'string' ? href : String(href)} className={className}>
      {children}
    </a>
  ),
}));

describe('Quiz completion', () => {
  it('renders score, accuracy, and counts', () => {
    render(
      <QuizResults totalQuestions={4} correctCount={3} timeSpentSeconds={120} onRetry={() => {}} />
    );
    expect(screen.getByText('Quiz complete')).toBeInTheDocument();
    expect(screen.getByText('You got 3 of 4 correct.')).toBeInTheDocument();
    expect(screen.getByText('75%')).toBeInTheDocument();
    expect(screen.getByRole('progressbar', { name: 'Accuracy' })).toHaveAttribute(
      'aria-valuenow',
      '75'
    );
  });

  it('renders per-topic rows and flags weak topics', () => {
    render(
      <QuizResults
        totalQuestions={4}
        correctCount={2}
        timeSpentSeconds={60}
        onRetry={() => {}}
        topics={[
          { topic: 'Algebra', attempted: 2, correct: 2, incorrect: 0, accuracy: 100 },
          { topic: 'Calculus', attempted: 2, correct: 0, incorrect: 2, accuracy: 0 },
        ]}
      />
    );
    expect(screen.getByText('Per-topic accuracy')).toBeInTheDocument();
    expect(screen.getByText('Algebra')).toBeInTheDocument();
    expect(screen.getByText('Calculus')).toBeInTheDocument();
    expect(screen.getByText('Weak')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent(/weak topics to review/i);
    expect(screen.getByRole('alert')).toHaveTextContent(/calculus/i);
  });

  it('renders per-question breakdown with source links', () => {
    render(
      <QuizResults
        totalQuestions={1}
        correctCount={1}
        timeSpentSeconds={10}
        onRetry={() => {}}
        details={[
          {
            question: 'What is 2 + 2?',
            correct: true,
            selectedIndex: 1,
            correctAnswer: 1,
            sourceQuestionId: 'sq-1',
            documentId: 'doc-1',
          },
        ]}
      />
    );
    expect(screen.getByText(/what is 2 \+ 2/i)).toBeInTheDocument();
    expect(screen.getByText('✓')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'doc-1' })).toHaveAttribute(
      'href',
      '/documents/doc-1'
    );
  });

  it('shows saved session state and calls onRetry', () => {
    const onRetry = vi.fn();
    render(
      <QuizResults
        totalQuestions={2}
        correctCount={2}
        timeSpentSeconds={30}
        onRetry={onRetry}
        sessionId="sess-123"
        persistStatus="saved"
      />
    );
    expect(screen.getByText(/results saved · session sess-123/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /retry quiz/i }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('shows persistence errors with a retry-save action', () => {
    const onRetryPersist = vi.fn();
    render(
      <QuizResults
        totalQuestions={2}
        correctCount={1}
        timeSpentSeconds={30}
        onRetry={() => {}}
        persistStatus="error"
        persistError="boom"
        onRetryPersist={onRetryPersist}
      />
    );
    expect(screen.getByRole('alert')).toHaveTextContent(/could not save results: boom/i);
    fireEvent.click(screen.getByRole('button', { name: /retry save/i }));
    expect(onRetryPersist).toHaveBeenCalledTimes(1);
  });
});
