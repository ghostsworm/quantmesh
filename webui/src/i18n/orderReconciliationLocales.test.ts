import { describe, expect, it } from 'vitest'
import { orderReconciliationLocaleCodes, withOrderReconciliation } from './orderReconciliation'

const localeModules = import.meta.glob<{ default: Record<string, unknown> }>('./locales/*.json', { eager: true })
const requiredKeys = ['title', 'warning', 'create', 'reconcile', 'release', 'cursorUnavailable', 'releaseConfirmDescription', 'expectedIntentRevision', 'expectedStrategyRevision', 'alreadyAccounted']

describe('所有支持语言均提供订单人工核账文案资源', () => {
  it('逐一检查项目内全部 locale，缺少翻译时使用完整英语资源而非显示 key', () => {
    expect(Object.keys(localeModules).length).toBe(orderReconciliationLocaleCodes().length)
    for (const [path, module] of Object.entries(localeModules)) {
      const language = path.match(/locales\/([^/]+)\.json$/)?.[1]
      expect(language, path).toBeTruthy()
      expect(orderReconciliationLocaleCodes()).toContain(language)
      const bundle = withOrderReconciliation(language as string, module.default)
      const configuration = bundle.configuration as Record<string, unknown>
      const panel = configuration.orderReconciliation as Record<string, unknown>
      for (const key of requiredKeys) expect(typeof panel[key], `${language}: ${key}`).toBe('string')
      expect(panel.statuses, `${language}: statuses`).toBeTruthy()
    }
  })
})
