import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

describe('trackUsageBeaconPixel', () => {
  let store: Record<string, string>

  beforeEach(() => {
    vi.resetModules()
    store = {}
    vi.stubGlobal('window', globalThis as unknown as Window & typeof globalThis)
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => (k in store ? store[k] : null),
      setItem: (k: string, v: string) => {
        store[k] = v
      },
      removeItem: (k: string) => {
        delete store[k]
      },
      clear: () => {
        store = {}
      },
      key: (i: number) => Object.keys(store)[i] ?? null,
      get length() {
        return Object.keys(store).length
      },
    })
    vi.stubEnv('VITE_DISABLE_TELEMETRY', '')
  })

  afterEach(() => {
    vi.unstubAllEnvs()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('does not set image src when telemetry is disabled via env', async () => {
    vi.stubEnv('VITE_DISABLE_TELEMETRY', '1')
    const ctor = vi.fn()
    vi.stubGlobal(
      'Image',
      class {
        src = ''
        decoding = ''
        referrerPolicy = ''
        constructor() {
          ctor()
        }
      },
    )
    const { trackUsageBeaconPixel } = await import('./telemetry')
    trackUsageBeaconPixel()
    expect(ctor).not.toHaveBeenCalled()
  })

  it('does not set image src when telemetry is disabled via localStorage', async () => {
    localStorage.setItem('QUANTMESH_DISABLE_TELEMETRY', '1')
    const ctor = vi.fn()
    vi.stubGlobal(
      'Image',
      class {
        src = ''
        decoding = ''
        referrerPolicy = ''
        constructor() {
          ctor()
        }
      },
    )
    const { trackUsageBeaconPixel } = await import('./telemetry')
    trackUsageBeaconPixel()
    expect(ctor).not.toHaveBeenCalled()
  })

  it('loads beacon URL when telemetry is allowed', async () => {
    let lastSrc = ''
    vi.stubGlobal(
      'Image',
      class {
        decoding = ''
        referrerPolicy = ''
        set src(v: string) {
          lastSrc = v
        }
        get src() {
          return lastSrc
        }
      },
    )
    const { trackUsageBeaconPixel, USAGE_BEACON_URL } = await import('./telemetry')
    trackUsageBeaconPixel()
    expect(lastSrc).toBe(USAGE_BEACON_URL)
  })
})
