import { createInstance } from 'i18next'
import { describe, expect, it, vi } from 'vitest'
import { runCapitalRelease, type CapitalReleaseNotice } from './capitalReleaseFlow'
import { capitalReleaseLocales, withCapitalRelease } from '../i18n/capitalRelease'

describe('capital release feedback and refresh', () => {
  it.each(['success', 'partial', 'blocked', 'interrupted'] as const)('refreshes after %s and distinguishes actual amounts', async (outcome) => {
    const events: string[] = []
    const notices: CapitalReleaseNotice[] = []
    const request = async () => {
      events.push('request')
      if (outcome === 'interrupted') throw new Error('secret backend detail')
      return { success: outcome === 'success', partial: outcome === 'partial', total_released: outcome === 'blocked' ? 0 : 200, requires_reconciliation: outcome !== 'success', message: 'secret backend detail' }
    }
    await runCapitalRelease(request, async () => { events.push('refresh') }, (notice) => { notices.push(notice); events.push('notice') })
    expect(events).toEqual(['request', 'notice', 'refresh'])
    const name = outcome === 'interrupted' ? 'unknown' : outcome
    expect(notices[0].titleKey).toBe(`capitalRelease.${name}Title`)
    if (outcome === 'partial') expect(notices[0]).toMatchObject({ status: 'warning', amount: '200.00' })
    const i18n = createInstance()
    await i18n.init({ lng: 'zh-CN', resources: { 'zh-CN': { translation: withCapitalRelease('zh-CN', {}) } } })
    const displayed = i18n.t(notices[0].descriptionKey, { amount: notices[0].amount })
    expect(displayed).not.toContain('secret backend detail')
    if (outcome === 'partial') expect(displayed).toContain('已释放 200.00 USDT')
    if (outcome === 'interrupted') { expect(displayed).toContain('可能已有部分资金释放'); expect(displayed).not.toContain('未释放资金') }
  })
  it('reports stale allocation when refresh fails, without erasing the actual release', async () => {
    const notices: CapitalReleaseNotice[] = []
    const refresh = vi.fn().mockRejectedValue(new Error('refresh failed'))
    await runCapitalRelease(async () => ({ success: false, partial: true, total_released: 200, requires_reconciliation: true, message: '' }), refresh, (notice) => notices.push(notice))
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(notices.map((notice) => notice.titleKey)).toEqual(['capitalRelease.partialTitle', 'capitalRelease.refreshTitle'])
  })
  it('does not round a confirmed tiny partial release into a zero-release message', async () => {
    const notices: CapitalReleaseNotice[] = []
    await runCapitalRelease(async () => ({ success: false, partial: true, total_released: 0.0004, requires_reconciliation: true, message: '' }), async () => {}, (notice) => notices.push(notice))
    expect(notices[0]).toMatchObject({ amount: '0.0004', status: 'warning', titleKey: 'capitalRelease.partialTitle' })
  })
  it('does not finish the flow until the allocation refresh has settled', async () => {
    let finishRefresh: () => void = () => {}
    let complete = false
    const pendingRefresh = new Promise<void>((resolve) => { finishRefresh = resolve })
    const flow = runCapitalRelease(async () => ({ success: true, partial: false, total_released: 200, requires_reconciliation: false, message: '' }), () => pendingRefresh, () => {}).then(() => { complete = true })
    await Promise.resolve()
    expect(complete).toBe(false)
    finishRefresh()
    await flow
    expect(complete).toBe(true)
  })
  it('provides complete locale keys and an explicit English fallback', () => {
    for (const resource of Object.values(capitalReleaseLocales)) expect(Object.keys(resource).sort()).toEqual(Object.keys(capitalReleaseLocales['en-US']).sort())
    expect(withCapitalRelease('de-DE', {}).capitalRelease).toEqual(capitalReleaseLocales['en-US'])
    expect(withCapitalRelease('zh-TW', {}).capitalRelease.partialDetail).toContain('已釋放')
  })
})
