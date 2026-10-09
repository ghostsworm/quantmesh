import { describe, expect, it } from 'vitest'
import { pendingOrderIDsForMutation, requireCancelCompletion, requireCloseCompletion } from './configMutationPreflight'

describe('configuration mutation financial preflight', () => {
  it('preserves explicitly known empty and valid pending IDs', () => {
    expect(pendingOrderIDsForMutation({ orders: [] })).toEqual([])
    expect(pendingOrderIDsForMutation({ orders: [{ order_id: 42, status: 'PLACED' }, { order_id: 43, status: 'PARTIALLY_FILLED' }] })).toEqual([42, 43])
  })
  it.each([
    null, {}, { orders: null },
    { orders: [{ order_id: 0, status: 'UNKNOWN' }] },
    { orders: [{ order_id: 42, status: 'UNKNOWN' }] },
    { orders: [{ order_id: 42, status: 'CANCEL_REQUESTED' }] },
    { orders: [{ order_id: 0, status: 'PLACED' }] },
    { orders: [{ order_id: '42', status: 'PLACED' }] },
    { orders: [{ order_id: Number.MAX_SAFE_INTEGER + 1, status: 'PLACED' }] },
    { orders: [{ order_id: 42, status: 'PLACED' }, { order_id: 42, status: 'CONFIRMED' }] },
  ])('uncertainty cannot disappear through filtering IDs: %j', response => {
    expect(() => pendingOrderIDsForMutation(response)).toThrow('configuration_preflight_unverified')
  })
  it('accepts only a complete acknowledged cancellation count', () => {
    expect(() => requireCancelCompletion({ success: true, count: 2 }, 2)).not.toThrow()
  })
  it.each([null, {}, { success: false, count: 2 }, { success: true, count: 1 }, { success: true }, { success: true, count: '2' }])('rejects incomplete or uncertain cancellation: %j', result => {
    expect(() => requireCancelCompletion(result, 2)).toThrow('configuration_preflight_unverified')
  })
  it('accepts complete close receipt without claiming inventory is flat', () => {
    expect(() => requireCloseCompletion({ success_count: 0, fail_count: 0 })).not.toThrow()
    expect(() => requireCloseCompletion({ success_count: 2, fail_count: 0 })).not.toThrow()
  })
  it.each([null, {}, { success_count: 1, fail_count: 1 }, { success_count: 1 }, { success_count: -1, fail_count: 0 }, { success_count: 0.5, fail_count: 0 }, { success_count: 2, fail_count: '0' }])('rejects partial or malformed close receipts: %j', result => {
    expect(() => requireCloseCompletion(result)).toThrow('configuration_preflight_unverified')
  })
})
