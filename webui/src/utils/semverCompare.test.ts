import { describe, it, expect } from 'vitest'
import { compareSemver, isRemoteNewerThanCurrent } from './semverCompare'

describe('compareSemver', () => {
  it('handles v prefix', () => {
    expect(compareSemver('v3.89.0', '3.89.0')).toBe(0)
    expect(compareSemver('v3.90.0', '3.89.0')).toBeGreaterThan(0)
  })

  it('compares major.minor.patch', () => {
    expect(compareSemver('3.89.0', '3.90.0')).toBeLessThan(0)
    expect(compareSemver('3.90.0', '3.89.0')).toBeGreaterThan(0)
  })

  it('release is newer than prerelease of same core', () => {
    expect(compareSemver('3.89.0', '3.89.0-rc2')).toBeGreaterThan(0)
    expect(compareSemver('3.89.0-rc2', '3.89.0')).toBeLessThan(0)
  })

  it('isRemoteNewerThanCurrent', () => {
    expect(isRemoteNewerThanCurrent('3.88.0', '3.89.0')).toBe(true)
    expect(isRemoteNewerThanCurrent('3.89.0', '3.89.0')).toBe(false)
    expect(isRemoteNewerThanCurrent('3.90.0', '3.89.0')).toBe(false)
  })
})
