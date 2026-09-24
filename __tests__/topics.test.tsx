import React from 'react';
import { describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { TopicSelector } from '../components/TopicSelector';

const topics = [
  { id: 't1', name: 'Algebra', question_count: 5 },
  { id: 't2', name: 'Calculus', question_count: 1 },
  { id: 't3', name: 'Geometry' },
];

describe('Topic selection', () => {
  it('renders placeholder plus all topics with question counts', () => {
    render(<TopicSelector topics={topics} value="" onChange={() => {}} />);
    expect(screen.getByText('Select a topic')).toBeInTheDocument();
    expect(screen.getByText('Algebra — 5 questions')).toBeInTheDocument();
    // Singular form for a single question.
    expect(screen.getByText('Calculus — 1 question')).toBeInTheDocument();
    expect(screen.getByText('Geometry')).toBeInTheDocument();
  });

  it('calls onChange with the selected topic id', () => {
    const onChange = vi.fn();
    render(<TopicSelector topics={topics} value="" onChange={onChange} />);
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 't2' } });
    expect(onChange).toHaveBeenCalledWith('t2');
  });

  it('reflects the controlled value', () => {
    render(<TopicSelector topics={topics} value="t1" onChange={() => {}} />);
    expect(screen.getByRole('combobox')).toHaveValue('t1');
  });

  it('supports a custom placeholder', () => {
    render(
      <TopicSelector topics={topics} value="" onChange={() => {}} placeholder="Pick one" />
    );
    expect(screen.getByText('Pick one')).toBeInTheDocument();
  });
});
