import { useEffect, useState, type FormEvent } from 'react'
import { api, ApiError } from '../services/api'
import type { User } from '../types/api'
import { btnDanger, btnPrimary, btnSecondary, card, ErrorNote, Field, input } from '../components/ui'
import { formatDate } from '../lib/format'
import { useAuth } from '../store/auth'

export default function AdminUsersPage() {
  const me = useAuth((s) => s.user)
  const [users, setUsers] = useState<User[]>([])
  const [error, setError] = useState<string | null>(null)
  const [secret, setSecret] = useState<{ username: string; password: string } | null>(null)
  const [username, setUsername] = useState('')
  const [role, setRole] = useState<'user' | 'admin'>('user')
  const [password, setPassword] = useState('')

  const [tick, setTick] = useState(0)

  useEffect(() => {
    let cancelled = false
    api.listUsers().then(
      (u) => {
        if (!cancelled) setUsers(u)
      },
      (e) => {
        if (!cancelled) setError(e instanceof ApiError ? e.message : 'Could not load users.')
      },
    )
    return () => {
      cancelled = true
    }
  }, [tick])

  async function run<T>(fn: () => Promise<T>): Promise<T | undefined> {
    setError(null)
    try {
      const r = await fn()
      setTick((n) => n + 1)
      return r
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Request failed.')
    }
  }

  async function create(e: FormEvent) {
    e.preventDefault()
    const r = await run(() => api.createUser(username.trim(), role, password || undefined))
    if (r) {
      setSecret(r.temp_password ? { username: r.user.username, password: r.temp_password } : null)
      setUsername('')
      setPassword('')
    }
  }

  return (
    <div className="space-y-6">
      <h1 className="text-xl font-semibold">Users</h1>
      <ErrorNote>{error}</ErrorNote>

      {secret && (
        <div role="status" className="space-y-2 rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm dark:border-amber-800 dark:bg-amber-950">
          <p>
            Temporary password for <strong>{secret.username}</strong>. It is shown only once; they must change it at first login.
          </p>
          <p className="select-all break-all rounded bg-white px-2 py-1 font-mono dark:bg-slate-900">{secret.password}</p>
          <button className={btnSecondary} onClick={() => setSecret(null)}>I have copied it</button>
        </div>
      )}

      <form onSubmit={create} className={`${card} grid gap-4 sm:grid-cols-4`}>
        <Field label="Username">
          <input className={input} value={username} onChange={(e) => setUsername(e.target.value)} pattern="[A-Za-z0-9._\-]{3,40}" title="3-40 letters, digits, . _ -" required />
        </Field>
        <Field label="Role">
          <select className={input} value={role} onChange={(e) => setRole(e.target.value as 'user' | 'admin')}>
            <option value="user">User</option>
            <option value="admin">Admin</option>
          </select>
        </Field>
        <Field label="Temporary password (blank = generate)">
          <input className={input} type="text" value={password} onChange={(e) => setPassword(e.target.value)} minLength={12} autoComplete="off" />
        </Field>
        <div className="flex items-end">
          <button className={`${btnPrimary} w-full`}>Create user</button>
        </div>
      </form>

      <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-slate-200 text-xs uppercase text-slate-500 dark:border-slate-800">
            <tr>
              <th className="px-3 py-2">Username</th>
              <th className="px-3 py-2">Role</th>
              <th className="px-3 py-2">Status</th>
              <th className="px-3 py-2">Last login</th>
              <th className="px-3 py-2"><span className="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
            {users.map((u) => {
              const locked = u.is_root || u.id === me?.id
              return (
                <tr key={u.id}>
                  <td className="px-3 py-2 font-medium">{u.username}{u.is_root && <span className="ml-2 text-xs text-slate-500">root</span>}</td>
                  <td className="px-3 py-2">{u.role}</td>
                  <td className="px-3 py-2">
                    {u.disabled ? 'disabled' : u.must_change_password ? 'must change password' : 'active'}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2">{formatDate(u.last_login_at)}</td>
                  <td className="px-3 py-2">
                    {!locked && (
                      <div className="flex flex-wrap justify-end gap-2">
                        <button className={btnSecondary} onClick={() => run(() => api.patchUser(u.id, { disabled: !u.disabled }))}>
                          {u.disabled ? 'Enable' : 'Disable'}
                        </button>
                        <button
                          className={btnSecondary}
                          onClick={async () => {
                            const r = await run(() => api.patchUser(u.id, { reset_password: true }))
                            if (r?.temp_password) setSecret({ username: u.username, password: r.temp_password })
                          }}
                        >
                          Reset password
                        </button>
                        <button
                          className={btnDanger}
                          onClick={() => {
                            if (window.confirm(`Delete user "${u.username}"? Their uploaded files stay on disk.`)) void run(() => api.deleteUser(u.id))
                          }}
                        >
                          Delete
                        </button>
                      </div>
                    )}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
