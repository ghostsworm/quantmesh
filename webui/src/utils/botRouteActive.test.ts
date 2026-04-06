import { describe, it, expect } from 'vitest'
import { isBotWorkspaceRoot } from './botRouteActive'

describe('isBotWorkspaceRoot', () => {
  const prefix = '/bots/binance:btcusdt:futures'

  it('matches exact bot workspace path', () => {
    expect(isBotWorkspaceRoot(`${prefix}`, prefix)).toBe(true)
  })

  it('matches path with trailing slash', () => {
    expect(isBotWorkspaceRoot(`${prefix}/`, prefix)).toBe(true)
  })

  it('does not match dashboard', () => {
    expect(isBotWorkspaceRoot(`${prefix}/dashboard`, prefix)).toBe(false)
  })

  it('does not match other subroutes', () => {
    expect(isBotWorkspaceRoot(`${prefix}/positions`, prefix)).toBe(false)
  })
})
