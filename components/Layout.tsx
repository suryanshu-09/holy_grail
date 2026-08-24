import type { ReactNode } from 'react'
import { Navbar } from './Navbar'
import { Sidebar } from './Sidebar'

export function Layout({ children }: { children?: ReactNode }) {
  return (
    <div className="flex min-h-screen flex-col bg-gray-50">
      <Navbar />
      <div className="flex flex-1">
        <Sidebar />
        <main className="flex-1 p-6">{children}</main>
      </div>
    </div>
  )
}
