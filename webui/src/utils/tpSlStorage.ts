import type { TpSlRule } from '../components/TpSlRulesModal'

export function storageKey(exchange: string, symbol: string): string {
  return `tp_sl_rules_${exchange}_${symbol}`
}

export function loadRules(exchange: string, symbol: string): TpSlRule[] {
  try {
    const raw = localStorage.getItem(storageKey(exchange, symbol))
    if (!raw) return []
    return JSON.parse(raw) as TpSlRule[]
  } catch {
    return []
  }
}

export function saveRules(exchange: string, symbol: string, rules: TpSlRule[]): void {
  localStorage.setItem(storageKey(exchange, symbol), JSON.stringify(rules))
}
