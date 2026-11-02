import { useEffect, useState } from 'react'
import { api, ApiError } from '../services/api'
import type { ServerUpload } from '../types/api'
import { btnDanger, ErrorNote, StateBadge } from '../components/ui'
import { formatBytes, formatDate } from '../lib/format'
import { useAuth } from '../store/auth'

export default function UploadsPage({ all = false }: { all?: boolean }) {
  const isAdmin = useAuth((s) => s.user?.role === 'admin')
  const [rows, setRows] = useState<ServerUpload[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [tick, setTick] = useState(0)

  useEffect(() => {
    let cancelled = false
    const run = async () => {
      try {
        const r = await api.listUploads(all)
        if (!cancelled) {
          setRows(r)
          setError(null)
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof ApiError ? e.message : 'Could not load uploads.')
      }
    }
    void run()
    const t = setInterval(() => void run(), 5000)
    return () => {
      cancelled = true
      clearInterval(t)
    }
  }, [all, tick])

  async function discard(u: ServerUpload) {
    if (!window.confirm(`Permanently delete the quarantined data for "${u.original_name}"?`)) return
    try {
      await api.deleteUpload(u.id)
      setTick((n) => n + 1)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not delete.')
    }
  }

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">{all ? 'All uploads' : 'My uploads'}</h1>
      <ErrorNote>{error}</ErrorNote>
      {rows === null && !error && <p className="text-slate-500">Loading…</p>}
      {rows?.length === 0 && <p className="text-slate-500">No uploads yet.</p>}
      {rows && rows.length > 0 && (
        <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-slate-200 text-xs uppercase text-slate-500 dark:border-slate-800">
              <tr>
                <th className="px-3 py-2">File</th>
                {all && <th className="px-3 py-2">User</th>}
                <th className="px-3 py-2">Size</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2">Started</th>
                <th className="px-3 py-2">SHA-256</th>
                {all && isAdmin && <th className="px-3 py-2"><span className="sr-only">Actions</span></th>}
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
              {rows.map((u) => (
                <tr key={u.id} className="align-top">
                  <td className="max-w-xs break-all px-3 py-2">
                    {u.original_name}
                    {u.error && u.state !== 'complete' && <div className="text-xs text-red-700 dark:text-red-400">{u.error}</div>}
                    {u.stored_path && <div className="font-mono text-xs text-slate-500">{u.stored_path}</div>}
                  </td>
                  {all && <td className="px-3 py-2">{u.owner_name}</td>}
                  <td className="whitespace-nowrap px-3 py-2">
                    {u.state === 'created' ? `${formatBytes(u.offset)} / ` : ''}{formatBytes(u.size_bytes)}
                  </td>
                  <td className="px-3 py-2"><StateBadge state={u.state} /></td>
                  <td className="whitespace-nowrap px-3 py-2">{formatDate(u.created_at)}</td>
                  <td className="px-3 py-2 font-mono text-xs" title={u.server_sha256 || u.client_sha256}>
                    {(u.server_sha256 || u.client_sha256).slice(0, 12) || '–'}
                  </td>
                  {all && isAdmin && (
                    <td className="px-3 py-2 text-right">
                      {u.state === 'failed_hash' && <button className={btnDanger} onClick={() => discard(u)}>Delete data</button>}
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
