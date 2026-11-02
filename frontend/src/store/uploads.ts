import { create } from 'zustand'
import { api } from '../services/api'
import { UploadJob, type JobSnapshot } from '../lib/uploadJob'
import type { AppConfig } from '../types/api'
import { checkFileSize } from '../lib/format'

export interface QueueItem {
  key: string
  name: string
  snap: JobSnapshot
  /** set when the file was rejected before upload started */
  rejected?: string
}

interface UploadsState {
  config: AppConfig | null
  configError: string | null
  items: QueueItem[]
  loadConfig: () => Promise<void>
  add: (files: File[]) => void
  pause: (key: string) => void
  resume: (key: string) => void
  cancel: (key: string) => void
  remove: (key: string) => void
}

const jobs = new Map<string, UploadJob>()
let seq = 0

const ACTIVE = new Set(['uploading', 'finishing', 'verifying'])

export const useUploads = create<UploadsState>((set, get) => {
  const update = (key: string, snap: JobSnapshot) => {
    set((s) => ({ items: s.items.map((i) => (i.key === key ? { ...i, snap } : i)) }))
    pump()
  }

  // One transfer at a time. "finishing"/"verifying" are server-side waits and do
  // not block the next file from starting.
  function pump() {
    const { items } = get()
    const sending = items.some((i) => i.snap.status === 'uploading')
    if (sending) return
    const next = items.find((i) => i.snap.status === 'queued' && !i.rejected)
    if (next) void jobs.get(next.key)?.start()
  }

  return {
    config: null,
    configError: null,
    items: [],

    loadConfig: async () => {
      try {
        set({ config: await api.config(), configError: null })
      } catch (e) {
        set({ configError: e instanceof Error ? e.message : 'could not load configuration' })
      }
    },

    add: (files) => {
      const cfg = get().config
      if (!cfg) return
      const added: QueueItem[] = []
      for (const file of files) {
        const key = `u${++seq}`
        const problem = checkFileSize(file.size, cfg.max_upload_bytes)
        const job = new UploadJob({ file, chunkSize: cfg.chunk_size_bytes, onChange: (s) => update(key, s) })
        if (!problem) jobs.set(key, job)
        added.push({
          key, name: file.name, rejected: problem ?? undefined,
          snap: problem ? { ...job.snapshot, status: 'failed', error: problem } : job.snapshot,
        })
      }
      set((s) => ({ items: [...s.items, ...added] }))
      pump()
    },

    pause: (key) => jobs.get(key)?.pause(),
    resume: (key) => {
      void jobs.get(key)?.resume()
    },
    cancel: (key) => {
      void jobs.get(key)?.cancel()
    },
    remove: (key) => {
      const j = jobs.get(key)
      const item = get().items.find((i) => i.key === key)
      if (item && ACTIVE.has(item.snap.status)) return
      j?.dispose()
      jobs.delete(key)
      set((s) => ({ items: s.items.filter((i) => i.key !== key) }))
    },
  }
})
