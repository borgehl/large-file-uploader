import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useAuth } from '../store/auth'
import { btnSecondary } from './ui'

const link = ({ isActive }: { isActive: boolean }) =>
  `rounded-md px-3 py-1.5 text-sm font-medium ${isActive ? 'bg-slate-200 dark:bg-slate-800' : 'hover:bg-slate-100 dark:hover:bg-slate-900'}`

export default function Layout() {
  const user = useAuth((s) => s.user)
  const logout = useAuth((s) => s.logout)
  const nav = useNavigate()
  const locked = !!user?.must_change_password

  return (
    <div className="min-h-screen">
      <header className="border-b border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-2 px-4 py-3">
          <span className="mr-4 text-base font-semibold">Large File Uploader</span>
          {user && !locked && (
            <nav className="flex flex-wrap gap-1" aria-label="Main">
              <NavLink to="/" end className={link}>Upload</NavLink>
              <NavLink to="/uploads" className={link}>My uploads</NavLink>
              {user.role === 'admin' && (
                <>
                  <NavLink to="/admin/users" className={link}>Users</NavLink>
                  <NavLink to="/admin/uploads" className={link}>All uploads</NavLink>
                </>
              )}
            </nav>
          )}
          <div className="ml-auto flex items-center gap-2 text-sm">
            {user && (
              <>
                <span className="text-slate-600 dark:text-slate-400">{user.username}</span>
                {!locked && <NavLink to="/change-password" className={link}>Password</NavLink>}
                <button
                  className={btnSecondary}
                  onClick={async () => {
                    await logout()
                    nav('/login')
                  }}
                >
                  Log out
                </button>
              </>
            )}
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}
