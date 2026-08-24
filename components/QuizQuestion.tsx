import React from 'react'
import { QuestionCard, type QuestionMeta } from './QuestionCard'

type Props = QuestionMeta & {
  options: string[]
  onAnswer: (option: string, index: number) => void
  disabled?: boolean
  selectedIndex?: number | null
  className?: string
}

const LETTERS = ['A', 'B', 'C', 'D', 'E', 'F', 'G', 'H']

export function QuizQuestion({
  options,
  onAnswer,
  disabled = false,
  selectedIndex,
  className = '',
  ...meta
}: Props) {
  return (
    <div className={className}>
      <QuestionCard {...meta} />
      <div className="mt-4 flex flex-col gap-2">
        {options.map((option, index) => {
          const isSelected = selectedIndex === index
          return (
            <button
              key={`${index}-${option}`}
              type="button"
              disabled={disabled}
              onClick={() => onAnswer(option, index)}
              className={`flex items-start gap-3 rounded-lg border px-4 py-3 text-left transition-colors ${
                isSelected
                  ? 'border-blue-600 bg-blue-50'
                  : 'border-gray-200 bg-white hover:bg-gray-50'
              } ${disabled ? 'cursor-not-allowed opacity-60' : ''}`}
            >
              <span
                className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold ${
                  isSelected ? 'bg-blue-600 text-white' : 'bg-gray-100 text-gray-600'
                }`}
              >
                {LETTERS[index] ?? index + 1}
              </span>
              <span className="text-sm text-gray-800">{option}</span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
