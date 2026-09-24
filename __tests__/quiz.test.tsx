import React from 'react';
import { describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { QuizSetupForm } from '../components/quiz/QuizSetupForm';

describe('Quiz interaction (setup form)', () => {
  it('renders the setup form with defaults', () => {
    render(<QuizSetupForm onSubmit={() => {}} />);
    expect(screen.getByText('Set up your quiz')).toBeInTheDocument();
    expect(screen.getByLabelText(/number of questions/i)).toHaveValue(10);
    expect(screen.getByRole('button', { name: /generate quiz/i })).toBeInTheDocument();
  });

  it('submits default mode and length', () => {
    const onSubmit = vi.fn();
    render(<QuizSetupForm onSubmit={onSubmit} />);
    fireEvent.click(screen.getByRole('button', { name: /generate quiz/i }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ mode: 'mcq', num_questions: 10, length: 10 })
    );
  });

  it('maps topic text to topic + topics fields', () => {
    const onSubmit = vi.fn();
    render(<QuizSetupForm onSubmit={onSubmit} />);
    fireEvent.change(screen.getByLabelText(/topic/i), { target: { value: 'Algebra' } });
    fireEvent.change(screen.getByLabelText(/subject/i), { target: { value: 'Mathematics' } });
    fireEvent.click(screen.getByRole('button', { name: /generate quiz/i }));
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ topic: 'Algebra', topics: ['Algebra'], subject: 'Mathematics' })
    );
  });

  it('clamps question count into 1–50', () => {
    const onSubmit = vi.fn();
    const { container } = render(<QuizSetupForm onSubmit={onSubmit} />);
    fireEvent.change(screen.getByLabelText(/number of questions/i), { target: { value: '999' } });
    // Submit the form directly: native constraint validation (max=50) would
    // block a button click, while the component clamps programmatically.
    fireEvent.submit(container.querySelector('form') as HTMLFormElement);
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ num_questions: 50 }));
  });

  it('shows the error and disables submit while loading', () => {
    render(<QuizSetupForm onSubmit={() => {}} loading error="No sources found" />);
    expect(screen.getByRole('alert')).toHaveTextContent(/no sources found/i);
    expect(screen.getByRole('button', { name: /generating/i })).toBeDisabled();
  });
});
