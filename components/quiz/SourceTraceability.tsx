import React, { useEffect, useState } from 'react';
import Link from 'next/link';
import {
  getDocumentImageUrl,
  getQuestionById,
  type QuizQuestion,
  type SourceQuestion,
} from '../../lib/api';

type Props = {
  /** Original PYQ id (QuizQuestion.source_question_id). */
  sourceQuestionId: string;
  /** Owning document id (QuizQuestion.document_id). Linked to /documents/[id]. */
  documentId: string;
  /**
   * Convenience: pass the generated quiz question directly instead of the
   * individual ids. Explicit `sourceQuestionId`/`documentId` win when both given.
   */
  question?: Pick<QuizQuestion, 'source_question_id' | 'document_id'>;
  /**
   * Pre-fetched original PYQ (GET /api/v1/questions/{id}). When omitted the
   * component lazily fetches it via `getQuestionById` on first toggle.
   */
  source?: SourceQuestion | null;
  /** Page fallbacks used until/unless the full source record is available. */
  startPage?: number | null;
  endPage?: number | null;
  pageNumber?: number | null;
  /**
   * Generated (possibly reworded) quiz question text. When provided and the
   * fetched original wording differs, a "Reworded from original" note is shown
   * alongside the original wording for easy comparison.
   */
  generatedWording?: string | null;
  className?: string;
};

function formatPageLabel(opts: {
  startPage?: number | null;
  endPage?: number | null;
  pageNumber?: number | null;
}): string | null {
  const { startPage, endPage, pageNumber } = opts;
  if (startPage != null && endPage != null && startPage !== endPage) {
    return `Pages ${startPage}-${endPage}`;
  }
  if (startPage != null) {
    return `Page ${startPage}`;
  }
  if (pageNumber != null) {
    return `Page ${pageNumber}`;
  }
  return null;
}

export function SourceTraceability({
  sourceQuestionId,
  documentId,
  question,
  source: sourceProp,
  startPage: startPageProp,
  endPage: endPageProp,
  pageNumber: pageNumberProp,
  generatedWording,
  className = '',
}: Props) {
  const resolvedSourceId = sourceQuestionId || question?.source_question_id || '';
  const resolvedDocumentId = documentId || question?.document_id || '';
  const [expanded, setExpanded] = useState(false);
  const [source, setSource] = useState<SourceQuestion | null>(sourceProp ?? null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Keep in sync when a pre-fetched source is supplied late (e.g. after fetch upstream).
  useEffect(() => {
    if (sourceProp) {
      setSource(sourceProp);
    }
  }, [sourceProp]);

  if (!resolvedSourceId || !resolvedDocumentId) {
    return null;
  }

  const startPage = source?.start_page ?? startPageProp ?? null;
  const endPage = source?.end_page ?? endPageProp ?? null;
  const pageNumber = source?.page_number ?? pageNumberProp ?? null;
  const pageLabel = formatPageLabel({ startPage, endPage, pageNumber });

  const originalWording = source?.question_text ?? null;
  const originalImages = source?.images ?? [];
  const normalize = (s: string) => s.trim().replace(/\s+/g, ' ');
  const isReworded =
    !!generatedWording &&
    !!originalWording &&
    normalize(generatedWording) !== normalize(originalWording);

  async function handleToggle() {
    const next = !expanded;
    setExpanded(next);
    if (next && !source && !loading) {
      setLoading(true);
      setError(null);
      try {
        const fetched = await getQuestionById(resolvedSourceId);
        setSource(fetched);
      } catch (e) {
        setError(e instanceof Error ? e.message : 'Failed to load original question');
      } finally {
        setLoading(false);
      }
    }
  }

  return (
    <div className={`rounded-lg border border-gray-100 bg-gray-50 px-4 py-3 ${className}`}>
      <p className="text-xs font-semibold uppercase tracking-wide text-gray-500">Source</p>
      <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-gray-700">
        <span>
          Question ID:{' '}
          <span className="font-mono text-xs text-gray-800">{resolvedSourceId}</span>
        </span>
        <span aria-hidden="true" className="text-gray-300">
          ·
        </span>
        <span>
          Document:{' '}
          <Link
            href={`/documents/${resolvedDocumentId}`}
            className="font-mono text-xs text-blue-600 hover:underline"
          >
            {resolvedDocumentId}
          </Link>
        </span>
        {pageLabel && (
          <>
            <span aria-hidden="true" className="text-gray-300">
              ·
            </span>
            <span className="rounded-full bg-gray-200 px-2 py-0.5 text-xs font-semibold text-gray-700">
              {pageLabel}
            </span>
          </>
        )}
      </div>

      <button
        type="button"
        onClick={handleToggle}
        aria-expanded={expanded}
        className="mt-2 text-sm font-medium text-blue-600 hover:underline"
      >
        {expanded ? 'Hide Original' : 'View Original'}
      </button>

      {expanded && (
        <div className="mt-2 rounded-lg border border-gray-200 bg-white px-4 py-3">
          {loading && <p className="text-sm text-gray-500">Loading original…</p>}
          {error && (
            <p role="alert" className="text-sm text-red-600">
              {error}
            </p>
          )}
          {!loading && !error && (
            <>
              {isReworded ? (
                <p className="mb-2 inline-block rounded-full bg-amber-100 px-2 py-0.5 text-[11px] font-bold uppercase tracking-wide text-amber-800">
                  Reworded from original
                </p>
              ) : (
                originalWording && (
                  <p className="mb-2 inline-block rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-bold uppercase tracking-wide text-gray-600">
                    Original wording
                  </p>
                )
              )}
              {originalWording ? (
                <p className="whitespace-pre-wrap text-sm leading-relaxed text-gray-800">
                  {originalWording}
                </p>
              ) : (
                <p className="text-sm text-gray-400">Original wording not available.</p>
              )}
              {originalImages.length > 0 && (
                <div className="mt-3 space-y-3">
                  {originalImages.map((name) => (
                    <div
                      key={name}
                      className="overflow-hidden rounded border border-gray-200 bg-gray-50"
                    >
                      <img
                        src={getDocumentImageUrl(resolvedDocumentId, name, true)}
                        alt={`Original image ${name}`}
                        className="max-h-80 w-full object-contain"
                        onError={(e) => {
                          const target = e.currentTarget as HTMLImageElement;
                          const fallback = getDocumentImageUrl(resolvedDocumentId, name, false);
                          if (target.src !== fallback) {
                            target.src = fallback;
                          }
                        }}
                        loading="lazy"
                      />
                      <div className="px-2 py-1 font-mono text-xs text-gray-500">{name}</div>
                    </div>
                  ))}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}

export default SourceTraceability;
