const validCurrencyCode = /^[A-Za-z0-9]{1,16}$/

export function formatWithdrawAmount(amount: number, currency?: string): string {
  if (!Number.isFinite(amount)) return '—'

  const normalizedCurrency = currency?.trim()
  const unit = normalizedCurrency && validCurrencyCode.test(normalizedCurrency)
    ? normalizedCurrency.toUpperCase()
    : '—'

  return `${amount.toFixed(2)} ${unit}`
}
