import Link from 'next/link'
import { useRouter } from 'next/router'

const links = [
  { href: '/', label: 'Dashboard' },
  { href: '/documents', label: 'Documents' },
  { href: '/topics', label: 'Topics' },
  { href: '/quiz', label: 'Quiz' },
]

export function Sidebar() {
  const router = useRouter()
  const pathname = router.pathname

  return (
    <aside className="w-48 shrink-0 border-r border-gray-200 bg-white">
      <nav className="flex flex-col gap-1 p-3">
        {links.map((link) => {
          const isActive =
            link.href === '/' ? pathname === '/' : pathname?.startsWith(link.href)
          return (
            <Link
              key={link.href}
              href={link.href}
              className={`rounded px-3 py-2 text-sm ${
                isActive
                  ? 'bg-blue-50 font-semibold text-blue-700'
                  : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900'
              }`}
            >
              {link.label}
            </Link>
          )
        })}
      </nav>
    </aside>
  )
}
