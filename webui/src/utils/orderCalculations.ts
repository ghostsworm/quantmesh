/**
 * 订单金额计算工具
 * 总价格 = 价格 × 数量（名义价值，USDT）
 * 资金占用 = 总价格 ÷ 杠杆（实际占用的保证金）
 */
export function computeOrderTotalPrice(price: number | null | undefined, quantity: number | null | undefined): number {
  const p = price ?? 0
  const q = quantity ?? 0
  return p * q
}

export function computeOrderCapitalUsage(totalPrice: number, leverage: number): number {
  if (leverage <= 0) return totalPrice
  return totalPrice / leverage
}
