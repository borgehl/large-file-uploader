import type { ReactNode } from 'react'

export const btn =
  'inline-flex items-center justify-center rounded-md px-3 py-1.5 text-sm font-medium transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600 disabled:cursor-not-allowed disabled:opacity-50'
export const btnPrimary = `${btn} bg-blue-600 text-white hover:bg-blue-700`
export const btnSecondary = `${btn} border border-slate-300 bg-white text-slate-800 hover:bg-slate-100 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100 dark:hover:bg-slate-800`
export const btnDanger = `${btn} border border-red-300 bg-white text-red-700 hover:bg-red-50 dark:border-red-900 dark:bg-slate-900 dark:text-red-400 dark:hover:bg-red-950`
export const input =
  'block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm shadow-sm focus:border-blue-600 focus:outline-none focus:ring-1 focus:ring-blue-600 dark:border-slate-700 dark:bg-slate-900'
export const card = 'rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-slate-800 dark:bg-slate-900'

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block space-y-1">
      <span className="text-sm font-medium text-slate-700 dark:text-slate-300">{label}</span>
      {children}
    </label>
  )
}

export function ErrorNote({ children }: { children: ReactNode }) {
  if (!children) return null
  return (
    <p role="alert" className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200">
      {children}
    </p>
  )
}

const BADGE: Record<string, string> = {
  complete: 'bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300',
  failed_hash: 'bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300',
  failed: 'bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300',
  verifying: 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300',
  received: 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300',
  created: 'bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300',
}

const LABEL: Record<string, string> = {
  created: 'uploading',
  received: 'received',
  verifying: 'verifying',
  complete: 'complete',
  failed_hash: 'checksum failed',
  aborted: 'cancelled',
  expired: 'expired',
}

export function StateBadge({ state }: { state: string }) {
  return (
    <span className={`inline-block rounded-full px-2 py-0.5 text-xs font-medium ${BADGE[state] ?? 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'}`}>
      {LABEL[state] ?? state}
    </span>
  )
}

export function ProgressBar({ value, busy = false, label }: { value: number; busy?: boolean; label: string }) {
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(value)}
      className="h-2.5 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-slate-800"
    >
      <div
        className={`h-full rounded-full bg-blue-600 transition-[width] ${busy ? 'progress-busy' : ''}`}
        style={{ width: `${value}%` }}
      />
    </div>
  )
}
