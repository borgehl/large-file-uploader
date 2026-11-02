import { defaultOptions, Upload, type DetailedError } from 'tus-js-client'
import { api, ApiError } from '../services/api'
import type { ServerUpload, UploadState } from '../types/api'
import { chunkHashingStack } from './chunkHttpStack'
import { hashFile, type HashHandle } from './hashFile'

export type JobStatus =
  | 'queued'
  | 'uploading'
  | 'paused'
  | 'finishing' // all bytes sent, waiting for the digest/server verification
  | 'verifying'
  | 'complete'
  | 'failed'
  | 'cancelled'

export interface JobSnapshot {
  status: JobStatus
  uploadedBytes: number
  totalBytes: number
  hashedBytes: number
  verifiedBytes: number
  bytesPerSec: number
  serverId: string | null
  serverState: UploadState | null
  error: string | null
  digest: string | null
}

export interface JobOptions {
  file: File
  chunkSize: number
  onChange: (s: JobSnapshot) => void
  pollMs?: number
}

const RETRY_DELAYS = [0, 1000, 3000, 10_000, 30_000, 60_000]
const POLL_MS = 2000

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms))
}

/**
 * One file's lifecycle: hash in a worker while uploading with tus (every chunk
 * carries its own SHA-256), submit the whole-file digest, then wait for the
 * server to verify and finalize it.
 */
export class UploadJob {
  private snap: JobSnapshot
  private tus: Upload | null = null
  private hash: HashHandle | null = null
  private digestSent = false
  private lastSampleAt = 0
  private lastSampleBytes = 0
  private disposed = false
  private fileSize: number
  private fileMtime: number
  private opts: JobOptions

  constructor(opts: JobOptions) {
    this.opts = opts
    this.fileSize = opts.file.size
    this.fileMtime = opts.file.lastModified
    this.snap = {
      status: 'queued', uploadedBytes: 0, totalBytes: opts.file.size, hashedBytes: 0, verifiedBytes: 0,
      bytesPerSec: 0, serverId: null, serverState: null, error: null, digest: null,
    }
  }

  get snapshot() { return this.snap }
  get file() { return this.opts.file }

  private set(patch: Partial<JobSnapshot>) {
    this.snap = { ...this.snap, ...patch }
    if (!this.disposed) this.opts.onChange(this.snap)
  }

  private fail(message: string) {
    this.hash?.cancel()
    this.set({ status: 'failed', error: message, bytesPerSec: 0 })
  }

  async start() {
    if (this.snap.status === 'complete' || this.snap.status === 'cancelled') return
    this.set({ status: 'uploading', error: null })
    this.beginHashing()
    await this.beginTus()
  }

  private beginHashing() {
    if (this.hash || this.snap.digest) return
    this.hash = hashFile(this.opts.file, (hashed) => this.set({ hashedBytes: hashed }))
    this.hash.promise.then(
      (r) => {
        if (r.size !== this.fileSize || r.lastModified !== this.fileMtime) {
          this.fail('The file changed on disk while it was being uploaded. Start the upload again.')
          return
        }
        this.set({ digest: r.digest, hashedBytes: r.size })
        void this.sendDigest()
      },
      (e: Error) => {
        if (e.name !== 'AbortError') this.fail(`Could not read the file to compute its checksum: ${e.message}`)
      },
    )
  }

  private async beginTus() {
    const base = defaultOptions.httpStack
    const upload = new Upload(this.opts.file, {
      endpoint: '/files/',
      chunkSize: this.opts.chunkSize,
      retryDelays: RETRY_DELAYS,
      metadata: { filename: this.opts.file.name, filetype: this.opts.file.type || 'application/octet-stream' },
      httpStack: chunkHashingStack(base),
      removeFingerprintOnSuccess: true,
      onShouldRetry: (err, attempt, options) => shouldRetry(err, attempt, options.retryDelays?.length ?? 0),
      onUploadUrlAvailable: () => {
        const id = idFromUrl(upload.url)
        if (id) {
          this.set({ serverId: id })
          void this.sendDigest()
        }
      },
      onProgress: (sent, total) => this.onProgress(sent, total),
      onSuccess: () => void this.afterTransfer(),
      onError: (err) => this.onTusError(err),
    })
    this.tus = upload
    try {
      const previous = await upload.findPreviousUploads()
      if (previous.length > 0) upload.resumeFromPreviousUpload(previous[0])
    } catch {
      /* storage unavailable: just start fresh */
    }
    this.lastSampleAt = 0
    upload.start()
  }

  private onProgress(sent: number, total: number) {
    // Exponentially smoothed throughput from successive progress samples.
    const now = Date.now()
    let bps = this.snap.bytesPerSec
    if (this.lastSampleAt && now > this.lastSampleAt && sent >= this.lastSampleBytes) {
      const inst = ((sent - this.lastSampleBytes) * 1000) / (now - this.lastSampleAt)
      bps = bps > 0 ? bps * 0.8 + inst * 0.2 : inst
    }
    this.lastSampleAt = now
    this.lastSampleBytes = sent
    this.set({ uploadedBytes: sent, totalBytes: total, bytesPerSec: bps })
  }

  private onTusError(err: Error | DetailedError) {
    const d = err as DetailedError
    const status = d.originalResponse?.getStatus?.()
    if (status === 401) {
      this.set({ status: 'paused', error: 'Your session expired. Log in again, then resume this upload.', bytesPerSec: 0 })
      return
    }
    if (status === 413) return this.fail('The server rejected this file as too large.')
    if (status === 507) return this.fail('The server does not have enough free disk space for this file.')
    if (status === 403 || status === 404 || status === 409) {
      // The stored upload URL is stale (cancelled, expired, finished or not ours): forget it.
      void this.tus?.findPreviousUploads().then(() => {}, () => {})
      return this.fail('This upload can no longer be continued. Start it again.')
    }
    this.fail(`Upload failed: ${err.message}`)
  }

  /** Posts the whole-file digest once both the upload id and the digest are known. */
  private async sendDigest() {
    const { serverId, digest } = this.snap
    if (this.digestSent || !serverId || !digest) return
    this.digestSent = true
    for (let attempt = 0; ; attempt++) {
      try {
        await api.submitDigest(serverId, digest)
        return
      } catch (e) {
        if (this.disposed || this.snap.status === 'cancelled') return
        if (e instanceof ApiError && e.status >= 400 && e.status < 500 && e.status !== 429) {
          this.digestSent = false
          if (e.status === 409) this.fail('The server already holds a different checksum for this upload.')
          else this.fail(`The checksum could not be submitted: ${e.message}`)
          return
        }
        if (attempt >= 8) {
          this.digestSent = false
          this.fail('The checksum could not be submitted after several attempts.')
          return
        }
        await sleep(Math.min(30_000, 1000 * 2 ** attempt))
      }
    }
  }

  /** All bytes are on the server; wait for the digest, then for server verification. */
  private async afterTransfer() {
    this.set({ status: 'finishing', uploadedBytes: this.snap.totalBytes, bytesPerSec: 0 })
    try {
      await this.hash?.promise
    } catch {
      return // fail() already ran
    }
    await this.sendDigest()
    if (this.snap.status === 'failed' || this.snap.status === 'cancelled') return
    await this.poll()
  }

  private async poll() {
    const id = this.snap.serverId
    if (!id) return
    while (!this.disposed && this.snap.status !== 'cancelled' && this.snap.status !== 'failed') {
      let u: ServerUpload
      try {
        u = await api.getUpload(id)
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return this.fail('The upload disappeared from the server.')
        await sleep(this.opts.pollMs ?? POLL_MS)
        continue
      }
      this.set({ serverState: u.state, verifiedBytes: u.verified_bytes ?? this.snap.verifiedBytes })
      switch (u.state) {
        case 'complete':
          return this.set({ status: 'complete', bytesPerSec: 0, error: null })
        case 'failed_hash':
          return this.fail(`The server's copy did not match your file (${u.error || 'checksum mismatch'}). Upload it again.`)
        case 'aborted':
        case 'expired':
          return this.fail(`The upload was ${u.state} on the server.`)
        case 'verifying':
          this.set({ status: 'verifying' })
          break
        default:
          if (u.error) this.set({ error: u.error })
      }
      await sleep(this.opts.pollMs ?? POLL_MS)
    }
  }

  pause() {
    if (this.snap.status !== 'uploading') return
    void this.tus?.abort(false)
    this.set({ status: 'paused', bytesPerSec: 0 })
  }

  async resume() {
    if (this.snap.status !== 'paused') return
    this.digestSent = false
    this.set({ status: 'uploading', error: null })
    this.beginHashing()
    await this.beginTus()
  }

  async cancel() {
    this.hash?.cancel()
    try {
      await this.tus?.abort(true) // sends DELETE so the server drops the partial file
    } catch {
      /* best effort; the janitor removes abandoned partials */
    }
    this.set({ status: 'cancelled', bytesPerSec: 0 })
  }

  dispose() {
    this.disposed = true
  }
}

export function idFromUrl(url: string | null | undefined): string | null {
  if (!url) return null
  const m = /\/files\/([0-9a-f]{32})\/?$/.exec(url)
  return m ? m[1] : null
}

/**
 * Retry policy: always retry a chunk the server rejected on checksum (460) or
 * any network/5xx failure; never retry auth or other client errors, except the
 * tus-specific 409/423 conflicts.
 */
export function shouldRetry(err: Error | DetailedError, attempt: number, maxAttempts: number): boolean {
  if (attempt >= maxAttempts) return false
  const status = (err as DetailedError).originalResponse?.getStatus?.()
  if (status === undefined) return true // network error
  if (status === 460) return true
  if (status === 409 || status === 423) return true
  return !(status >= 400 && status < 500)
}
