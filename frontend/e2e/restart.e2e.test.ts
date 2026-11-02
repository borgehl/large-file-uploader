/**
 * Durability: the backend is restarted (docker compose restart) in the middle of an upload and
 * between "all bytes received" and "digest submitted". Needs E2E_COMPOSE_DIR (repo root).
 */
import { randomBytes } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { createWriteStream, mkdtempSync, rmSync } from 'node:fs'
import { once } from 'node:events'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { defaultOptions } from 'tus-js-client'
import { api, BASE, CHUNK, makeUser, MiB, ROOT_PW, rootSession, sha256File, startUpload, waitFor, type Session } from './helpers'

const COMPOSE_DIR = process.env.E2E_COMPOSE_DIR ?? ''
const SIZE = 5 * CHUNK + 4321

function restartBackend() {
  execFileSync('docker', ['compose', 'restart', 'backend'], { cwd: COMPOSE_DIR, stdio: 'ignore' })
}

async function backendUp() {
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(BASE + '/api/healthz')).ok) return
    } catch {
      /* not yet */
    }
    await new Promise((r) => setTimeout(r, 200))
  }
  throw new Error('backend did not come back')
}

describe.skipIf(!BASE || !ROOT_PW || !COMPOSE_DIR)('server restarts', () => {
  let dir: string
  let file: string
  let digest: string
  let user: Session

  beforeAll(async () => {
    dir = mkdtempSync(join(tmpdir(), 'lfu-restart-'))
    file = join(dir, 'r.bin')
    const w = createWriteStream(file)
    for (let i = 0; i < SIZE; i += MiB) w.write(randomBytes(Math.min(MiB, SIZE - i)))
    w.end()
    await once(w, 'finish')
    digest = await sha256File(file)
    user = (await makeUser(await rootSession(), 'rs' + randomBytes(3).toString('hex'))).session
  }, 120_000)

  afterAll(() => rmSync(dir, { recursive: true, force: true }))

  it('keeps the session and partial upload across a restart, then resumes and completes', async () => {
    const first = startUpload(file, user, { stack: defaultOptions.httpStack, stopAfter: 2 * CHUNK })
    const { url } = await first.done
    const id = url.split('/').pop()!
    const before = (await api(user, 'GET', `/api/uploads/${id}`)).json.offset as number
    expect(before).toBeGreaterThanOrEqual(2 * CHUNK)

    restartBackend()
    await backendUp()

    // sessions live in SQLite, so the same cookie still works, and the offset is unchanged
    const after = await api(user, 'GET', `/api/uploads/${id}`)
    expect(after.res.status).toBe(200)
    expect(after.json.offset).toBe(before)

    await startUpload(file, user, { stack: defaultOptions.httpStack, uploadUrl: url }).done
    expect((await api(user, 'POST', `/api/uploads/${id}/sha256`, { sha256: digest })).res.status).toBe(200)
    expect((await waitFor(user, id, 'complete')).server_sha256).toBe(digest)
  }, 240_000)

  it('finishes verification after a restart that happened before the digest arrived', async () => {
    const { url } = await startUpload(file, user, { stack: defaultOptions.httpStack }).done
    const id = url.split('/').pop()!
    await waitFor(user, id, 'received')

    restartBackend()
    await backendUp()

    expect((await api(user, 'POST', `/api/uploads/${id}/sha256`, { sha256: digest })).res.status).toBe(200)
    expect((await waitFor(user, id, 'complete')).server_sha256).toBe(digest)
  }, 240_000)
})
