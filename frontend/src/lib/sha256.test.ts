import { describe, expect, it } from 'vitest'
import { sha256Blob, sha256Buffer, toHex } from './sha256'

const EMPTY = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
const ABC = 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'

describe('sha256Blob', () => {
  it('matches known vectors', async () => {
    expect(await sha256Blob(new Blob([]))).toBe(EMPTY)
    expect(await sha256Blob(new Blob(['abc']))).toBe(ABC)
  })

  it('streaming across slice boundaries equals one-shot Web Crypto', async () => {
    // 20 MiB + 13 bytes: spans three 8 MiB slices with an odd tail
    const data = new Uint8Array(20 * 1024 * 1024 + 13)
    for (let i = 0; i < data.length; i++) data[i] = (i * 31 + (i >> 9)) & 0xff
    const streamed = await sha256Blob(new Blob([data]))
    const oneShot = await sha256Buffer(data)
    expect(streamed).toBe(oneShot)
  })

  it('reports progress ending at the full size', async () => {
    const seen: number[] = []
    await sha256Blob(new Blob([new Uint8Array(9 * 1024 * 1024)]), (p) => seen.push(p.hashedBytes))
    expect(seen.at(-1)).toBe(9 * 1024 * 1024)
  })

  it('stops when aborted', async () => {
    const ctl = new AbortController()
    ctl.abort()
    await expect(sha256Blob(new Blob(['abc']), undefined, ctl.signal)).rejects.toThrow()
  })
})

describe('sha256Buffer', () => {
  it('hashes views by their own window, not the whole backing buffer', async () => {
    const backing = new Uint8Array([9, 9, 97, 98, 99, 9])
    expect(await sha256Buffer(backing.subarray(2, 5))).toBe(ABC)
    expect(await sha256Buffer(new ArrayBuffer(0))).toBe(EMPTY)
  })
  it('toHex pads', () => {
    expect(toHex(new Uint8Array([0, 1, 255]))).toBe('0001ff')
  })
})
