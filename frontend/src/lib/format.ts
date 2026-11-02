const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB']

export function formatBytes(n: number, digits = 1): string {
  if (!Number.isFinite(n) || n < 0) return '–'
  let v = n
  let i = 0
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024
    i++
  }
  return `${i === 0 ? v : v.toFixed(digits)} ${UNITS[i]}`
}

export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '–'
  const s = Math.round(seconds)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  if (h > 0) return `${h}h ${String(m).padStart(2, '0')}m`
  if (m > 0) return `${m}m ${String(sec).padStart(2, '0')}s`
  return `${sec}s`
}

export function formatRate(bytesPerSec: number): string {
  return `${formatBytes(bytesPerSec)}/s`
}

export function formatDate(unix: number | null | undefined): string {
  if (!unix) return '–'
  return new Date(unix * 1000).toLocaleString()
}

export function percent(done: number, total: number): number {
  if (total <= 0) return 0
  return Math.max(0, Math.min(100, (done / total) * 100))
}

/** Client-side mirror of the server's size rule (strictly below the limit, and non-empty). */
export function checkFileSize(size: number, maxBytes: number): string | null {
  if (size <= 0) return 'This file is empty.'
  if (size >= maxBytes) return `This file is ${formatBytes(size)}; files must be smaller than ${formatBytes(maxBytes)}.`
  return null
}
