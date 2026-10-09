function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function pendingOrderIDsForMutation(value: unknown): number[] {
  if (!record(value) || !Array.isArray(value.orders)) throw new Error('configuration_preflight_unverified')
  const ids: number[] = []
  for (const order of value.orders) {
    if (!record(order) || !['PLACED', 'CONFIRMED', 'PARTIALLY_FILLED'].includes(String(order.status))
      || typeof order.order_id !== 'number' || !Number.isSafeInteger(order.order_id) || order.order_id <= 0
      || ids.includes(order.order_id)) throw new Error('configuration_preflight_unverified')
    ids.push(order.order_id)
  }
  return ids
}

// Request acceptance is not complete cancellation or proof of flat inventory.
export function requireCancelCompletion(value: unknown, requestedCount: number) {
  if (!record(value) || value.success !== true || value.count !== requestedCount) {
    throw new Error('configuration_preflight_unverified')
  }
}

export function requireCloseCompletion(value: unknown) {
  if (!record(value) || !Number.isSafeInteger(value.success_count) || Number(value.success_count) < 0
    || value.fail_count !== 0) throw new Error('configuration_preflight_unverified')
}
