import { describe, it, expect, vi } from 'vitest'
import { displayPauseOpeningReason } from './botPauseReason'

describe('displayPauseOpeningReason', () => {
  it('maps known code via i18n', () => {
    const t = vi.fn((k: string) => (k === 'botRiskControl.pauseReason.manual' ? 'Manual pause' : k))
    expect(displayPauseOpeningReason('manual', t as any)).toBe('Manual pause')
  })

  it('maps Chinese alias to code', () => {
    const t = vi.fn((k: string) => (k === 'botRiskControl.pauseReason.manual' ? 'Paused manually' : k))
    expect(displayPauseOpeningReason('手动暂停', t as any)).toBe('Paused manually')
  })

  it('returns raw when no translation', () => {
    const t = vi.fn((k: string) => k)
    expect(displayPauseOpeningReason('custom_backend_note', t as any)).toBe('custom_backend_note')
  })
})
