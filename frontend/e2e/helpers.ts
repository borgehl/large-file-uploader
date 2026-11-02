import { createHash, randomBytes } from 'node:crypto'
import { createReadStream } from 'node:fs'
import { once } from 'node:events'
import { readFileSync } from 'node:fs'
import { expect } from 'vitest'
import { Upload, type DetailedError, type HttpRequest, type HttpResponse, type HttpStack } from 'tus-js-client'
import { chunkHashingStack } from '../src/lib/chunkHttpStack'
import { shouldRetry } from '../src/lib/uploadJob'

export const BASE = process.env.E2E_BASE ?? ''
export const ROOT_PW = process.env.E2E_ROOT_PASSWORD ?? ''
export const UPLOAD_DIR = process.env.E2E_UPLOAD_DIR ?? ''
export const MiB = 1024 * 1024
export const CHUNK = 32 * MiB

export interface Session {
  cookie: string
}

export async function api(s: Session | null, method: string, path: string, body?: unknown) {
  const res = await fetch(BASE + path, {
    method,
    headers: { 'Content-Type': 'application/json', ...(s ? { Cookie: s.cookie } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  return { res, json: text ? JSON.parse(text) : null }
}

export async function login(username: string, password: string): Promise<Session> {
  const { res } = await api(null, 'POST', '/api/auth/login', { username, password })
  expect(res.status).toBe(200)
  const cookie = (res.headers.getSetCookie()[0] ?? '').split(';')[0]
  expect(cookie).toMatch(/^lfu_session=/)
  return { cookie }
}

/** Logs in as root (completing the forced first-login password change if needed). */
export async function rootSession(): Promise<Session> {
  let root = await login('root', ROOT_PW).catch(() => null)
  if (!root) throw new Error('root login failed: set E2E_ROOT_PASSWORD to the CURRENT root password')
  const me = await api(root, 'GET', '/api/auth/me')
  if (me.json.must_change_password) {
    const np = ROOT_PW + '-changed-' + randomBytes(2).toString('hex')
    expect((await api(root, 'POST', '/api/auth/password', { current_password: ROOT_PW, new_password: np })).res.status).toBe(200)
    console.warn(`[e2e] root password was changed to: ${np}`)
    root = await login('root', np)
  }
  return root
}

/** Creates a user via the admin API and finishes its forced password change. */
export async function makeUser(root: Session, username: string): Promise<{ session: Session; password: string }> {
  const created = await api(root, 'POST', '/api/admin/users', { username, role: 'user' })
  expect(created.res.status).toBe(201)
  const temp = created.json.temp_password as string
  const first = await login(username, temp)
  const password = 'e2e-permanent-' + randomBytes(4).toString('hex')
  expect((await api(first, 'POST', '/api/auth/password', { current_password: temp, new_password: password })).res.status).toBe(200)
  return { session: await login(username, password), password }
}

export async function sha256File(path: string): Promise<string> {
  const h = createHash('sha256')
  const s = createReadStream(path)
  s.on('data', (c) => h.update(c))
  await once(s, 'end')
  return h.digest('hex')
}

export async function waitFor(s: Session, id: string, state: string) {
  for (let i = 0; i < 600; i++) {
    const { json } = await api(s, 'GET', `/api/uploads/${id}`)
    if (json.state === state) return json
    if (json.state === 'failed_hash' && state !== 'failed_hash') throw new Error('unexpected failed_hash: ' + json.error)
    await new Promise((r) => setTimeout(r, 200))
  }
  throw new Error(`timeout waiting for ${state}`)
}

// Wraps a stack and corrupts the body of the first PATCH after the checksum header was computed on the clean bytes.
export function corruptOnce(base: HttpStack, counter: { corrupted: number }): HttpStack {
  return {
    getName: () => 'corrupt-once',
    createRequest(method: string, url: string): HttpRequest {
      const inner = base.createRequest(method, url)
      return new Proxy(inner, {
        get(t, p) {
          if (p === 'send') {
            return (body: unknown): Promise<HttpResponse> => {
              if (method === 'PATCH' && counter.corrupted === 0 && body instanceof Uint8Array && body.length > 10) {
                counter.corrupted++
                // keep tus' Node-only `size` tag on the slice: it is what sets Content-Length
                const bad = Object.assign(Buffer.from(body), { size: body.length })
                bad[5] ^= 0xff
                return t.send(bad)
              }
              return t.send(body)
            }
          }
          const v = Reflect.get(t, p)
          return typeof v === 'function' ? v.bind(t) : v
        },
      })
    },
  }
}

export function startUpload(path: string, s: Session, opts: { stack: HttpStack; uploadUrl?: string; stopAfter?: number }) {
  // A Buffer source makes tus hand byte bodies to the HTTP stack, like Blob slices in the browser.
  const source = readFileSync(path)
  const retried460 = { n: 0 }
  let upload!: Upload
  const done = new Promise<{ url: string }>((resolve, reject) => {
    upload = new Upload(source, {
      endpoint: BASE + '/files/',
      uploadUrl: opts.uploadUrl,
      chunkSize: CHUNK,
      retryDelays: [0, 200, 500],
      headers: { Cookie: s.cookie },
      metadata: { filename: '../../e2e big "file".bin' },
      httpStack: chunkHashingStack(opts.stack),
      onShouldRetry: (err: Error | DetailedError, attempt, options) => {
        if ((err as DetailedError).originalResponse?.getStatus() === 460) retried460.n++
        return shouldRetry(err, attempt, options.retryDelays?.length ?? 0)
      },
      onProgress: (sent) => {
        if (opts.stopAfter && sent >= opts.stopAfter) {
          opts.stopAfter = 0
          void upload.abort().then(() => resolve({ url: upload.url ?? '' }))
        }
      },
      onSuccess: () => resolve({ url: upload.url ?? '' }),
      onError: reject,
    })
    upload.start()
  })
  return { done, retried460, get url() { return upload.url } }
}

