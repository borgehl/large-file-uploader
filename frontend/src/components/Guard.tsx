import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { redirectFor, useAuth } from '../store/auth'

/** Renders the child routes only when the session state allows this path. */
export default function Guard() {
  const { user, loading } = useAuth()
  const { pathname } = useLocation()
  if (loading) return <p className="p-8 text-center text-slate-500">Loading…</p>
  const to = redirectFor(user, pathname)
  if (to) return <Navigate to={to} replace />
  return <Outlet />
}
