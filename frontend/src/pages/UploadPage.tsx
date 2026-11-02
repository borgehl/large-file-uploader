import { useEffect, useRef, useState, type DragEvent } from 'react'
import UploadCard from '../components/UploadCard'
import { btnPrimary, ErrorNote } from '../components/ui'
import { formatBytes } from '../lib/format'
import { WakeLockController } from '../lib/wakeLock'
import { useUploads } from '../store/uploads'

export default function UploadPage() {
  const { config, configError, items, loadConfig, add, pause, resume, cancel, remove } = useUploads()
  const [dragging, setDragging] = useState(false)
  const picker = useRef<HTMLInputElement>(null)

  useEffect(() => {
    void loadConfig()
  }, [loadConfig])

  const hasActive = items.some((i) => ['uploading', 'paused', 'finishing', 'verifying', 'queued'].includes(i.snap.status))
  useEffect(() => {
    if (!hasActive) return
    const warn = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [hasActive])

  // Keep the machine awake only while bytes are actually moving (hashing/sending). Paused
  // uploads and server-side verification do not need it. The queue outlives this page, so
  // the lock is released on unmount and re-taken if the user comes back mid-transfer.
  const transferring = items.some((i) => i.snap.status === 'uploading')
  useEffect(() => {
    if (!transferring) return
    const wake = new WakeLockController()
    wake.setWanted(true)
    return () => wake.dispose()
  }, [transferring])

  function onDrop(e: DragEvent) {
    e.preventDefault()
    setDragging(false)
    add(Array.from(e.dataTransfer.files))
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">Upload files</h1>
        {config && (
          <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">
            Files must be smaller than {formatBytes(config.max_upload_bytes)}. Large uploads can take a while; you can pause and resume them.
          </p>
        )}
      </div>
      <ErrorNote>{configError}</ErrorNote>

      <div
        onDragOver={(e) => {
          e.preventDefault()
          setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
        className={`flex flex-col items-center gap-3 rounded-lg border-2 border-dashed p-10 text-center ${dragging ? 'border-blue-600 bg-blue-50 dark:bg-blue-950' : 'border-slate-300 dark:border-slate-700'}`}
      >
        <p className="text-slate-700 dark:text-slate-300">Drag files here, or</p>
        <button className={btnPrimary} disabled={!config} onClick={() => picker.current?.click()}>
          Choose files
        </button>
        <input
          ref={picker}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            add(Array.from(e.target.files ?? []))
            e.target.value = ''
          }}
        />
      </div>

      <p className="text-sm text-slate-600 dark:text-slate-400">
        If the page is reloaded or the connection drops mid-upload, choose the same file again and the upload continues from where it stopped.
      </p>

      {items.length > 0 && (
        <ul className="space-y-3" aria-label="Uploads">
          {items.map((i) => (
            <UploadCard
              key={i.key}
              item={i}
              onPause={() => pause(i.key)}
              onResume={() => resume(i.key)}
              onCancel={() => cancel(i.key)}
              onRemove={() => remove(i.key)}
            />
          ))}
        </ul>
      )}
    </div>
  )
}
