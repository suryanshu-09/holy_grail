import React from 'react'
import type { ProcessingStatus as ProcessingStatusData } from '../lib/api'

type Props = {
  status: ProcessingStatusData | null
  loading?: boolean
  className?: string
}

function stepIcon(state: string): string {
  switch (state) {
    case 'done':
      return '✓'
    case 'active':
      return '→'
    case 'failed':
      return '✗'
    case 'pending':
    default:
      return '○'
  }
}

function stepColor(state: string): string {
  switch (state) {
    case 'done':
      return 'text-green-600'
    case 'active':
      return 'text-blue-600'
    case 'failed':
      return 'text-red-600'
    case 'pending':
    default:
      return 'text-gray-400'
  }
}

function isTerminal(status: string): boolean {
  return status === 'completed' || status === 'failed'
}

export function ProcessingStatus({ status, loading = false, className = '' }: Props) {
  if (loading && !status) {
    return <p className={`text-sm text-gray-500 ${className}`}>Loading processing status…</p>
  }
  if (!status) {
    return null
  }

  const progress = Math.min(Math.max(status.progress ?? 0, 0), 100)
  const heading =
    status.status === 'idle'
      ? 'Not processing yet'
      : status.status === 'completed'
        ? 'Processing complete'
        : status.status === 'failed'
          ? 'Processing failed'
          : 'Processing...'

  return (
    <div className={className} aria-live="polite">
      <p className="text-sm font-medium text-gray-700">{heading}</p>
      <ul className="mt-2 space-y-1">
        {status.steps.map((step) => (
          <li key={step.key} className="flex items-center gap-2 text-sm">
            <span aria-hidden="true" className={`inline-block w-4 text-center font-medium ${stepColor(step.state)}`}>
              {stepIcon(step.state)}
            </span>
            <span className={step.state === 'pending' ? 'text-gray-500' : 'text-gray-800'}>{step.label}</span>
          </li>
        ))}
      </ul>
      <div className="mt-3">
        <div className="mb-1 flex items-center justify-between text-xs text-gray-500">
          <span>{status.current_step ? `Current: ${status.current_step}` : status.status}</span>
          <span>{progress}%</span>
        </div>
        <div
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={progress}
          aria-label="Processing progress"
          className="h-2 w-full overflow-hidden rounded-full bg-gray-200"
        >
          <div
            className={`h-full rounded-full transition-all duration-300 ${
              status.status === 'failed' ? 'bg-red-500' : isTerminal(status.status) ? 'bg-green-600' : 'bg-blue-600'
            }`}
            style={{ width: `${progress}%` }}
          />
        </div>
      </div>
      {status.last_error && <p className="mt-2 text-sm text-red-600">{status.last_error}</p>}
    </div>
  )
}

export default ProcessingStatus
