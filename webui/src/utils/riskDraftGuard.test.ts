import { describe, expect, it } from 'vitest'
import { RiskDraftGuard } from './riskDraftGuard'

function loadedGuard() {
  const guard = new RiskDraftGuard()
  guard.reset('bot-a')
  const ticket = guard.beginFetch('bot-a')!
  expect(guard.acceptsDraft(ticket)).toBe(true)
  return guard
}

describe('risk draft request ownership', () => {
  it('ignores a duplicate acknowledgement after a later save starts', () => {
    const guard = loadedGuard()
    const old = guard.beginSave('bot-a')!
    guard.finishSave(old, true)
    const current = guard.beginSave('bot-a')!
    expect(guard.finishSave(old, true)).toBe(false)
    expect(guard.beginSave('bot-a')).toBeUndefined()
    expect(guard.finishSave(current, true)).toBe(true)
  })
  it('keeps dirty config while allowing fresh position status', () => {
    const guard = loadedGuard()
    expect(guard.edit('bot-a')).toBe(true)
    const poll = guard.beginFetch('bot-a')!
    expect(guard.accepts(poll)).toBe(true)
    expect(guard.acceptsDraft(poll)).toBe(false)
  })

  it('rejects out-of-order responses and old bot generations including A-B-A', () => {
    const guard = loadedGuard()
    const first = guard.beginFetch('bot-a')!
    const second = guard.beginFetch('bot-a')!
    expect(guard.accepts(first)).toBe(false)
    expect(guard.accepts(second)).toBe(true)
    guard.reset('bot-b')
    expect(guard.edit('bot-a')).toBe(false)
    expect(guard.beginSave('bot-b')).toBeUndefined()
    guard.reset('bot-a')
    expect(guard.accepts(second)).toBe(false)
  })

  it('invalidates pre-save polls and only reloads an unchanged saved draft', () => {
    const guard = loadedGuard()
    guard.edit('bot-a')
    const stale = guard.beginFetch('bot-a')!
    const save = guard.beginSave('bot-a')!
    expect(guard.beginSave('bot-a')).toBeUndefined()
    expect(guard.accepts(stale)).toBe(false)
    const duringSave = guard.beginFetch('bot-a')!
    expect(guard.acceptsDraft(duringSave)).toBe(false)
    expect(guard.finishSave(save, true)).toBe(true)
    expect(guard.accepts(duringSave)).toBe(false)
    expect(guard.acceptsDraft(guard.beginFetch('bot-a')!)).toBe(true)
  })

  it('does not clear edits made while a save is in flight', () => {
    const guard = loadedGuard()
    guard.edit('bot-a')
    const save = guard.beginSave('bot-a')!
    guard.edit('bot-a')
    guard.finishSave(save, true)
    expect(guard.acceptsDraft(guard.beginFetch('bot-a')!)).toBe(false)
  })

  it('retains failed drafts and ignores old save completions after navigation/unmount', () => {
    const guard = loadedGuard()
    guard.edit('bot-a')
    const save = guard.beginSave('bot-a')!
    guard.finishSave(save, false)
    expect(guard.acceptsDraft(guard.beginFetch('bot-a')!)).toBe(false)
    const retry = guard.beginSave('bot-a')!
    guard.reset('bot-b')
    expect(guard.finishSave(retry, true)).toBe(false)
    const poll = guard.beginFetch('bot-b')!
    guard.reset('')
    expect(guard.accepts(poll)).toBe(false)
  })
})
