import { create } from 'zustand'
import { api, ApiError, setUnauthorizedHandler } from '../services/api'
import type { User } from '../types/api'

interface AuthState {
  user: User | null
  /** true until the first /me check has finished */
  loading: boolean
  error: string | null
  check: () => Promise<void>
  login: (username: string, password: string) => Promise<boolean>
  logout: () => Promise<void>
  passwordChanged: () => void
  clearError: () => void
}

export function loginErrorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 429) {
      const s = e.retryAfter
      return s ? `Too many failed attempts. Try again in ${s > 90 ? Math.ceil(s / 60) + ' minutes' : s + ' seconds'}.` : e.message
    }
    return e.message
  }
  return 'Could not reach the server.'
}

export const useAuth = create<AuthState>((set, get) => ({
  user: null,
  loading: true,
  error: null,

  check: async () => {
    try {
      set({ user: await api.me(), loading: false })
    } catch (e) {
      // 403 password_change_required still means "logged in"; /me is always allowed so it won't occur here.
      set({ user: null, loading: false, error: e instanceof ApiError && e.status !== 401 ? e.message : null })
    }
  },

  login: async (username, password) => {
    set({ error: null })
    try {
      set({ user: await api.login(username, password) })
      return true
    } catch (e) {
      set({ error: loginErrorMessage(e) })
      return false
    }
  },

  logout: async () => {
    try {
      await api.logout()
    } catch {
      /* already logged out */
    }
    set({ user: null })
  },

  passwordChanged: () => {
    const u = get().user
    if (u) set({ user: { ...u, must_change_password: false } })
  },

  clearError: () => set({ error: null }),
}))

setUnauthorizedHandler(() => useAuth.setState({ user: null, loading: false }))

/** Where a user should be sent, given the path they asked for. null = stay. */
export function redirectFor(user: User | null, path: string): string | null {
  if (!user) return path === '/login' ? null : '/login'
  if (user.must_change_password) return path === '/change-password' ? null : '/change-password'
  if (path === '/login') return '/'
  if (path.startsWith('/admin') && user.role !== 'admin') return '/'
  return null
}
