import React, { useState } from 'react'
import Link from 'next/link'
import { Button, EmptyState, ErrorState, LoadingState } from '../components/ui'
import { getStudyGuide, type StudyGuide } from '../lib/api'

const STEPS = ['Topic', 'Explanation', 'Example', 'PYQs', 'Quiz'] as const

export default function StudyPage() {
  const [topic, setTopic] = useState('')
  const [subject, setSubject] = useState('')
  const [weak, setWeak] = useState(false)
  const [guide, setGuide] = useState<StudyGuide | null>(null)
  const [step, setStep] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadGuide = async () => {
    if (!topic.trim()) {
      setError('Enter a topic to build the study guide.')
      return
    }
    setLoading(true)
    setError(null)
    try {
      const g = await getStudyGuide({
        topic: topic.trim(),
        subject: subject.trim() || undefined,
        weak,
      })
      setGuide(g)
      setStep(1)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load the study guide.')
    } finally {
      setLoading(false)
    }
  }

  const quizHref = guide
    ? `/quiz?topic=${encodeURIComponent(guide.topic)}${guide.subject ? `&subject=${encodeURIComponent(guide.subject)}` : ''}`
    : '/quiz'

  return (
    <div className="mx-auto max-w-3xl px-4 py-8">
      <h1 className="text-2xl font-bold">Study Mode</h1>
      <p className="mt-1 text-sm text-gray-600">Topic → Explanation → Example → PYQs → Quiz</p>

      {/* Step indicator */}
      <ol className="mt-4 flex flex-wrap gap-2 text-xs">
        {STEPS.map((label, i) => (
          <li
            key={label}
            className={`rounded-full px-3 py-1 ${i <= step ? 'bg-blue-600 text-white' : 'bg-gray-200 text-gray-700'}`}
          >
            {i + 1}. {label}
          </li>
        ))}
      </ol>

      {/* Step 0: pick a topic */}
      <div className="mt-6 flex flex-col gap-3 rounded border p-4">
        <label className="text-sm font-medium">
          Topic
          <input
            className="mt-1 w-full rounded border px-3 py-2"
            value={topic}
            onChange={(e) => setTopic(e.target.value)}
            placeholder="e.g. Deadlock"
          />
        </label>
        <label className="text-sm font-medium">
          Subject (optional)
          <input
            className="mt-1 w-full rounded border px-3 py-2"
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            placeholder="e.g. Operating Systems"
          />
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={weak} onChange={(e) => setWeak(e.target.checked)} />
          This is a weak topic for me (extra focus)
        </label>
        <div>
          <Button onClick={loadGuide} disabled={loading}>
            {loading ? 'Building guide…' : 'Build study guide'}
          </Button>
        </div>
      </div>

      {loading && <div className="mt-4"><LoadingState label="Building your study guide…" /></div>}
      {error && <div className="mt-4"><ErrorState message={error} onRetry={loadGuide} /></div>}

      {!loading && !error && !guide && (
        <div className="mt-4"><EmptyState message="Pick a topic above to start the Topic → Explanation → Example → PYQs → Quiz flow." /></div>
      )}

      {guide && !loading && (
        <div className="mt-6 flex flex-col gap-4">
          {/* Explanation */}
          <section className="rounded border p-4">
            <h2 className="font-semibold">Explanation{guide.subject ? ` — ${guide.topic} (${guide.subject})` : ` — ${guide.topic}`}</h2>
            {guide.weak_topic && (
              <p className="mt-1 inline-block rounded bg-amber-100 px-2 py-0.5 text-xs text-amber-800">Weak topic — focus here</p>
            )}
            <p className="mt-2 text-sm">{guide.explanation}</p>
            <ul className="mt-2 list-disc pl-5 text-sm">
              {guide.key_points.map((kp, i) => (
                <li key={i}>{kp}</li>
              ))}
            </ul>
            {guide.summary && <p className="mt-2 text-xs text-gray-600">{guide.summary}</p>}
          </section>

          {/* Example */}
          <section className="rounded border p-4">
            <h2 className="font-semibold">Worked example</h2>
            <p className="mt-2 text-sm">{guide.example}</p>
          </section>

          {/* PYQs */}
          <section className="rounded border p-4">
            <h2 className="font-semibold">Past questions ({guide.pyq_refs.length})</h2>
            {guide.pyq_refs.length === 0 ? (
              <p className="mt-2 text-sm text-gray-600">No PYQs indexed for this topic yet.</p>
            ) : (
              <ul className="mt-2 flex flex-col gap-2">
                {guide.pyq_refs.map((ref) => (
                  <li key={ref.question_id} className="rounded bg-gray-50 p-2 text-sm">
                    <span className="text-xs text-gray-500">
                      {ref.question_id}{ref.year ? ` · ${ref.year}` : ''}{ref.subject ? ` · ${ref.subject}` : ''}
                    </span>
                    <p>{ref.text}</p>
                  </li>
                ))}
              </ul>
            )}
          </section>

          {/* Quiz */}
          <section className="rounded border p-4">
            <h2 className="font-semibold">Practice quiz ({guide.next_quiz.questions.length} questions)</h2>
            {guide.next_quiz.questions.length === 0 ? (
              <p className="mt-2 text-sm text-gray-600">No quiz available until PYQs are indexed.</p>
            ) : (
              <ol className="mt-2 flex list-decimal flex-col gap-2 pl-5 text-sm">
                {guide.next_quiz.questions.map((q, i) => (
                  <li key={q.id || i}>
                    {q.question}
                    <ul className="mt-1 list-disc pl-5 text-gray-700">
                      {q.options.map((opt, j) => (
                        <li key={j} className={j === q.correct_answer ? 'font-medium text-green-700' : ''}>{opt}</li>
                      ))}
                    </ul>
                  </li>
                ))}
              </ol>
            )}
            <div className="mt-3">
              <Link href={quizHref} className="rounded bg-blue-600 px-4 py-2 text-sm text-white">
                Take the full quiz
              </Link>
            </div>
          </section>

          <div className="flex gap-2">
            {STEPS.map((label, i) => (
              <Button key={label} onClick={() => setStep(i)} disabled={i === step}>
                {label}
              </Button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
