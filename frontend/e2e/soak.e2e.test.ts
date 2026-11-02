/**
 * Opt-in soak: pushes a multi-GiB file through the real UI in Chrome and records timings and memory.
 *
 *   E2E_SOAK_GIB=8 E2E_BASE=... E2E_ROOT_PASSWORD=... E2E_UPLOAD_DIR=... E2E_COMPOSE_DIR=... \
 *   pnpm vitest run e2e/soak
 *
 * Needs free disk of about 3x the file size (source file, incoming/, then renamed into complete/).
 */
import { randomBytes } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { closeSync, mkdtempSync, openSync, rmSync, statSync, writeSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { chromium, type Browser } from 'playwright-core'
import { api, BASE, makeUser, MiB, ROOT_PW, rootSession, sha256File, UPLOAD_DIR, type Session } from './helpers'

const GIB = Number(process.env.E2E_SOAK_GIB ?? 0)
const COMPOSE_DIR = process.env.E2E_COMPOSE_DIR ?? ''

function backendMemMiB(): number {
  try {
    const out = execFileSync('docker', ['stats', '--no-stream', '--format', '{{.MemUsage}}', 'lfu-backend']).toString()
    const m = /([\d.]+)(MiB|GiB|KiB)/.exec(out)
    if (!m) return NaN
    return m[2] === 'GiB' ? Number(m[1]) * 1024 : m[2] === 'KiB' ? Number(m[1]) / 1024 : Number(m[1])
  } catch {
    return NaN
  }
}

describe.skipIf(!BASE || !ROOT_PW || !GIB)('soak', () => {
  let browser: Browser
  let dir: string
  let file: string
  let size: number
  let session: Session
  let password: string
  const username = 'soak' + randomBytes(3).toString('hex')

  beforeAll(async () => {
    size = Math.round(GIB * 1024 * MiB) + 4097
    dir = mkdtempSync(join(tmpdir(), 'lfu-soak-'))
    file = join(dir, 'soak.bin')
    const fd = openSync(file, 'w')
    for (let off = 0; off < size; off += MiB) writeSync(fd, randomBytes(Math.min(MiB, size - off)))
    closeSync(fd)
    const made = await makeUser(await rootSession(), username)
    session = made.session
    password = made.password
    browser = await chromium.launch({ executablePath: process.env.E2E_CHROME ?? '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox', '--enable-precise-memory-info'] })
  }, 900_000)

  afterAll(async () => {
    await browser?.close()
    rmSync(dir, { recursive: true, force: true })
  })

  it(`uploads ${GIB} GiB through the UI and verifies it`, async () => {
    const page = await (await browser.newContext()).newPage()
    await page.goto(BASE + '/login')
    await page.getByLabel('Username').fill(username)
    await page.getByLabel('Password').fill(password)
    await page.getByRole('button', { name: 'Log in' }).click()
    await page.getByRole('heading', { name: 'Upload files' }).waitFor()

    let peakHeap = 0
    let peakBackend = 0
    const sampler = setInterval(async () => {
      peakBackend = Math.max(peakBackend, backendMemMiB() || 0)
      const heap = await page.evaluate(() => (performance as unknown as { memory?: { usedJSHeapSize: number } }).memory?.usedJSHeapSize ?? 0).catch(() => 0)
      peakHeap = Math.max(peakHeap, heap / MiB)
    }, 2000)

    const t0 = Date.now()
    await page.locator('input[type=file]').setInputFiles(file)
    let tSent = 0
    let tVerifying = 0
    const start = Date.now()
    for (;;) {
      if (Date.now() - start > 40 * 60_000) throw new Error('soak timed out')
      const txt = await page.getByRole('listitem').first().innerText().catch(() => '')
      if (!tSent && /Server is verifying|Finishing|computing the file checksum|Complete:/.test(txt)) tSent = Date.now()
      if (!tVerifying && /Server is verifying/.test(txt)) tVerifying = Date.now()
      if (/Complete: the server verified/.test(txt)) break
      if (/Failed|Cancelled/.test(txt) && !/Uploading|Finishing/.test(txt)) throw new Error('upload failed in UI: ' + txt)
      await new Promise((r) => setTimeout(r, 1000))
    }
    const tDone = Date.now()
    clearInterval(sampler)

    const mbps = (size / MiB) / ((tSent - t0) / 1000)
    console.log(
      `[soak] ${GIB} GiB: bytes on server after ${((tSent - t0) / 1000).toFixed(0)}s (${mbps.toFixed(0)} MiB/s); ` +
        `server verification ${tVerifying ? ((tDone - tVerifying) / 1000).toFixed(0) : '<2'}s; total ${((tDone - t0) / 1000).toFixed(0)}s; ` +
        `peak page JS heap ${peakHeap.toFixed(0)} MiB; peak backend RSS ${peakBackend.toFixed(0)} MiB` +
        (COMPOSE_DIR ? '' : ' (set E2E_COMPOSE_DIR for backend numbers)'),
    )

    const { json: list } = await api(session, 'GET', '/api/uploads')
    const rec = list.find((u: { original_name: string }) => u.original_name === 'soak.bin')
    expect(rec.state).toBe('complete')
    const local = await sha256File(file)
    expect(rec.server_sha256).toBe(local)
    if (UPLOAD_DIR) expect(statSync(join(UPLOAD_DIR, rec.stored_path)).size).toBe(size)
    await page.context().close()
  }, 3_000_000)
})
