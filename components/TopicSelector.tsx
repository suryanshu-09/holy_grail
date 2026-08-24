import React from 'react'

export interface Topic {
  id: string
  name: string
  subject?: string | null
}

type Props = {
  topics: Topic[]
  onChange: (topicId: string) => void
  value?: string
  placeholder?: string
} & Omit<React.SelectHTMLAttributes<HTMLSelectElement>, 'onChange'>

export function TopicSelector({ topics, onChange, value = '', placeholder = 'Select a topic', ...props }: Props) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="rounded border border-gray-300 bg-white px-3 py-2 text-sm text-gray-700 focus:border-blue-500 focus:outline-none"
      {...props}
    >
      <option value="">{placeholder}</option>
      {topics.map((topic) => (
        <option key={topic.id} value={topic.id}>
          {topic.name}
        </option>
      ))}
    </select>
  )
}
