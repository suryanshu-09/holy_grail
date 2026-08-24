import React from 'react'

type Props = {
  currentStep: number
  totalSteps: number
  label?: string
  className?: string
}

export function ProgressIndicator({ currentStep, totalSteps, label, className = '' }: Props) {
  const safeTotal = Math.max(totalSteps, 1)
  const clampedStep = Math.min(Math.max(currentStep, 0), safeTotal)
  const percent = Math.round((clampedStep / safeTotal) * 100)

  return (
    <div className={className}>
      <div className="mb-1 flex items-center justify-between text-xs text-gray-500">
        <span>{label ?? 'Progress'}</span>
        <span>
          {clampedStep} / {safeTotal}
        </span>
      </div>
      <div
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={safeTotal}
        aria-valuenow={clampedStep}
        className="h-2 w-full overflow-hidden rounded-full bg-gray-200"
      >
        <div
          className="h-full rounded-full bg-blue-600 transition-all duration-300"
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  )
}
