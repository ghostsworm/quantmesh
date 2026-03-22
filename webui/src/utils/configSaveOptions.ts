import type { ConfigDiff } from '../services/config'

/**
 * 判断配置变更是否涉及交易参数（网格、方向、手数等）。
 * 仅当变更路径以 trading. 开头时，才需要在保存前弹「取消委托/平仓」选项。
 * 全局配置（exchanges.*、app、ai 等）修改时无需弹框。
 */
export function hasTradingParamChanges(diff: ConfigDiff): boolean {
  return diff.changes.some((c) => (c.path || '').startsWith('trading.'))
}
