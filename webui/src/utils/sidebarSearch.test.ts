import { describe, expect, it } from 'vitest'
import { filterSidebarItems, SidebarSearchItem } from './sidebarSearch'

const items: SidebarSearchItem[] = [
  { label: 'Bot 列表', group: 'Bot 广场', to: '/bots', keywords: ['robot'] },
  { label: '资金费率', group: '市场数据', to: '/funding-rate', keywords: ['funding'] },
  { label: '个人资料', group: '系统设置', to: '/profile', keywords: ['account'] },
]

describe('filterSidebarItems', () => {
  it('matches label, group, path and keywords', () => {
    expect(filterSidebarItems(items, 'bot')).toHaveLength(1)
    expect(filterSidebarItems(items, '市场')).toHaveLength(1)
    expect(filterSidebarItems(items, '/profile')).toHaveLength(1)
    expect(filterSidebarItems(items, 'account')).toHaveLength(1)
  })

  it('returns empty results for blank queries', () => {
    expect(filterSidebarItems(items, '   ')).toEqual([])
  })
})
