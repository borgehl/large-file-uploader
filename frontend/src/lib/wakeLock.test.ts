import { describe, expect, it } from 'vitest'
import { WakeLockController, type WakeLockEnv, type WakeLockSentinelLike } from './wakeLock'

function fakeEnv(opts: { supported?: boolean; deny?: boolean } = {}) {
  const state = { requests: 0, releases: 0, visible: true, listeners: [] as Array<() => void>, sentinels: [] as Array<() => void> }
  const env: WakeLockEnv = {
    request:
      opts.supported === false
        ? undefined
        : async () => {
            state.requests++
            if (opts.deny) throw new Error('NotAllowedError')
            let onRelease: () => void = () => {}
            const s: WakeLockSentinelLike = {
              release: async () => {
                state.releases++
              },
              addEventListener: (_t, l) => {
                onRelease = l
              },
            }
            state.sentinels.push(() => onRelease())
            return s
          },
    isVisible: () => state.visible,
    onVisible: (cb) => {
      state.listeners.push(cb)
      return () => {
        state.listeners = state.listeners.filter((l) => l !== cb)
      }
    },
  }
  return { env, state }
}

const tick = () => new Promise((r) => setTimeout(r, 0))

describe('WakeLockController', () => {
  it('acquires when wanted and releases when not', async () => {
    const { env, state } = fakeEnv()
    const c = new WakeLockController(env)
    c.setWanted(true)
    await tick()
    expect(c.held).toBe(true)
    expect(state.requests).toBe(1)
    c.setWanted(false)
    await tick()
    expect(c.held).toBe(false)
    expect(state.releases).toBe(1)
    expect(state.listeners).toHaveLength(0)
  })

  it('does not request twice for repeated setWanted(true)', async () => {
    const { env, state } = fakeEnv()
    const c = new WakeLockController(env)
    c.setWanted(true)
    c.setWanted(true)
    await tick()
    expect(state.requests).toBe(1)
  })

  it('re-acquires when the tab becomes visible after the browser dropped the lock', async () => {
    const { env, state } = fakeEnv()
    const c = new WakeLockController(env)
    c.setWanted(true)
    await tick()
    state.visible = false
    state.sentinels[0]() // browser releases on hide
    expect(c.held).toBe(false)
    state.visible = true
    state.listeners.forEach((l) => l())
    await tick()
    expect(c.held).toBe(true)
    expect(state.requests).toBe(2)
  })

  it('does not request while the tab is hidden', async () => {
    const { env, state } = fakeEnv()
    state.visible = false
    const c = new WakeLockController(env)
    c.setWanted(true)
    await tick()
    expect(state.requests).toBe(0)
  })

  it('releases a lock that arrives after it was no longer wanted', async () => {
    const { env, state } = fakeEnv()
    const c = new WakeLockController(env)
    c.setWanted(true)
    c.setWanted(false)
    await tick()
    expect(c.held).toBe(false)
    expect(state.releases).toBe(1)
  })

  it('is a harmless no-op when unsupported or denied', async () => {
    const a = new WakeLockController(fakeEnv({ supported: false }).env)
    expect(a.supported).toBe(false)
    a.setWanted(true)
    await tick()
    expect(a.held).toBe(false)

    const b = new WakeLockController(fakeEnv({ deny: true }).env)
    b.setWanted(true)
    await tick()
    expect(b.held).toBe(false)
  })
})
