import type { HttpRequest, HttpResponse, HttpStack } from 'tus-js-client'
import { sha256Buffer } from './sha256'

export const CHUNK_HEADER = 'X-Chunk-SHA256'

async function bodyBytes(body: unknown): Promise<ArrayBuffer | ArrayBufferView> {
  if (body instanceof Blob) return body.arrayBuffer()
  if (body instanceof ArrayBuffer || ArrayBuffer.isView(body)) return body
  throw new Error('unsupported upload body type; cannot compute chunk checksum')
}

class ChunkHashingRequest implements HttpRequest {
  private inner: HttpRequest
  constructor(inner: HttpRequest) {
    this.inner = inner
  }
  getMethod() { return this.inner.getMethod() }
  getURL() { return this.inner.getURL() }
  setHeader(h: string, v: string) { this.inner.setHeader(h, v) }
  getHeader(h: string) { return this.inner.getHeader(h) }
  setProgressHandler(fn: (n: number) => void) { this.inner.setProgressHandler(fn) }
  abort() { return this.inner.abort() }
  getUnderlyingObject() { return this.inner.getUnderlyingObject() }

  async send(body: unknown): Promise<HttpResponse> {
    if (this.inner.getMethod() === 'PATCH') {
      // The server refuses PATCH bodies without a matching SHA-256, so a chunk
      // corrupted in transit is rejected (HTTP 460) instead of being appended.
      const bytes = await bodyBytes(body ?? new ArrayBuffer(0))
      this.inner.setHeader(CHUNK_HEADER, await sha256Buffer(bytes))
    }
    return this.inner.send(body)
  }
}

/** Wraps another tus HttpStack so every PATCH carries X-Chunk-SHA256. */
export function chunkHashingStack(base: HttpStack): HttpStack {
  return {
    createRequest: (method, url) => new ChunkHashingRequest(base.createRequest(method, url)),
    getName: () => `${base.getName()}+chunk-sha256`,
  }
}
