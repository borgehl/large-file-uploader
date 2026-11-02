import { createSHA256 } from 'hash-wasm'

const SLICE_BYTES = 8 * 1024 * 1024

export interface HashProgress {
  hashedBytes: number
  totalBytes: number
}

/**
 * Streams a Blob/File through an incremental SHA-256 (Web Crypto cannot hash
 * incrementally) and returns the lowercase hex digest. Reads 8 MiB slices so a
 * 100 GiB file never has to fit in memory.
 */
export async function sha256Blob(
  blob: Blob,
  onProgress?: (p: HashProgress) => void,
  signal?: AbortSignal,
): Promise<string> {
  const hasher = await createSHA256()
  hasher.init()
  let pos = 0
  let lastReport = 0
  while (pos < blob.size) {
    if (signal?.aborted) throw new DOMException('hashing aborted', 'AbortError')
    const buf = await blob.slice(pos, pos + SLICE_BYTES).arrayBuffer()
    hasher.update(new Uint8Array(buf))
    pos += buf.byteLength
    if (buf.byteLength === 0) throw new Error('unexpected end of file while hashing')
    const now = Date.now()
    if (onProgress && (now - lastReport > 250 || pos >= blob.size)) {
      lastReport = now
      onProgress({ hashedBytes: pos, totalBytes: blob.size })
    }
  }
  return hasher.digest('hex')
}

/** One-shot SHA-256 of a buffer using Web Crypto (used per chunk). */
export async function sha256Buffer(buf: ArrayBuffer | ArrayBufferView): Promise<string> {
  const view = ArrayBuffer.isView(buf)
    ? new Uint8Array(buf.buffer as ArrayBuffer, buf.byteOffset, buf.byteLength)
    : new Uint8Array(buf)
  const digest = await crypto.subtle.digest('SHA-256', view)
  return toHex(new Uint8Array(digest))
}

export function toHex(bytes: Uint8Array): string {
  let s = ''
  for (const b of bytes) s += b.toString(16).padStart(2, '0')
  return s
}
