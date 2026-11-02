/**
 * Keeps the screen (and so the machine) awake while a transfer is running, using the
 * Screen Wake Lock API. Best effort: it needs a secure context (HTTPS or localhost), is
 * missing in some browsers, and the browser releases the lock whenever the tab is hidden,
 * so it is re-requested when the tab becomes visible again. It cannot stop a manual sleep
 * or a closed laptop lid.
 */

export interface WakeLockSentinelLike {
  release(): Promise<void>
  addEventListener(type: 'release', listener: () => void): void
}

export interface WakeLockEnv {
  request?: (type: 'screen') => Promise<WakeLockSentinelLike>
  isVisible: () => boolean
  onVisible: (cb: () => void) => () => void
}

export function browserWakeLockEnv(): WakeLockEnv {
  const wl = typeof navigator !== 'undefined' ? (navigator as Navigator).wakeLock : undefined
  return {
    request: wl ? (t) => wl.request(t) : undefined,
    isVisible: () => document.visibilityState === 'visible',
    onVisible: (cb) => {
      const h = () => document.visibilityState === 'visible' && cb()
      document.addEventListener('visibilitychange', h)
      return () => document.removeEventListener('visibilitychange', h)
    },
  }
}

export class WakeLockController {
  private wanted = false
  private sentinel: WakeLockSentinelLike | null = null
  private pending = false
  private off: (() => void) | null = null

  private env: WakeLockEnv

  constructor(env: WakeLockEnv = browserWakeLockEnv()) {
    this.env = env
  }

  get supported(): boolean {
    return !!this.env.request
  }

  get held(): boolean {
    return this.sentinel !== null
  }

  /** Call whenever "is a transfer running" may have changed. */
  setWanted(wanted: boolean): void {
    if (wanted === this.wanted) return
    this.wanted = wanted
    if (wanted) {
      this.off = this.env.onVisible(() => void this.acquire())
      void this.acquire()
    } else {
      this.off?.()
      this.off = null
      const s = this.sentinel
      this.sentinel = null
      void s?.release().catch(() => {})
    }
  }

  private async acquire(): Promise<void> {
    if (!this.wanted || this.sentinel || this.pending || !this.env.request || !this.env.isVisible()) return
    this.pending = true
    try {
      const s = await this.env.request('screen')
      if (!this.wanted) {
        void s.release().catch(() => {})
        return
      }
      s.addEventListener('release', () => {
        if (this.sentinel === s) this.sentinel = null
      })
      this.sentinel = s
    } catch {
      // denied (battery saver, insecure context, ...): uploading still works, the machine may just sleep
    } finally {
      this.pending = false
    }
  }

  dispose(): void {
    this.setWanted(false)
  }
}
