import React from 'react'

type Props = { message?: string; onRetry?: () => void }

export function ErrorState({ message = 'Something went wrong.', onRetry }: Props) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded border border-red-200 bg-red-50 py-12 px-6 text-center" role="alert">
      <span className="text-sm text-red-600">{message}</span>
      {onRetry && (
        <button
          onClick={onRetry}
          className="px-4 py-2 rounded bg-red-600 text-white hover:bg-red-700"
        >
          Retry
        </button>
      )}
    </div>
  )
}
