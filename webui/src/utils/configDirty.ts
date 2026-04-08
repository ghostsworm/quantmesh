import type { Config } from '../services/config'

/** 将手续费输入框中的值合并进配置副本，用于与已保存快照比较（避免未 blur 时漏检未保存）。 */
export function mergeFeeRateInputsIntoConfig(config: Config, feeRateInputs: Record<string, string>): Config {
  const cloned = JSON.parse(JSON.stringify(config)) as Config
  const exchanges = cloned.exchanges
  if (!exchanges) return cloned
  for (const ex of Object.keys(exchanges)) {
    const raw = feeRateInputs[ex]
    if (raw === undefined) continue
    const trimmed = String(raw).trim()
    const parsed = trimmed === '' ? 0 : Number(trimmed)
    if (Number.isNaN(parsed)) continue
    const slot = exchanges[ex]
    if (slot) slot.fee_rate = parsed
  }
  return cloned
}
