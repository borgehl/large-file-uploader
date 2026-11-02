import type { QueueItem } from '../store/uploads'
import { formatBytes, formatDuration, formatRate, percent } from '../lib/format'
import { btnDanger, btnSecondary, card, ProgressBar } from './ui'

interface Props {
  item: QueueItem
  onPause: () => void
  onResume: () => void
  onCancel: () => void
  onRemove: () => void
}

function statusText(item: QueueItem): string {
  const s = item.snap
  switch (s.status) {
    case 'queued': return 'Waiting for the current upload to finish'
    case 'uploading': return 'Uploading'
    case 'paused': return 'Paused'
    case 'finishing': return s.digest ? 'Finishing…' : 'All data sent; computing the file checksum…'
    case 'verifying': return 'Server is verifying the checksum'
    case 'complete': return 'Complete: the server verified the checksum'
    case 'cancelled': return 'Cancelled'
    case 'failed': return 'Failed'
  }
}

export default function UploadCard({ item, onPause, onResume, onCancel, onRemove }: Props) {
  const s = item.snap
  const sendPct = percent(s.uploadedBytes, s.totalBytes)
  const hashPct = percent(s.hashedBytes, s.totalBytes)
  const verifyPct = percent(s.verifiedBytes, s.totalBytes)
  const eta = s.bytesPerSec > 0 ? (s.totalBytes - s.uploadedBytes) / s.bytesPerSec : NaN
  const active = s.status === 'uploading' || s.status === 'finishing' || s.status === 'verifying'
  const showHash = !s.digest && s.hashedBytes < s.totalBytes && s.status !== 'failed' && s.status !== 'cancelled'

  return (
    <li className={`${card} space-y-3`}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="min-w-0 break-all font-medium">{item.name}</p>
        <p className="text-sm text-slate-600 dark:text-slate-400">{formatBytes(s.totalBytes)}</p>
      </div>

      {s.status !== 'failed' && s.status !== 'cancelled' && (
        <div className="space-y-1">
          <ProgressBar
            label={`Upload progress for ${item.name}`}
            value={s.status === 'complete' ? 100 : s.status === 'verifying' ? verifyPct : sendPct}
            busy={s.status === 'uploading' || s.status === 'verifying'}
          />
          <p className="flex flex-wrap justify-between gap-x-4 text-xs text-slate-600 dark:text-slate-400">
            <span>{statusText(item)}</span>
            {s.status === 'uploading' && (
              <span>
                {formatBytes(s.uploadedBytes)} of {formatBytes(s.totalBytes)} ({sendPct.toFixed(1)}%)
                {s.bytesPerSec > 0 && <> · {formatRate(s.bytesPerSec)} · {formatDuration(eta)} left</>}
              </span>
            )}
            {s.status === 'verifying' && <span>{formatBytes(s.verifiedBytes)} verified ({verifyPct.toFixed(1)}%)</span>}
          </p>
          {showHash && (
            <p className="text-xs text-slate-500">Computing checksum in the background: {hashPct.toFixed(0)}%</p>
          )}
        </div>
      )}

      {s.error && (
        <p role={s.status === 'failed' ? 'alert' : 'status'} className={`text-sm ${s.status === 'failed' ? 'text-red-700 dark:text-red-400' : 'text-amber-700 dark:text-amber-400'}`}>
          {s.error}
        </p>
      )}
      {s.status === 'complete' && s.digest && (
        <p className="break-all font-mono text-xs text-slate-500">SHA-256 {s.digest}</p>
      )}

      <div className="flex flex-wrap gap-2">
        {s.status === 'uploading' && <button className={btnSecondary} onClick={onPause}>Pause</button>}
        {s.status === 'paused' && <button className={btnSecondary} onClick={onResume}>Resume</button>}
        {(s.status === 'uploading' || s.status === 'paused' || s.status === 'queued') && (
          <button className={btnDanger} onClick={onCancel}>Cancel</button>
        )}
        {!active && (s.status === 'complete' || s.status === 'failed' || s.status === 'cancelled') && (
          <button className={btnSecondary} onClick={onRemove}>Dismiss</button>
        )}
      </div>
    </li>
  )
}
