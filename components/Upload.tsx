import React, { useCallback, useRef, useState } from 'react'
import { uploadDocument, type Document as DocumentRecord } from '../lib/api'
import { Button } from './ui'

const MAX_FILE_SIZE_BYTES = 50 * 1024 * 1024
const MAX_FILE_SIZE_MB = MAX_FILE_SIZE_BYTES / (1024 * 1024)

interface UploadProps {
  onUploaded?: (document: DocumentRecord) => void
}

function validateFile(file: File): string | null {
  const isPdf = file.type === 'application/pdf' || /\.pdf$/i.test(file.name)
  if (!isPdf) {
    return `"${file.name}" was rejected: only PDF files are allowed.`
  }
  if (file.size === 0) {
    return `"${file.name}" was rejected: the file is empty.`
  }
  if (file.size > MAX_FILE_SIZE_BYTES) {
    const sizeMb = (file.size / (1024 * 1024)).toFixed(1)
    return `"${file.name}" was rejected: the file is ${sizeMb} MB, which exceeds the ${MAX_FILE_SIZE_MB} MB limit.`
  }
  return null
}

export function Upload({ onUploaded }: UploadProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [dragActive, setDragActive] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [progress, setProgress] = useState(0)
  const [activeFilename, setActiveFilename] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const handleFiles = useCallback(
    async (files: FileList | null) => {
      const file = files?.[0]
      if (!file || uploading) {
        return
      }
      const rejection = validateFile(file)
      if (rejection) {
        setError(rejection)
        return
      }
      setError(null)
      setActiveFilename(file.name)
      setProgress(0)
      setUploading(true)
      try {
        const document = await uploadDocument(file, setProgress)
        setProgress(100)
        onUploaded?.(document)
      } catch (err) {
        setError(err instanceof Error ? err.message : `Failed to upload "${file.name}".`)
      } finally {
        setUploading(false)
      }
    },
    [uploading, onUploaded]
  )

  const openPicker = () => {
    if (!uploading) {
      inputRef.current?.click()
    }
  }

  const handleDragOver = (event: React.DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    event.stopPropagation()
    if (!uploading) {
      setDragActive(true)
    }
  }

  const handleDragLeave = (event: React.DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    event.stopPropagation()
    setDragActive(false)
  }

  const handleDrop = (event: React.DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    event.stopPropagation()
    setDragActive(false)
    void handleFiles(event.dataTransfer.files)
  }

  return (
    <div>
      <div
        role="button"
        tabIndex={uploading ? -1 : 0}
        aria-disabled={uploading}
        onClick={openPicker}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault()
            openPicker()
          }
        }}
        onDragEnter={handleDragOver}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
        className={`rounded-lg border-2 border-dashed p-8 text-center transition-colors ${
          dragActive ? 'border-blue-500 bg-blue-50' : 'border-gray-300'
        } ${uploading ? 'opacity-60' : 'cursor-pointer hover:border-blue-400 hover:bg-gray-50'}`}
      >
        <p className="text-sm text-gray-600">Drag &amp; drop a PDF here, or click to browse</p>
        <Button
          className="mt-4"
          disabled={uploading}
          onClick={(event) => {
            event.stopPropagation()
            openPicker()
          }}
        >
          {uploading ? 'Uploading...' : 'Choose File'}
        </Button>
        <p className="mt-2 text-xs text-gray-400">PDF only, up to {MAX_FILE_SIZE_MB} MB.</p>
        <input
          ref={inputRef}
          type="file"
          accept="application/pdf,.pdf"
          className="hidden"
          onChange={(event) => {
            void handleFiles(event.target.files)
            event.target.value = ''
          }}
        />
      </div>

      {uploading && (
        <div className="mt-3">
          <div className="flex items-center justify-between gap-2 text-xs text-gray-500">
            <span className="truncate">Uploading {activeFilename}...</span>
            <span>{progress}%</span>
          </div>
          <div className="mt-1 h-2 w-full overflow-hidden rounded-full bg-gray-200">
            <div
              className="h-full rounded-full bg-blue-600 transition-all"
              style={{ width: `${progress}%` }}
            />
          </div>
        </div>
      )}

      {error && (
        <div role="alert" className="mt-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          {error}
        </div>
      )}
    </div>
  )
}
