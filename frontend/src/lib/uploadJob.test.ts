import { describe, expect, it } from 'vitest'
import type { DetailedError } from 'tus-js-client'
import { idFromUrl, shouldRetry } from './uploadJob'

const withStatus = (status?: number) =>
  Object.assign(new Error('x'), {
    originalResponse: status === undefined ? null : { getStatus: () => status },
  }) as unknown as DetailedError

describe('shouldRetry', () => {
  it('retries checksum rejections, network errors, 5xx and tus conflicts', () => {
    for (const s of [460, 500, 502, 503, 409, 423, undefined]) expect(shouldRetry(withStatus(s), 0, 6)).toBe(true)
  })
  it('does not retry auth, validation or size errors', () => {
    for (const s of [400, 401, 403, 404, 413]) expect(shouldRetry(withStatus(s), 0, 6)).toBe(false)
  })
  it('stops after the last attempt', () => {
    expect(shouldRetry(withStatus(460), 6, 6)).toBe(false)
  })
})

describe('idFromUrl', () => {
  const id = 'a'.repeat(32)
  it('extracts the upload id', () => {
    expect(idFromUrl(`https://host/files/${id}`)).toBe(id)
    expect(idFromUrl(`/files/${id}/`)).toBe(id)
  })
  it('rejects anything else', () => {
    expect(idFromUrl(null)).toBeNull()
    expect(idFromUrl('/files/../etc')).toBeNull()
    expect(idFromUrl('/files/short')).toBeNull()
  })
})
