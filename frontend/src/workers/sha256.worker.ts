/// <reference lib="webworker" />
import { sha256Blob } from '../lib/sha256'

export type HashRequest = { type: 'start'; file: File }
export type HashResponse =
  | { type: 'progress'; hashedBytes: number; totalBytes: number }
  | { type: 'done'; digest: string; size: number; lastModified: number }
  | { type: 'error'; message: string }

const ctx = self as unknown as DedicatedWorkerGlobalScope

ctx.onmessage = async (ev: MessageEvent<HashRequest>) => {
  if (ev.data.type !== 'start') return
  const file = ev.data.file
  try {
    const digest = await sha256Blob(file, (p) => ctx.postMessage({ type: 'progress', ...p } satisfies HashResponse))
    ctx.postMessage({ type: 'done', digest, size: file.size, lastModified: file.lastModified } satisfies HashResponse)
  } catch (e) {
    ctx.postMessage({ type: 'error', message: e instanceof Error ? e.message : String(e) } satisfies HashResponse)
  }
}
