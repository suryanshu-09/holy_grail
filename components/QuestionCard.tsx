import React from 'react'
import { getBaseUrl } from '../lib/api'

export type QuestionMeta = {
  id?: string
  questionText?: string
  subject?: string
  year?: number
  pageNumber?: number
  startPage?: number
  endPage?: number
  difficulty?: string
  questionType?: string
  options?: string[]
  images?: string[]
  documentId?: string
  confidence?: number
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

function imageUrl(documentId: string | undefined, name: string, thumb = false): string {
  if (!documentId) return ''
  const base = getBaseUrl()
  const fileName = thumb ? `thumb_${name.replace(/\.(jpg|jpeg)$/i, '.png')}` : name
  return `${base}/documents/${documentId}/images/${fileName}`
}

export function QuestionCard({
  id,
  questionText,
  subject,
  year,
  pageNumber,
  startPage,
  endPage,
  difficulty,
  questionType,
  options,
  images,
  documentId,
  confidence,
  className = '',
}: Props) {
  const hasMeta = Boolean(subject || year || pageNumber || startPage || difficulty || questionType)
  const pageLabel = startPage && endPage && startPage !== endPage ? `Pages ${startPage}–${endPage}` : startPage ? `Page ${startPage}` : pageNumber ? `Page ${pageNumber}` : null

  return (
    <div
      data-question-id={id}
      className={`rounded-lg border border-gray-200 bg-white p-5 shadow-sm ${className}`}
    >
      <p className="whitespace-pre-wrap text-base font-medium leading-relaxed text-gray-900">
        {questionText ?? 'No question text available'}
      </p>
      {options && options.length > 0 && (
        <ul className="mt-3 space-y-1.5">
          {options.map((opt, idx) => (
            <li key={idx} className="flex gap-2 text-sm text-gray-700">
              <span className="font-medium text-gray-500">{String.fromCharCode(65 + idx)}.</span>
              <span>{opt}</span>
            </li>
          ))}
        </ul>
      )}
      {images && images.length > 0 && documentId && (
        <div className="mt-4 space-y-3">
          {images.map((img) => (
            <div key={img} className="overflow-hidden rounded border border-gray-200 bg-gray-50">
              {/* Try thumbnail first; fallback to original via onError */}
              <img
                src={imageUrl(documentId, img, true)}
                alt={`Question image ${img}`}
                className="max-h-80 w-full object-contain"
                onError={(e) => {
                  const target = e.currentTarget as HTMLImageElement
                  if (target.src !== imageUrl(documentId, img, false)) {
                    target.src = imageUrl(documentId, img, false)
                  }
                }}
                loading="lazy"
              />
              <div className="px-2 py-1 text-xs text-gray-500">{img}</div>
            </div>
          ))}
        </div>
      )}
      {hasMeta && (
        <div className="mt-4 flex flex-wrap gap-2">
          {subject && <MetaChip>{subject}</MetaChip>}
          {year != null && <MetaChip>{year}</MetaChip>}
          {pageLabel && <MetaChip>{pageLabel}</MetaChip>}
          {difficulty && <MetaChip>{difficulty}</MetaChip>}
          {questionType && <MetaChip>{questionType}</MetaChip>}
          {confidence != null && <MetaChip>{Math.round(confidence * 100)}% confidence</MetaChip>}
        </div>
      )}
    </div>
  )
}
