import { afterEach, describe, expect, it, vi } from 'vitest'
vi.mock('./apiTransport', () => ({
  API_BASE_URL: 'http://localhost/api',
  fetchWithAuth: async (url: string, options: RequestInit = {}) => {
    const result = await globalThis.fetch(url, options)
    if (!result.ok) {
      const error = new Error(`HTTP ${result.status}`) as Error & { status: number; responseBody: unknown }
      error.status = result.status
      error.responseBody = await result.json()
      throw error
    }
    return result.json()
  },
}))
import {
  canManuallyReconcile, canReleaseOrderQuarantine, createOrderReconciliation,
  listOrderReconciliations, reconcileOrderAccounting, releaseOrderQuarantine,
  type OrderReconciliationCase,
} from './orderReconciliation'

const owner = {
  exchange: 'binance', market: 'futures', account_scope: 'scope-1', bot: 'bot-1', symbol: 'BTCUSDT',
  strategy_name: 'primary', strategy_type: 'grid', client_order_id: 'client-1', venue_order_id: 'venue-1',
}
const validation = {
  owner_matched: true, intent_revision_matched: true, venue_terminal: true, fills_complete: true, fees_complete: true,
  strategy_readback: true, gate_bridge_available: true, evidence_matches_case: true,
  release_retryable: false, release_ready: false, blockers: [],
}
const preparedCase: OrderReconciliationCase = {
  case_id: 'case-1', idempotency_key: 'create-key', owner, expected_intent_revision: 'intent-revision-3', expected_strategy_revision: 'strategy-revision-7',
  reason: 'Recorded fills differ', evidence_refs: ['exchange statement #42'], status: 'prepared', revision: 2,
  strategy_cursor: { owner, revision: 'strategy-revision-7', cursor: { sequence: 3, trade_id: 'trade-3' }, result: { net_quantity: '0.1', realized_pnl: [] }, read_at: '2026-10-10T00:01:00Z' },
  validation,
  created_by: 'admin', created_at: '2026-10-10T00:00:00Z', updated_at: '2026-10-10T00:01:00Z',
}
const readyCase: OrderReconciliationCase = {
  ...preparedCase, status: 'ready_to_release', revision: 4, release_evidence_hash: 'a'.repeat(64),
  validation: { ...validation, release_ready: true },
  receipt: {
    outcome: 'reconciled', operation_id: 'case-1', owner, expected_strategy_revision: 'strategy-revision-7', from: { sequence: 3, trade_id: 'trade-3' },
    target: { sequence: 4, trade_id: 'trade-4' }, revision: 'revision-4',
    result: { net_quantity: '0.1', realized_pnl: [{ asset: 'USDT', amount: '0.12' }] }, completed_at: '2026-10-10T00:02:00Z',
  },
}

function response(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

afterEach(() => vi.unstubAllGlobals())

describe('订单核账 API 契约与操作门禁', () => {
  it('创建请求分别提交 intent 和 strategy revision，不接受成交列表', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response({ case: preparedCase, status: 'prepared', evidence_verified: false }, 201))
    vi.stubGlobal('fetch', fetchMock)
    const result = await createOrderReconciliation({
      owner, expected_intent_revision: 'intent-revision-3', expected_strategy_revision: 'strategy-revision-7', reason: 'Recorded fills differ',
      evidence_refs: ['exchange statement #42'], idempotency_key: 'create-key',
    })
    expect(result.case_id).toBe('case-1')
    expect(fetchMock).toHaveBeenCalledWith(expect.stringMatching(/\/api\/capital\/order-reconciliations$/), expect.objectContaining({ method: 'POST' }))
    const body = JSON.parse(fetchMock.mock.calls[0][1].body)
    expect(body).toEqual({ owner, expected_intent_revision: 'intent-revision-3', expected_strategy_revision: 'strategy-revision-7', reason: 'Recorded fills differ', evidence_refs: ['exchange statement #42'], idempotency_key: 'create-key' })
    expect(body).not.toHaveProperty('fills')
    expect(body).not.toHaveProperty('fees')
  })

  it('人工 reconcile 和 release 使用独立端点与服务端 revision/evidence 凭证', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(response({ case: readyCase, status: 'ready_to_release', release_requires_confirmation: true }))
      .mockResolvedValueOnce(response({ case: { ...readyCase, status: 'released', revision: 5 }, status: 'released' }))
    vi.stubGlobal('fetch', fetchMock)
    const reconciled = await reconcileOrderAccounting(preparedCase)
    await releaseOrderQuarantine(reconciled)
    expect(fetchMock.mock.calls[0][0]).toMatch(/\/case-1\/reconcile$/)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ case_revision: 2, confirm: true })
    expect(fetchMock.mock.calls[1][0]).toMatch(/\/case-1\/release$/)
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ case_revision: 4, evidence_hash: 'a'.repeat(64), confirm: true })
  })

  it('API 未接线的 404、畸形响应、未验证状态均不得形成成功态', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response({ error: 'not_found' }, 404)))
    await expect(listOrderReconciliations()).rejects.toMatchObject({ status: 404 })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response({ cases: [{ ...preparedCase, owner: { strategy_id: 'wrong' } }] })))
    await expect(listOrderReconciliations()).rejects.toThrow('order_reconciliation_contract_invalid')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response({ case: { ...readyCase, release_evidence_hash: undefined }, status: 'ready_to_release' })))
    await expect(reconcileOrderAccounting(preparedCase)).rejects.toThrow('order_reconciliation_release_evidence_missing')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response({ case: preparedCase, status: 'prepared' }, 409)))
    await expect(releaseOrderQuarantine(readyCase)).rejects.toMatchObject({ status: 409 })
  })

  it('仅允许证据和两类 revision 均匹配的 prepared/reconciling 重试，release 依服务端状态门禁', () => {
    expect(canManuallyReconcile(preparedCase)).toBe(true)
    expect(canManuallyReconcile({ ...preparedCase, status: 'reconciling' })).toBe(true)
    expect(canManuallyReconcile({ ...preparedCase, status: 'confirmed' })).toBe(false)
    expect(canManuallyReconcile({ ...preparedCase, status: 'unverified' })).toBe(false)
    expect(canManuallyReconcile({ ...preparedCase, expected_intent_revision: '' })).toBe(false)
    expect(canManuallyReconcile({ ...preparedCase, validation: { ...validation, intent_revision_matched: false } })).toBe(false)
    expect(canReleaseOrderQuarantine(readyCase)).toBe(true)
    expect(canReleaseOrderQuarantine({ ...readyCase, status: 'release_pending' })).toBe(false)
    expect(canReleaseOrderQuarantine({ ...readyCase, status: 'release_pending', validation: { ...validation, release_retryable: true } })).toBe(true)
    expect(canReleaseOrderQuarantine({ ...readyCase, status: 'reconciling' })).toBe(false)
    expect(canReleaseOrderQuarantine({ ...readyCase, release_evidence_hash: undefined })).toBe(false)
    expect(canReleaseOrderQuarantine({ ...readyCase, receipt: undefined })).toBe(false)
  })
})
