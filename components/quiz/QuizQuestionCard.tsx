import React from 'react';
import type { QuizQuestion as QuizQuestionType, SourceQuestion } from '../../lib/api';
import { SourceTraceability } from './SourceTraceability';

const LETTERS = ['A', 'B', 'C', 'D', 'E', 'F', 'G', 'H'];

type Props = {
  question: QuizQuestionType;
  questionNumber?: number;
  totalQuestions?: number;
  /** Currently selected option index (null = none). Controlled by parent. */
  selectedIndex: number | null;
  /** True once the user pressed Submit — locks the card and reveals feedback. */
  submitted: boolean;
  onSelect: (index: number) => void;
  onSubmit: () => void;
  /** Optional Next handler rendered after submission. */
  onNext?: () => void;
  isLast?: boolean;
  submitting?: boolean;
  className?: string;
  /** Show the source-traceability panel (View Original). Defaults to true. */
  showSource?: boolean;
  /** Pre-fetched original PYQ; when omitted it is fetched lazily on toggle. */
  source?: SourceQuestion | null;
};

function optionClassName(opts: {
  index: number;
  selectedIndex: number | null;
  submitted: boolean;
  correctAnswer: number;
}): string {
  const { index, selectedIndex, submitted, correctAnswer } = opts;
  const base =
    'flex w-full items-start gap-3 rounded-lg border px-4 py-3 text-left text-sm transition-colors';
  if (!submitted) {
    return (
      base +
      (selectedIndex === index
        ? ' border-blue-600 bg-blue-50 text-gray-900'
        : ' border-gray-200 bg-white text-gray-800 hover:border-gray-300 hover:bg-gray-50')
    );
  }
  if (index === correctAnswer) {
    return base + ' border-green-600 bg-green-50 text-gray-900';
  }
  if (index === selectedIndex) {
    return base + ' border-red-500 bg-red-50 text-gray-900';
  }
  return base + ' border-gray-200 bg-white text-gray-500';
}

export function QuizQuestionCard({
  question,
  questionNumber,
  totalQuestions,
  selectedIndex,
  submitted,
  onSelect,
  onSubmit,
  onNext,
  isLast = false,
  submitting = false,
  className = '',
  showSource = true,
  source = null,
}: Props) {
  const isCorrect = submitted && selectedIndex !== null && selectedIndex === question.correct_answer;
  const isIncorrect = submitted && selectedIndex !== null && selectedIndex !== question.correct_answer;
  const showHint = submitted && selectedIndex === null;

  return (
    <div className={`rounded-lg border border-gray-200 bg-white p-6 shadow-sm ${className}`}>
      {(questionNumber !== undefined || totalQuestions !== undefined) && (
        <p className="mb-1 text-xs font-semibold uppercase tracking-wide text-gray-400">
          Question{questionNumber !== undefined ? ` ${questionNumber}` : ''}
          {totalQuestions !== undefined ? ` of ${totalQuestions}` : ''}
        </p>
      )}
      <p className="whitespace-pre-wrap text-base font-medium leading-relaxed text-gray-900">
        {question.question}
      </p>

      <div className="mt-4 flex flex-col gap-2" role="radiogroup" aria-label="Answer options">
        {question.options.map((option, index) => {
          const isSelected = selectedIndex === index;
          const isCorrectOption = submitted && index === question.correct_answer;
          return (
            <button
              key={`${index}-${option}`}
              type="button"
              role="radio"
              aria-checked={isSelected}
              disabled={submitted || submitting}
              onClick={() => onSelect(index)}
              className={`${optionClassName({
                index,
                selectedIndex,
                submitted,
                correctAnswer: question.correct_answer,
              })} ${(submitted || submitting) && !isSelected && !isCorrectOption ? 'opacity-70' : ''} ${
                submitted ? 'cursor-default' : ''
              } ${!submitted && !submitting ? 'cursor-pointer' : ''} disabled:cursor-not-allowed`}
            >
              <span
                className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold ${
                  isCorrectOption
                    ? 'bg-green-600 text-white'
                    : submitted && isSelected
                      ? 'bg-red-500 text-white'
                      : isSelected
                        ? 'bg-blue-600 text-white'
                        : 'bg-gray-100 text-gray-600'
                }`}
              >
                {LETTERS[index] ?? index + 1}
              </span>
              <span className="flex-1">{option}</span>
              {submitted && isCorrectOption && (
                <span aria-label="Correct answer" className="shrink-0 font-bold text-green-700">
                  ✓
                </span>
              )}
              {submitted && isSelected && !isCorrectOption && (
                <span aria-label="Your answer" className="shrink-0 font-bold text-red-600">
                  ✕
                </span>
              )}
            </button>
          );
        })}
      </div>

      {/* Action row */}
      <div className="mt-5 flex items-center gap-3">
        {!submitted ? (
          <button
            type="button"
            onClick={onSubmit}
            disabled={selectedIndex === null || submitting}
            className="rounded-lg bg-blue-600 px-5 py-2 text-sm font-semibold text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {submitting ? 'Submitting…' : 'Submit answer'}
          </button>
        ) : (
          onNext && (
            <button
              type="button"
              onClick={onNext}
              className="rounded-lg bg-blue-600 px-5 py-2 text-sm font-semibold text-white hover:bg-blue-700"
            >
              {isLast ? 'See results' : 'Next question'}
            </button>
          )
        )}
        {!submitted && selectedIndex === null && (
          <span className="text-xs text-gray-400">Select an option to submit.</span>
        )}
      </div>

      {/* Feedback */}
      {submitted && (isCorrect || isIncorrect) && (
        <div
          role="status"
          className={`mt-4 rounded-lg px-4 py-3 text-sm font-medium ${
            isCorrect ? 'bg-green-50 text-green-800' : 'bg-red-50 text-red-700'
          }`}
        >
          {isCorrect
            ? 'Correct — nice work!'
            : `Not quite. The correct answer is ${LETTERS[question.correct_answer] ?? question.correct_answer + 1}.`}
        </div>
      )}
      {showHint && (
        <p className="mt-4 text-sm text-gray-400">No answer selected.</p>
      )}

      {/* Explanation */}
      {submitted && question.explanation && (
        <div className="mt-3 rounded-lg border border-gray-100 bg-gray-50 px-4 py-3">
          <p className="text-xs font-semibold uppercase tracking-wide text-gray-500">Explanation</p>
          <p className="mt-1 whitespace-pre-wrap text-sm leading-relaxed text-gray-700">
            {question.explanation}
          </p>
        </div>
      )}

      {/* Source traceability — lazy View Original with page badge, document
          backlink, original wording comparison and original images. */}
      {showSource && question.source_question_id && question.document_id && (
        <SourceTraceability
          sourceQuestionId={question.source_question_id}
          documentId={question.document_id}
          generatedWording={question.question}
          source={source}
          className="mt-3"
        />
      )}
    </div>
  );
}

export default QuizQuestionCard;
