import React from 'react'

export function LoadingState({ label = 'Loading...' }: { label?: string }) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-12" role="status" aria-live="polite">
      <div className="h-8 w-8 animate-spin rounded-full border-4 border-gray-300 border-t-blue-600" />
      <span className="text-sm text-gray-500">{label}</span>
    </div>
  )
}

export function Skeleton({ className = '' }: { className?: string }) {
  return <div className={`animate-pulse rounded bg-gray-200 ${className}`} />
}

export function SkeletonList({ count = 3 }: { count?: number }) {
  return (
    <div className="space-y-4">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="rounded border border-gray-200 p-4">
          <Skeleton className="h-4 w-1/3 mb-2" />
          <Skeleton className="h-3 w-2/3" />
        </div>
      ))}
    </div>
  )
}
