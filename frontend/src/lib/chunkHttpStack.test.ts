import { describe, expect, it } from 'vitest'
import type { HttpRequest, HttpResponse, HttpStack } from 'tus-js-client'
import { CHUNK_HEADER, chunkHashingStack } from './chunkHttpStack'

const ABC = 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'

function fakeStack() {
  const sent: { method: string; headers: Record<string, string>; body: unknown }[] = []
  const stack: HttpStack = {
    getName: () => 'fake',
    createRequest(method: string, url: string): HttpRequest {
      const headers: Record<string, string> = {}
      return {
        getMethod: () => method,
        getURL: () => url,
        setHeader: (k, v) => { headers[k] = v },
        getHeader: (k) => headers[k],
        setProgressHandler: () => {},
        abort: async () => {},
        getUnderlyingObject: () => null,
        send: async (body) => {
          sent.push({ method, headers: { ...headers }, body })
          return { getStatus: () => 204 } as HttpResponse
        },
      }
    },
  }
  return { stack, sent }
}

describe('chunkHashingStack', () => {
  it('adds the SHA-256 of a Blob body to PATCH requests', async () => {
    const { stack, sent } = fakeStack()
    const req = chunkHashingStack(stack).createRequest('PATCH', '/files/x')
    await req.send(new Blob(['abc']))
    expect(sent[0].headers[CHUNK_HEADER]).toBe(ABC)
  })

  it('hashes ArrayBuffer and typed array bodies too', async () => {
    const { stack, sent } = fakeStack()
    const s = chunkHashingStack(stack)
    await s.createRequest('PATCH', '/f').send(new TextEncoder().encode('abc'))
    await s.createRequest('PATCH', '/f').send(new TextEncoder().encode('abc').buffer)
    expect(sent.map((x) => x.headers[CHUNK_HEADER])).toEqual([ABC, ABC])
  })

  it('leaves other methods alone', async () => {
    const { stack, sent } = fakeStack()
    const s = chunkHashingStack(stack)
    await s.createRequest('POST', '/files/').send(null)
    await s.createRequest('HEAD', '/files/x').send(null)
    expect(sent.every((x) => !(CHUNK_HEADER in x.headers))).toBe(true)
  })

  it('refuses bodies it cannot hash instead of sending them unverified', async () => {
    const { stack, sent } = fakeStack()
    const req = chunkHashingStack(stack).createRequest('PATCH', '/f')
    await expect(req.send('a string')).rejects.toThrow(/checksum/)
    expect(sent).toHaveLength(0)
  })
})
