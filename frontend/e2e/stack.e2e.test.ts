/**
 * End-to-end test against a running stack (docker compose up). Skipped unless E2E_BASE is set:
 *
 *   E2E_BASE=http://127.0.0.1:8081 E2E_ROOT_PASSWORD=<root password> \
 *   E2E_UPLOAD_DIR=../data/uploads pnpm vitest run e2e
 *
 * Uses the real tus-js-client plus the same chunk-hashing HTTP stack as the browser.
 */
import { randomBytes } from 'node:crypto'
import { createWriteStream, mkdtempSync, readFileSync, rmSync, statSync } from 'node:fs'
import { once } from 'node:events'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { defaultOptions } from 'tus-js-client'
import { api, BASE, CHUNK, corruptOnce, MiB, makeUser, ROOT_PW, rootSession, sha256File, startUpload, UPLOAD_DIR, waitFor, type Session } from './helpers'

const FILE_SIZE = 256 * MiB + 12345 // not a multiple of the chunk size

describe.skipIf(!BASE || !ROOT_PW)('live stack', () => {
  let dir: string
  let file: string
  let digest: string
  let user: Session
  const username = 'e2e' + randomBytes(3).toString('hex')

  beforeAll(async () => {
    dir = mkdtempSync(join(tmpdir(), 'lfu-e2e-'))
    file = join(dir, 'big.bin')
    const w = createWriteStream(file)
    for (let i = 0; i < FILE_SIZE; i += MiB) w.write(randomBytes(Math.min(MiB, FILE_SIZE - i)))
    w.end()
    await once(w, 'finish')
    digest = await sha256File(file)

    const made = await makeUser(await rootSession(), username)
    user = made.session
  }, 120_000)

  afterAll(() => rmSync(dir, { recursive: true, force: true }))

  it('uploads 256 MiB through nginx, survives a corrupted chunk, and finalizes with a verified hash', async () => {
    const counter = { corrupted: 0 }
    const job = startUpload(file, user, { stack: corruptOnce(defaultOptions.httpStack, counter) })
    const { url } = await job.done
    const id = url.split('/').pop()!
    expect(id).toMatch(/^[0-9a-f]{32}$/)
    expect(counter.corrupted).toBe(1)
    expect(job.retried460.n).toBe(1) // the server rejected the bad chunk with 460 and the client resent it

    expect((await api(user, 'POST', `/api/uploads/${id}/sha256`, { sha256: digest })).res.status).toBe(200)
    const final = await waitFor(user, id, 'complete')
    expect(final.server_sha256).toBe(digest)
    expect(final.size_bytes).toBe(FILE_SIZE)

    if (UPLOAD_DIR) {
      const stored = join(UPLOAD_DIR, final.stored_path as string)
      expect(final.stored_path).toMatch(/^complete\/\d{4}\/[0-9a-f]{32}\/e2e big _file_\.bin$/)
      expect(statSync(stored).size).toBe(FILE_SIZE)
      expect(await sha256File(stored)).toBe(digest)
      const mf = JSON.parse(readFileSync(join(stored, '..', 'manifest.json'), 'utf8'))
      expect(mf.sha256).toBe(digest)
      expect(mf.uploaded_by).toBe(username)
    }
  }, 300_000)

  it('resumes an interrupted upload from the server offset', async () => {
    const first = startUpload(file, user, { stack: defaultOptions.httpStack, stopAfter: 3 * CHUNK })
    const { url } = await first.done
    expect(url).toMatch(/\/files\/[0-9a-f]{32}$/)
    const id = url.split('/').pop()!
    const mid = await api(user, 'GET', `/api/uploads/${id}`)
    expect(mid.json.state).toBe('created')
    expect(mid.json.offset).toBeGreaterThanOrEqual(3 * CHUNK)
    expect(mid.json.offset).toBeLessThan(FILE_SIZE)

    // a new client instance continues at the existing URL (what a page reload + re-select does)
    const second = startUpload(file, user, { stack: defaultOptions.httpStack, uploadUrl: url })
    await second.done
    expect((await api(user, 'POST', `/api/uploads/${id}/sha256`, { sha256: digest })).res.status).toBe(200)
    const final = await waitFor(user, id, 'complete')
    expect(final.server_sha256).toBe(digest)
  }, 300_000)

  it('quarantines when the digest does not match what was sent', async () => {
    const job = startUpload(file, user, { stack: defaultOptions.httpStack })
    const { url } = await job.done
    const id = url.split('/').pop()!
    await api(user, 'POST', `/api/uploads/${id}/sha256`, { sha256: '0'.repeat(64) })
    const bad = await waitFor(user, id, 'failed_hash')
    expect(bad.error).toMatch(/mismatch/)
  }, 300_000)
})
