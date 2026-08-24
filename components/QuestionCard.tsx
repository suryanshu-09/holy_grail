import React from 'react'

export type QuestionMeta = {
  id?: string
  questionText?: string
  subject?: string
  year?: number
  pageNumber?: number
  difficulty?: string
}

type Props = QuestionMeta & {
  className?: string
}

function MetaChip({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-block rounded-full bg-gray-100 px-2.5 py-0.5 text-xs font-medium text-gray-600">
      {children}
    </span>
  )
}

export function QuestionCard({
  id,
  questionText,
  subject,
  year,
  pageNumber,
  difficulty,
  className = '',
}: Props) {
  const hasMeta = Boolean(subject || year || pageNumber || difficulty)

  return (
    <div
      data-question-id={id}
      className={`rounded-lg border border-gray-200 bg-white p-5 shadow-sm ${className}`}
    >
      <p className="text-base font-medium leading-relaxed text-gray-900">
        {questionText ?? 'No question text available'}
      </p>
      {hasMeta && (
        <div className="mt-4 flex flex-wrap gap-2">
          {subject && <MetaChip>{subject}</MetaChip>}
          {year != null && <MetaChip>{year}</MetaChip>}
          {pageNumber != null && <MetaChip>Page {pageNumber}</MetaChip>}
          {difficulty && <MetaChip>{difficulty}</MetaChip>}
        </div>
      )}
    </div>
  )
}
