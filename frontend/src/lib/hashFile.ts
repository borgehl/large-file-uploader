import type { HashRequest, HashResponse } from '../workers/sha256.worker'

export interface HashResult {
  digest: string
  size: number
  lastModified: number
}

export interface HashHandle {
  promise: Promise<HashResult>
  cancel: () => void
}

/** Hashes a file in a Web Worker so the UI thread stays responsive. */
export function hashFile(file: File, onProgress: (hashed: number, total: number) => void): HashHandle {
  const worker = new Worker(new URL('../workers/sha256.worker.ts', import.meta.url), { type: 'module' })
  let rejectFn: (e: Error) => void = () => {}
  const promise = new Promise<HashResult>((resolve, reject) => {
    rejectFn = reject
    worker.onmessage = (ev: MessageEvent<HashResponse>) => {
      const m = ev.data
      if (m.type === 'progress') onProgress(m.hashedBytes, m.totalBytes)
      else if (m.type === 'done') {
        worker.terminate()
        resolve({ digest: m.digest, size: m.size, lastModified: m.lastModified })
      } else {
        worker.terminate()
        reject(new Error(m.message))
      }
    }
    worker.onerror = (e) => {
      worker.terminate()
      reject(new Error(e.message || 'hash worker failed'))
    }
    worker.postMessage({ type: 'start', file } satisfies HashRequest)
  })
  return {
    promise,
    cancel: () => {
      worker.terminate()
      rejectFn(new DOMException('hashing cancelled', 'AbortError'))
    },
  }
}
