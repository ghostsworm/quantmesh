import { API_BASE_URL, fetchWithAuth } from './apiTransport'

export type CapitalReleaseSummary = {
  success: boolean
  total_released: number
  partial: boolean
  requires_reconciliation: boolean
  message: string
}
export type ReleaseCapitalResponse = CapitalReleaseSummary & { released: number; strategy?: string }
export type ReleaseAllCapitalResponse = CapitalReleaseSummary & { released: Record<string, number> }

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)
const isAmount = (value: unknown): value is number =>
  typeof value === 'number' && Number.isFinite(value) && value >= 0

function decode(body: unknown, all: false): ReleaseCapitalResponse
function decode(body: unknown, all: true): ReleaseAllCapitalResponse
function decode(body: unknown, all: boolean): ReleaseCapitalResponse | ReleaseAllCapitalResponse {
  if (!isRecord(body) || typeof body.success !== 'boolean') throw new Error('capital_release_contract_invalid')
  if (!body.success && (body.error !== 'capital_release_unverified' || body.requires_reconciliation !== true)) {
    throw new Error('capital_release_contract_invalid')
  }
  let released: number | Record<string, number>
  let total: number
  if (all) {
    if (!isRecord(body.released)) throw new Error('capital_release_contract_invalid')
    const entries = Object.entries(body.released)
    if (!entries.every(([, amount]) => isAmount(amount))) throw new Error('capital_release_contract_invalid')
    released = Object.fromEntries(entries) as Record<string, number>
    total = Object.values(released).reduce((sum, amount) => sum + amount, 0)
    if (!isAmount(body.total_released) || !isAmount(total) || Math.abs(body.total_released - total) > Number.EPSILON * Math.max(1, total) * Math.max(1, entries.length)) {
      throw new Error('capital_release_contract_invalid')
    }
  } else {
    if (!isAmount(body.released)) throw new Error('capital_release_contract_invalid')
    released = total = body.released
    if (!body.success && body.total_released !== total) throw new Error('capital_release_contract_invalid')
  }
  if (!body.success && body.partial !== (total > 0)) throw new Error('capital_release_contract_invalid')
  const summary = {
    success: body.success, total_released: total, partial: !body.success && total > 0,
    requires_reconciliation: !body.success, message: typeof body.message === 'string' ? body.message : '',
  }
  return typeof released === 'number'
    ? { ...summary, released, strategy: typeof body.strategy === 'string' ? body.strategy : undefined }
    : { ...summary, released }
}

async function requestRelease(path: string): Promise<unknown> {
  try {
    return await fetchWithAuth(`${API_BASE_URL}${path}`, { method: 'POST' })
  } catch (error) {
    // Only the explicit verified conflict contract is a known partial outcome.
    // Network/500/malformed responses remain uncertain, never "released zero".
    if (isRecord(error) && error.status === 409 && isRecord(error.responseBody) && error.responseBody.error === 'capital_release_unverified' && error.responseBody.success === false) {
      return error.responseBody
    }
    throw error
  }
}

export async function releaseStrategyCapital(strategyName: string): Promise<ReleaseCapitalResponse> {
  return decode(await requestRelease(`/strategies/${encodeURIComponent(strategyName)}/release-capital`), false)
}
export async function releaseAllStrategiesCapital(): Promise<ReleaseAllCapitalResponse> {
  return decode(await requestRelease('/strategies/release-all-capital'), true)
}
