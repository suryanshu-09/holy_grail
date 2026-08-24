import React from 'react'

type Props = { message?: string; children?: React.ReactNode }

export function EmptyState({ message = 'Nothing here yet.', children }: Props) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded border border-dashed border-gray-300 py-12 px-6 text-center">
      <span className="text-sm text-gray-500">{message}</span>
      {children}
    </div>
  )
}
