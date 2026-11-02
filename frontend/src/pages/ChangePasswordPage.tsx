import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, ApiError } from '../services/api'
import { useAuth } from '../store/auth'
import { btnPrimary, card, ErrorNote, Field, input } from '../components/ui'

export default function ChangePasswordPage() {
  const user = useAuth((s) => s.user)
  const passwordChanged = useAuth((s) => s.passwordChanged)
  const nav = useNavigate()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState(false)
  const forced = !!user?.must_change_password

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    if (next !== confirm) return setError('The new passwords do not match.')
    if (next.length < 12) return setError('The new password must be at least 12 characters.')
    try {
      await api.changePassword(current, next)
      passwordChanged()
      setDone(true)
      if (forced) nav('/', { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not change the password.')
    }
  }

  return (
    <form onSubmit={submit} className={`${card} mx-auto max-w-sm space-y-4`}>
      <h1 className="text-xl font-semibold">{forced ? 'Choose a new password' : 'Change password'}</h1>
      {forced && <p className="text-sm text-slate-600 dark:text-slate-400">You must set your own password before continuing.</p>}
      <ErrorNote>{error}</ErrorNote>
      {done && !forced && <p role="status" className="text-sm text-green-700 dark:text-green-400">Password changed. Your other sessions were signed out.</p>}
      <Field label="Current password">
        <input className={input} type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" required />
      </Field>
      <Field label="New password (12+ characters)">
        <input className={input} type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" minLength={12} required />
      </Field>
      <Field label="Repeat new password">
        <input className={input} type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="new-password" required />
      </Field>
      <button className={`${btnPrimary} w-full`}>Change password</button>
    </form>
  )
}
