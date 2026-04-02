import { describe, it, expect, vi, afterEach } from 'vitest'
import { downloadMediaFromUrl } from './mediaUtils'

describe('downloadMediaFromUrl', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('在 HTTP 非 2xx 时返回 ok: false', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
      })
    )
    const result = await downloadMediaFromUrl('https://example.com/missing', 'x.bin')
    expect(result).toEqual({ ok: false, error: 'HTTP 404' })
  })

  it('在 fetch 抛错时返回 ok: false', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network')))
    const result = await downloadMediaFromUrl('https://example.com/a', 'a.mp4')
    expect(result.ok).toBe(false)
    if (!result.ok) {
      expect(result.error).toContain('network')
    }
  })
})
