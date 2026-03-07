import { describe, expect, it } from 'vitest'

import {
  getInitialBackendProbeDelayMs,
  isAuthFlowPath,
} from './appRuntimeGuards'

describe('appRuntimeGuards', () => {
  it('treats auth and setup routes as auth flow pages', () => {
    expect(isAuthFlowPath('/login')).toBe(true)
    expect(isAuthFlowPath('/setup')).toBe(true)
    expect(isAuthFlowPath('/config-setup')).toBe(true)
    expect(isAuthFlowPath('/wizard')).toBe(true)
  })

  it('does not mark regular app routes as auth flow pages', () => {
    expect(isAuthFlowPath('/')).toBe(false)
    expect(isAuthFlowPath('/bots')).toBe(false)
    expect(isAuthFlowPath('/terms')).toBe(false)
  })

  it('adds a startup delay for backend probes on auth flow pages only', () => {
    expect(getInitialBackendProbeDelayMs('/login')).toBe(3000)
    expect(getInitialBackendProbeDelayMs('/setup')).toBe(3000)
    expect(getInitialBackendProbeDelayMs('/bots')).toBe(0)
  })
})
