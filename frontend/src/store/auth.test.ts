import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, request, setUnauthorizedHandler } from '../services/api'
import { loginErrorMessage, redirectFor } from './auth'
import type { User } from '../types/api'

const user = (over: Partial<User> = {}): User => ({
  id: 1, username: 'a', role: 'user', is_root: false, disabled: false,
  must_change_password: false, created_at: 0, last_login_at: null, ...over,
})

describe('redirectFor (route guard)', () => {
  it('sends anonymous visitors to login', () => {
    expect(redirectFor(null, '/')).toBe('/login')
    expect(redirectFor(null, '/admin/users')).toBe('/login')
    expect(redirectFor(null, '/login')).toBeNull()
  })
  it('confines users who must change their password', () => {
    const u = user({ must_change_password: true })
    expect(redirectFor(u, '/')).toBe('/change-password')
    expect(redirectFor(u, '/uploads')).toBe('/change-password')
    expect(redirectFor(u, '/change-password')).toBeNull()
  })
  it('keeps non-admins out of admin routes and logged-in users off the login page', () => {
    expect(redirectFor(user(), '/admin/users')).toBe('/')
    expect(redirectFor(user({ role: 'admin' }), '/admin/users')).toBeNull()
    expect(redirectFor(user(), '/login')).toBe('/')
  })
})

describe('request()', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    setUnauthorizedHandler(() => {})
  })

  it('calls the unauthorized handler on 401 outside login', async () => {
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'authentication required' }), { status: 401 })))
    await expect(request('GET', '/api/uploads')).rejects.toMatchObject({ status: 401, message: 'authentication required' })
    expect(handler).toHaveBeenCalledOnce()
  })

  it('does not treat a failed login as an expired session', async () => {
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'invalid username or password' }), { status: 401 })))
    await expect(request('POST', '/api/auth/login', {})).rejects.toBeInstanceOf(ApiError)
    expect(handler).not.toHaveBeenCalled()
  })

  it('carries Retry-After for rate limiting', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'too many' }), { status: 429, headers: { 'Retry-After': '30' } })))
    const err = (await request('POST', '/api/auth/login', {}).catch((e) => e)) as ApiError
    expect(err.retryAfter).toBe(30)
    expect(loginErrorMessage(err)).toMatch(/30 seconds/)
  })
})
