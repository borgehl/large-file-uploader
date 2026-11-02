/**
 * Real-browser end-to-end test (Chrome via playwright-core) against a running stack.
 *
 *   E2E_BASE=http://127.0.0.1:8081 E2E_ROOT_PASSWORD=<root pw> E2E_UPLOAD_DIR=../data/uploads \
 *   [E2E_CHROME=/usr/bin/google-chrome] pnpm vitest run e2e/browser
 *
 * Drives the actual UI: login, file picker, Blob chunks, the hashing Web Worker under the
 * production Content-Security-Policy, throttled upload with Pause/Resume, server verification.
 */
import { randomBytes } from 'node:crypto'
import { createWriteStream, mkdtempSync, rmSync, statSync } from 'node:fs'
import { once } from 'node:events'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { chromium, type Browser, type Page } from 'playwright-core'
import { api, BASE, makeUser, MiB, ROOT_PW, rootSession, sha256File, UPLOAD_DIR, type Session } from './helpers'

const FILE_SIZE = 300 * MiB + 777

describe.skipIf(!BASE || !ROOT_PW)('browser UI', () => {
  let browser: Browser
  let dir: string
  let file: string
  let digest: string
  let session: Session
  const username = 'ui' + randomBytes(3).toString('hex')
  let password = ''

  beforeAll(async () => {
    dir = mkdtempSync(join(tmpdir(), 'lfu-ui-'))
    file = join(dir, 'ui upload.bin')
    const w = createWriteStream(file)
    for (let i = 0; i < FILE_SIZE; i += MiB) w.write(randomBytes(Math.min(MiB, FILE_SIZE - i)))
    w.end()
    await once(w, 'finish')
    digest = await sha256File(file)
    const made = await makeUser(await rootSession(), username)
    session = made.session
    password = made.password
    browser = await chromium.launch({
      executablePath: process.env.E2E_CHROME ?? '/usr/bin/google-chrome',
      headless: true,
      args: ['--no-sandbox'],
    })
  }, 180_000)

  afterAll(async () => {
    await browser?.close()
    rmSync(dir, { recursive: true, force: true })
  })

  async function openLoggedIn(): Promise<{ page: Page; problems: string[] }> {
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    const problems: string[] = []
    page.on('pageerror', (e) => problems.push('pageerror: ' + e.message))
    page.on('console', (m) => {
      if (m.type() !== 'error') return
      // Expected: the app probes /api/auth/me before login and Chrome logs that 401.
      if (m.location().url.endsWith('/api/auth/me') && /401/.test(m.text())) return
      problems.push('console: ' + m.text())
    })
    await page.goto(BASE + '/')
    await page.waitForURL('**/login')
    await page.getByLabel('Username').fill(username)
    await page.getByLabel('Password').fill(password)
    await page.getByRole('button', { name: 'Log in' }).click()
    await page.getByRole('heading', { name: 'Upload files' }).waitFor()
    return { page, problems }
  }

  it('redirects anonymous visitors to login and rejects bad credentials', async () => {
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    await page.goto(BASE + '/uploads')
    await page.waitForURL('**/login')
    await page.getByLabel('Username').fill(username)
    await page.getByLabel('Password').fill('definitely wrong password')
    await page.getByRole('button', { name: 'Log in' }).click()
    await page.getByRole('alert').waitFor()
    expect(await page.getByRole('alert').innerText()).toMatch(/invalid username or password/i)
    await ctx.close()
  }, 60_000)

  it('uploads a 300 MiB file with pause/resume and ends in a server-verified state', async () => {
    const { page, problems } = await openLoggedIn()
    // Throttle the upload so there is time to pause mid-transfer.
    const cdp = await page.context().newCDPSession(page)
    await cdp.send('Network.enable')
    await cdp.send('Network.emulateNetworkConditions', {
      offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: 25 * MiB,
    })

    await page.locator('input[type=file]').setInputFiles(file)
    const bar = page.getByRole('progressbar', { name: /Upload progress/ })
    await bar.waitFor()

    // wait until a few percent are sent, then pause
    await expect.poll(async () => Number(await bar.getAttribute('aria-valuenow')), { timeout: 60_000 }).toBeGreaterThan(10)
    await page.getByRole('button', { name: 'Pause' }).click()
    await page.getByText('Paused', { exact: true }).waitFor()
    const pausedAt = Number(await bar.getAttribute('aria-valuenow'))
    expect(pausedAt).toBeLessThan(100)
    await page.waitForTimeout(1500)
    expect(Number(await bar.getAttribute('aria-valuenow'))).toBeLessThanOrEqual(pausedAt + 12) // may finish the in-flight chunk, then stops

    await page.getByRole('button', { name: 'Resume' }).click()
    await page.getByText(/Complete: the server verified the checksum/).waitFor({ timeout: 240_000 })

    // The UI shows the digest it computed in the worker; it must equal the real file hash.
    expect(await page.getByText(/^SHA-256 [0-9a-f]{64}$/).innerText()).toBe('SHA-256 ' + digest)

    // The server record agrees, and the file on the host is intact.
    const { json: list } = await api(session, 'GET', '/api/uploads')
    const rec = list.find((u: { original_name: string }) => u.original_name === 'ui upload.bin')
    expect(rec.state).toBe('complete')
    expect(rec.server_sha256).toBe(digest)
    if (UPLOAD_DIR) {
      const stored = join(UPLOAD_DIR, rec.stored_path)
      expect(statSync(stored).size).toBe(FILE_SIZE)
      expect(await sha256File(stored)).toBe(digest)
    }

    // No CSP violations, worker/wasm failures or other console errors on the way.
    expect(problems).toEqual([])
    await page.context().close()
  }, 360_000)

  it('rejects a file at the size limit in the UI before sending anything', async () => {
    const { page } = await openLoggedIn()
    const cfg = (await api(session, 'GET', '/api/config')).json
    // A sparse file exactly at the limit: allocation is free, and it must be refused client-side.
    const big = join(dir, 'at-limit.bin')
    const { openSync, ftruncateSync, closeSync } = await import('node:fs')
    const fd = openSync(big, 'w')
    ftruncateSync(fd, cfg.max_upload_bytes)
    closeSync(fd)
    await page.locator('input[type=file]').setInputFiles(big)
    await page.getByRole('alert').filter({ hasText: /must be smaller than/ }).waitFor()
    const { json: list } = await api(session, 'GET', '/api/uploads')
    expect(list.some((u: { original_name: string }) => u.original_name === 'at-limit.bin')).toBe(false)
    await page.context().close()
  }, 120_000)
})
