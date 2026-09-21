import React, { useState } from 'react';
import type { QuizFilters, QuizMode } from '../../lib/api';

export interface QuizSetupValues extends QuizFilters {
  mode: QuizMode;
  num_questions: number;
}

export const QUIZ_MODES: Array<{ value: QuizMode; label: string; hint: string }> = [
  { value: 'original', label: 'Original', hint: 'Questions as extracted' },
  { value: 'mcq', label: 'MCQ', hint: 'Multiple choice with distractors' },
  { value: 'similar', label: 'Similar', hint: 'Similar to a search query' },
  { value: 'mixed', label: 'Mixed', hint: 'Blend of modes' },
];

export const QUIZ_DIFFICULTIES = ['', 'easy', 'medium', 'hard'] as const;

type Props = {
  initial?: Partial<QuizSetupValues>;
  loading?: boolean;
  error?: string | null;
  onSubmit: (filters: QuizFilters) => void;
  className?: string;
};

const DEFAULTS: QuizSetupValues = {
  mode: 'mcq',
  num_questions: 10,
  difficulty: '',
  topic: '',
  subject: '',
  query: '',
};

export function QuizSetupForm({ initial, loading = false, error, onSubmit, className = '' }: Props) {
  const [mode, setMode] = useState<QuizMode>(initial?.mode ?? DEFAULTS.mode);
  const [numQuestions, setNumQuestions] = useState<number>(
    initial?.num_questions ?? initial?.length ?? initial?.limit ?? DEFAULTS.num_questions
  );
  const [difficulty, setDifficulty] = useState<string>(initial?.difficulty ?? '');
  const [topic, setTopic] = useState<string>(
    initial?.topic ?? (initial?.topics && initial.topics.length > 0 ? initial.topics[0] : '')
  );
  const [subject, setSubject] = useState<string>(initial?.subject ?? '');
  const [query, setQuery] = useState<string>(initial?.query ?? initial?.q ?? '');

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const clamped = Math.min(Math.max(Math.round(numQuestions) || 1, 1), 50);
    const filters: QuizFilters = {
      mode,
      num_questions: clamped,
      length: clamped,
    };
    if (difficulty.trim()) filters.difficulty = difficulty.trim();
    if (topic.trim()) {
      filters.topic = topic.trim();
      filters.topics = [topic.trim()];
    }
    if (subject.trim()) filters.subject = subject.trim();
    if (query.trim()) filters.query = query.trim();
    onSubmit(filters);
  };

  const inputCls =
    'w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 placeholder:text-gray-400 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500';
  const labelCls = 'mb-1 block text-xs font-semibold uppercase tracking-wide text-gray-500';

  return (
    <form
      onSubmit={handleSubmit}
      className={`rounded-lg border border-gray-200 bg-white p-6 shadow-sm ${className}`}
    >
      <h2 className="text-lg font-semibold text-gray-900">Set up your quiz</h2>
      <p className="mt-1 text-sm text-gray-500">Choose a mode, length, and optional filters.</p>

      <div className="mt-5 grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <label htmlFor="quiz-mode" className={labelCls}>
            Mode
          </label>
          <select
            id="quiz-mode"
            value={mode}
            onChange={(e) => setMode(e.target.value as QuizMode)}
            className={inputCls}
          >
            {QUIZ_MODES.map((m) => (
              <option key={m.value} value={m.value}>
                {m.label} — {m.hint}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label htmlFor="quiz-length" className={labelCls}>
            Number of questions (1–50)
          </label>
          <input
            id="quiz-length"
            type="number"
            min={1}
            max={50}
            value={numQuestions}
            onChange={(e) => setNumQuestions(Number(e.target.value))}
            className={inputCls}
          />
        </div>

        <div>
          <label htmlFor="quiz-difficulty" className={labelCls}>
            Difficulty
          </label>
          <select
            id="quiz-difficulty"
            value={difficulty}
            onChange={(e) => setDifficulty(e.target.value)}
            className={inputCls}
          >
            <option value="">Any difficulty</option>
            <option value="easy">Easy</option>
            <option value="medium">Medium</option>
            <option value="hard">Hard</option>
          </select>
        </div>

        <div>
          <label htmlFor="quiz-subject" className={labelCls}>
            Subject
          </label>
          <input
            id="quiz-subject"
            type="text"
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            placeholder="e.g. Mathematics"
            className={inputCls}
          />
        </div>

        <div>
          <label htmlFor="quiz-topic" className={labelCls}>
            Topic
          </label>
          <input
            id="quiz-topic"
            type="text"
            value={topic}
            onChange={(e) => setTopic(e.target.value)}
            placeholder="e.g. Algebra"
            className={inputCls}
          />
        </div>

        <div>
          <label htmlFor="quiz-query" className={labelCls}>
            Search query
          </label>
          <input
            id="quiz-query"
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="e.g. quadratic equations"
            className={inputCls}
          />
        </div>
      </div>

      {error && (
        <p role="alert" className="mt-4 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </p>
      )}

      <div className="mt-6 flex items-center justify-end gap-3">
        <button
          type="submit"
          disabled={loading}
          className="rounded-lg bg-blue-600 px-5 py-2 text-sm font-semibold text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {loading ? 'Generating…' : 'Generate quiz'}
        </button>
      </div>
    </form>
  );
}

export default QuizSetupForm;
