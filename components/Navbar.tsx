import Link from 'next/link'

const links = [
  { href: '/', label: 'Dashboard' },
  { href: '/documents', label: 'Documents' },
  { href: '/topics', label: 'Topics' },
  { href: '/quiz', label: 'Quiz' },
]

export function Navbar() {
  return (
    <header className="flex items-center justify-between border-b border-gray-200 bg-white px-6 py-3">
      <Link href="/" className="text-lg font-bold text-blue-600">
        Holy Grail
      </Link>
      <nav className="hidden md:flex items-center gap-6">
        {links.map((link) => (
          <Link
            key={link.href}
            href={link.href}
            className="text-sm text-gray-600 hover:text-blue-600"
          >
            {link.label}
          </Link>
        ))}
      </nav>
    </header>
  )
}
