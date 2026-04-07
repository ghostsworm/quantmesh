import { describe, expect, it } from 'vitest'
import enUS from './locales/en-US.json'
import zhCN from './locales/zh-CN.json'
import zhTW from './locales/zh-TW.json'

describe('configuration.singleBotGlobalHint', () => {
  it('includes configLink placeholder for Trans in primary locales', () => {
    for (const bundle of [enUS, zhCN, zhTW]) {
      const hint = bundle.configuration.singleBotGlobalHint
      expect(hint).toBeDefined()
      expect(hint).toContain('<configLink>')
      expect(hint).toContain('</configLink>')
    }
  })
})

describe('botRiskControl.globalMarketRiskHint', () => {
  it('includes configLink placeholder for Trans in primary locales', () => {
    for (const bundle of [enUS, zhCN, zhTW]) {
      const hint = bundle.botRiskControl.globalMarketRiskHint
      expect(hint).toBeDefined()
      expect(hint).toContain('<configLink>')
      expect(hint).toContain('</configLink>')
    }
  })
})
