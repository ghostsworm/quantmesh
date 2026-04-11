import { describe, it, expect } from 'vitest'
import { useBlocker } from 'react-router-dom'

/**
 * useBlocker 依赖 DataRouterContext；BrowserRouter 不提供该上下文。
 * 配置页等不得在 BrowserRouter 下调用 useBlocker，否则会运行时抛错白屏。
 */
describe('react-router data API 前提', () => {
  it('useBlocker 存在但仅能在 RouterProvider(data router) 下调用', () => {
    expect(typeof useBlocker).toBe('function')
  })
})
