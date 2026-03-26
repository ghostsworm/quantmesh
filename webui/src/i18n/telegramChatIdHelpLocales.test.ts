import { describe, expect, it } from 'vitest'

const localeModules = import.meta.glob<{ default: Record<string, unknown> }>('./locales/*.json', {
  eager: true,
})

describe('configuration.telegramChatIdHelp', () => {
  it('is defined and non-empty in every locale bundle', () => {
    for (const path of Object.keys(localeModules)) {
      const bundle = localeModules[path].default as { configuration?: { telegramChatIdHelp?: string } }
      const help = bundle.configuration?.telegramChatIdHelp
      expect(help, path).toBeDefined()
      expect(typeof help, path).toBe('string')
      expect(help!.length, path).toBeGreaterThan(20)
      expect(help, path).toContain('getUpdates')
    }
  })
})
