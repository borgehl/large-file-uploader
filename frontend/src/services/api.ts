import type { AppConfig, ServerUpload, User } from '../types/api'

export class ApiError extends Error {
  status: number
  retryAfter?: number
  constructor(status: number, message: string, retryAfter?: number) {
    super(message)
    this.status = status
    this.retryAfter = retryAfter
  }
}

type UnauthorizedHandler = () => void
let onUnauthorized: UnauthorizedHandler = () => {}

/** Called whenever an API request comes back 401 (session expired or revoked). */
export function setUnauthorizedHandler(fn: UnauthorizedHandler) {
  onUnauthorized = fn
}

export async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) {
    let msg = res.statusText || `HTTP ${res.status}`
    try {
      const j = await res.json()
      if (j && typeof j.error === 'string') msg = j.error
    } catch {
      /* non-JSON error body */
    }
    if (res.status === 401 && path !== '/api/auth/login') onUnauthorized()
    const ra = Number(res.headers.get('Retry-After'))
    throw new ApiError(res.status, msg, Number.isFinite(ra) && ra > 0 ? ra : undefined)
  }
  return (await res.json()) as T
}

export const api = {
  login: (username: string, password: string) => request<User>('POST', '/api/auth/login', { username, password }),
  logout: () => request<{ ok: boolean }>('POST', '/api/auth/logout'),
  me: () => request<User>('GET', '/api/auth/me'),
  changePassword: (current_password: string, new_password: string) =>
    request<{ ok: boolean }>('POST', '/api/auth/password', { current_password, new_password }),
  config: () => request<AppConfig>('GET', '/api/config'),

  listUploads: (all = false) => request<ServerUpload[]>('GET', `/api/uploads${all ? '?all=1' : ''}`),
  getUpload: (id: string) => request<ServerUpload>('GET', `/api/uploads/${encodeURIComponent(id)}`),
  submitDigest: (id: string, sha256: string) =>
    request<ServerUpload>('POST', `/api/uploads/${encodeURIComponent(id)}/sha256`, { sha256 }),
  deleteUpload: (id: string) => request<{ ok: boolean }>('DELETE', `/api/uploads/${encodeURIComponent(id)}`),

  listUsers: () => request<User[]>('GET', '/api/admin/users'),
  createUser: (username: string, role: 'user' | 'admin', password?: string) =>
    request<{ user: User; temp_password?: string }>('POST', '/api/admin/users', { username, role, password: password ?? '' }),
  patchUser: (id: number, patch: { disabled?: boolean; reset_password?: boolean }) =>
    request<{ user: User; temp_password?: string }>('PATCH', `/api/admin/users/${id}`, patch),
  deleteUser: (id: number) => request<{ ok: boolean }>('DELETE', `/api/admin/users/${id}`),
}
