import React, { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { QuizQuestionCard } from '../components/quiz/QuizQuestionCard';
import { QuizQuestion } from '../components/QuizQuestion';
import type { QuizQuestion as QuizQuestionType } from '../lib/api';

const q: QuizQuestionType = {
  source_question_id: 'sq-1',
  document_id: 'doc-1',
  question: 'What is 2 + 2?',
  options: ['3', '4', '5', '6'],
  correct_answer: 1,
  explanation: '2 + 2 equals 4.',
};

function Harness({ onSubmit }: { onSubmit?: () => void }) {
  const [selected, setSelected] = useState<number | null>(null);
  const [submitted, setSubmitted] = useState(false);
  return (
    <QuizQuestionCard
      question={q}
      questionNumber={1}
      totalQuestions={4}
      selectedIndex={selected}
      submitted={submitted}
      onSelect={setSelected}
      onSubmit={() => {
        setSubmitted(true);
        onSubmit?.();
      }}
      showSource={false}
    />
  );
}

describe('Answer submission', () => {
  it('disables submit until an option is selected', () => {
    render(<Harness />);
    expect(screen.getByRole('button', { name: /submit answer/i })).toBeDisabled();
    fireEvent.click(screen.getByRole('radio', { name: /4/ }));
    expect(screen.getByRole('button', { name: /submit answer/i })).not.toBeDisabled();
  });

  it('shows correct feedback and explanation after submitting the right answer', () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole('radio', { name: /4/ }));
    fireEvent.click(screen.getByRole('button', { name: /submit answer/i }));
    expect(screen.getByRole('status')).toHaveTextContent(/correct/i);
    expect(screen.getByText(/2 \+ 2 equals 4/)).toBeInTheDocument();
  });

  it('shows the correct letter after a wrong answer', () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole('radio', { name: /3/ }));
    fireEvent.click(screen.getByRole('button', { name: /submit answer/i }));
    expect(screen.getByRole('status')).toHaveTextContent(/correct answer is B/i);
  });

  it('locks options after submission', () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} />);
    fireEvent.click(screen.getByRole('radio', { name: /4/ }));
    fireEvent.click(screen.getByRole('button', { name: /submit answer/i }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    for (const radio of screen.getAllByRole('radio')) {
      expect(radio).toBeDisabled();
    }
  });

  it('QuizQuestion forwards the selected option index via onAnswer', () => {
    const onAnswer = vi.fn();
    render(
      <QuizQuestion
        questionText="Pick one"
        options={['a', 'b', 'c']}
        onAnswer={onAnswer}
        selectedIndex={null}
      />
    );
    fireEvent.click(screen.getByRole('button', { name: /b/ }));
    expect(onAnswer).toHaveBeenCalledWith('b', 1);
  });
});
