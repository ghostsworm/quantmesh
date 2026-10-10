import { API_BASE_URL, fetchWithAuth } from './apiTransport'

export type OrderReconciliationOwner = {
  exchange: string
  market: string
  account_scope: string
  bot: string
  symbol: string
  strategy_name: string
  strategy_type: string
  client_order_id: string
  venue_order_id: string
}

export type ReconciliationCursor = { sequence: number; trade_id?: string }
export type ReconciliationEconomicResult = { net_quantity: string; realized_pnl: Array<{ asset: string; amount: string }> }
export type ReconciliationReceipt = {
  outcome: 'reconciled' | 'already_accounted'
  operation_id: string
  owner: OrderReconciliationOwner
  expected_strategy_revision: string
  from: ReconciliationCursor
  target: ReconciliationCursor
  revision: string
  result: ReconciliationEconomicResult
  completed_at: string
}
export type ReconciliationValidation = {
  owner_matched: boolean
  intent_revision_matched: boolean
  venue_terminal: boolean
  fills_complete: boolean
  fees_complete: boolean
  strategy_readback: boolean
  gate_bridge_available: boolean
  evidence_matches_case: boolean
  release_retryable: boolean
  release_ready: boolean
  blockers: string[]
}
export type StrategyAccountingSnapshot = {
  owner: OrderReconciliationOwner
  revision: string
  cursor: ReconciliationCursor
  result: ReconciliationEconomicResult
  read_at: string
}

export type OrderReconciliationCase = {
  case_id: string
  idempotency_key: string
  owner: OrderReconciliationOwner
  expected_intent_revision: string
  expected_strategy_revision: string
  reason: string
  evidence_refs: string[]
  status: 'prepared' | 'confirmed' | 'reconciling' | 'ready_to_release' | 'release_pending' | 'released' | 'unverified' | string
  revision: number
  receipt?: ReconciliationReceipt
  strategy_cursor: StrategyAccountingSnapshot | null
  validation: ReconciliationValidation
  release_evidence_hash?: string
  created_by: string
  created_at: string
  updated_at: string
}

export type CreateOrderReconciliation = {
  owner: OrderReconciliationOwner
  expected_intent_revision: string
  expected_strategy_revision: string
  reason: string
  evidence_refs: string[]
  idempotency_key: string
}

const basePath = `${API_BASE_URL}/capital/order-reconciliations`

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
function isNonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && value.trim().length > 0
}
function decodeOwner(value: unknown): OrderReconciliationOwner {
  if (!isRecord(value)) throw new Error('order_reconciliation_contract_invalid')
  const keys: (keyof OrderReconciliationOwner)[] = [
    'exchange', 'market', 'account_scope', 'bot', 'symbol', 'strategy_name', 'strategy_type', 'client_order_id', 'venue_order_id',
  ]
  if (!keys.every((key) => isNonEmptyString(value[key]))) throw new Error('order_reconciliation_contract_invalid')
  return Object.fromEntries(keys.map((key) => [key, value[key]])) as OrderReconciliationOwner
}
function decodeCursor(value: unknown): ReconciliationCursor {
  if (!isRecord(value) || !Number.isSafeInteger(value.sequence) || (value.sequence as number) < 0
    || (value.trade_id !== undefined && typeof value.trade_id !== 'string')) {
    throw new Error('order_reconciliation_contract_invalid')
  }
  return { sequence: value.sequence as number, trade_id: value.trade_id as string | undefined }
}
function decodeEconomicResult(value: unknown): ReconciliationEconomicResult {
  if (!isRecord(value) || !isNonEmptyString(value.net_quantity) || !Array.isArray(value.realized_pnl)
    || !value.realized_pnl.every((amount) => isRecord(amount) && isNonEmptyString(amount.asset) && isNonEmptyString(amount.amount))) {
    throw new Error('order_reconciliation_contract_invalid')
  }
  return value as ReconciliationEconomicResult
}
function decodeValidation(value: unknown): ReconciliationValidation {
  const keys: (keyof Omit<ReconciliationValidation, 'blockers'>)[] = [
    'owner_matched', 'intent_revision_matched', 'venue_terminal', 'fills_complete', 'fees_complete',
    'strategy_readback', 'gate_bridge_available', 'evidence_matches_case', 'release_retryable', 'release_ready',
  ]
  if (!isRecord(value) || !keys.every((key) => typeof value[key] === 'boolean')
    || !Array.isArray(value.blockers) || !value.blockers.every(isNonEmptyString)) {
    throw new Error('order_reconciliation_contract_invalid')
  }
  return { ...Object.fromEntries(keys.map((key) => [key, value[key]])), blockers: value.blockers } as ReconciliationValidation
}
function decodeSnapshot(value: unknown, owner: OrderReconciliationOwner): StrategyAccountingSnapshot {
  if (!isRecord(value) || !isNonEmptyString(value.revision) || !isNonEmptyString(value.read_at)) throw new Error('order_reconciliation_contract_invalid')
  const snapshotOwner = decodeOwner(value.owner)
  if (JSON.stringify(snapshotOwner) !== JSON.stringify(owner)) throw new Error('order_reconciliation_contract_invalid')
  return { owner: snapshotOwner, revision: value.revision, cursor: decodeCursor(value.cursor), result: decodeEconomicResult(value.result), read_at: value.read_at }
}
function decodeReceipt(value: unknown, owner: OrderReconciliationOwner): ReconciliationReceipt {
  if (!isRecord(value) || (value.outcome !== 'reconciled' && value.outcome !== 'already_accounted')
    || !isNonEmptyString(value.operation_id) || !isNonEmptyString(value.expected_strategy_revision)
    || !isNonEmptyString(value.revision) || !isNonEmptyString(value.completed_at)) {
    throw new Error('order_reconciliation_contract_invalid')
  }
  const receiptOwner = decodeOwner(value.owner)
  if (JSON.stringify(receiptOwner) !== JSON.stringify(owner)) throw new Error('order_reconciliation_contract_invalid')
  return {
    outcome: value.outcome,
    operation_id: value.operation_id,
    owner: receiptOwner,
    expected_strategy_revision: value.expected_strategy_revision,
    from: decodeCursor(value.from),
    target: decodeCursor(value.target),
    revision: value.revision,
    result: decodeEconomicResult(value.result),
    completed_at: value.completed_at,
  }
}
function decodeCase(value: unknown): OrderReconciliationCase {
  if (!isRecord(value) || !isNonEmptyString(value.case_id) || !isNonEmptyString(value.idempotency_key)
    || typeof value.expected_intent_revision !== 'string' || typeof value.expected_strategy_revision !== 'string'
    || !isNonEmptyString(value.reason) || !Array.isArray(value.evidence_refs) || !value.evidence_refs.every(isNonEmptyString)
    || !isNonEmptyString(value.status) || !Number.isSafeInteger(value.revision) || (value.revision as number) < 1
    || !isNonEmptyString(value.created_by) || !isNonEmptyString(value.created_at) || !isNonEmptyString(value.updated_at)) {
    throw new Error('order_reconciliation_contract_invalid')
  }
  const owner = decodeOwner(value.owner)
  if (value.release_evidence_hash !== undefined && !isNonEmptyString(value.release_evidence_hash)) throw new Error('order_reconciliation_contract_invalid')
  const receipt = value.receipt === undefined ? undefined : decodeReceipt(value.receipt, owner)
  if (!Object.prototype.hasOwnProperty.call(value, 'strategy_cursor')) throw new Error('order_reconciliation_contract_invalid')
  const strategyCursor = value.strategy_cursor === null ? null : decodeSnapshot(value.strategy_cursor, owner)
  const validation = decodeValidation(value.validation)
  return {
    case_id: value.case_id, idempotency_key: value.idempotency_key, owner,
    expected_intent_revision: value.expected_intent_revision, expected_strategy_revision: value.expected_strategy_revision,
    reason: value.reason, evidence_refs: value.evidence_refs, status: value.status,
    revision: value.revision as number, receipt, strategy_cursor: strategyCursor, validation,
    release_evidence_hash: value.release_evidence_hash as string | undefined,
    created_by: value.created_by, created_at: value.created_at, updated_at: value.updated_at,
  }
}
function decodeList(value: unknown): OrderReconciliationCase[] {
  if (!isRecord(value) || !Array.isArray(value.cases)) throw new Error('order_reconciliation_contract_invalid')
  return value.cases.map(decodeCase)
}
function decodeAction(value: unknown, expectedStatus: string): OrderReconciliationCase {
  if (!isRecord(value) || value.status !== expectedStatus) throw new Error('order_reconciliation_not_verified')
  const item = decodeCase(value.case)
  if (item.status !== expectedStatus) throw new Error('order_reconciliation_not_verified')
  if (expectedStatus === 'ready_to_release' && (!item.receipt || !isNonEmptyString(item.release_evidence_hash))) {
    throw new Error('order_reconciliation_release_evidence_missing')
  }
  return item
}
function casePath(caseId: string): string {
  return `${basePath}/${encodeURIComponent(caseId)}`
}

export async function listOrderReconciliations(signal?: AbortSignal): Promise<OrderReconciliationCase[]> {
  return decodeList(await fetchWithAuth(basePath, { signal }))
}
export async function createOrderReconciliation(request: CreateOrderReconciliation): Promise<OrderReconciliationCase> {
  return decodeAction(await fetchWithAuth(basePath, { method: 'POST', body: JSON.stringify(request) }), 'prepared')
}
export async function reconcileOrderAccounting(item: OrderReconciliationCase): Promise<OrderReconciliationCase> {
  return decodeAction(await fetchWithAuth(`${casePath(item.case_id)}/reconcile`, {
    method: 'POST', body: JSON.stringify({ case_revision: item.revision, confirm: true }),
  }), 'ready_to_release')
}
export async function releaseOrderQuarantine(item: OrderReconciliationCase): Promise<OrderReconciliationCase> {
  if ((item.status !== 'ready_to_release' && item.status !== 'release_pending') || !item.release_evidence_hash || !item.receipt) {
    throw new Error('order_reconciliation_fresh_release_evidence_required')
  }
  return decodeAction(await fetchWithAuth(`${casePath(item.case_id)}/release`, {
    method: 'POST',
    body: JSON.stringify({ case_revision: item.revision, evidence_hash: item.release_evidence_hash, confirm: true }),
  }), 'released')
}

export function canManuallyReconcile(item: OrderReconciliationCase): boolean {
  const retryable = item.status === 'prepared' || item.status === 'reconciling'
  return retryable && item.revision > 0 && Boolean(item.expected_intent_revision && item.expected_strategy_revision)
    && Boolean(item.owner.venue_order_id && item.owner.client_order_id)
    && item.validation.owner_matched && item.validation.intent_revision_matched
    && item.validation.venue_terminal && item.validation.fills_complete && item.validation.fees_complete
    && item.validation.evidence_matches_case && item.validation.strategy_readback
}
export function canReleaseOrderQuarantine(item: OrderReconciliationCase): boolean {
  return (item.status === 'ready_to_release' || item.status === 'release_pending')
    && Boolean(item.release_evidence_hash) && Boolean(item.receipt)
    && item.validation.owner_matched && item.validation.intent_revision_matched
    && item.validation.strategy_readback && item.validation.evidence_matches_case
    && (item.status === 'ready_to_release' ? item.validation.release_ready : item.validation.release_retryable)
}
