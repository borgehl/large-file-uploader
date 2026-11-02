import { useState, type FormEvent } from 'react'
import { useAuth } from '../store/auth'
import { btnPrimary, card, ErrorNote, Field, input } from '../components/ui'

export default function LoginPage() {
  const { login, error } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    await login(username.trim(), password)
    setBusy(false)
  }

  return (
    <form onSubmit={submit} className={`${card} mx-auto max-w-sm space-y-4`}>
      <h1 className="text-xl font-semibold">Log in</h1>
      <ErrorNote>{error}</ErrorNote>
      <Field label="Username">
        <input className={input} value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" autoFocus required />
      </Field>
      <Field label="Password">
        <input className={input} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
      </Field>
      <button className={`${btnPrimary} w-full`} disabled={busy}>
        {busy ? 'Logging in…' : 'Log in'}
      </button>
    </form>
  )
}
