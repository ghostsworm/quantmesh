import { fetchWithAuth } from './apiTransport'

export type StrategySaveReceipt = {
  saved: boolean
  state: 'applied' | 'failed' | 'notRunning' | 'unverified'
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function readStrategySaveReceipt(body: unknown, botId: string): StrategySaveReceipt {
  const unknown: StrategySaveReceipt = { saved: false, state: 'unverified' }
  if (!record(body) || body.bot_id !== botId || body.config_saved !== true) return unknown
  const saved: StrategySaveReceipt = { saved: true, state: 'unverified' }
  const report = body.runtime_update
  if (!record(report) || typeof report.verified !== 'boolean' || !record(report.failed)) return saved
  const applied = report.applied
  const stopped = report.not_running
  if (!Array.isArray(applied) || !Array.isArray(stopped)) return saved
  const ids: unknown[] = [...applied, ...stopped, ...Object.keys(report.failed)]
  if (ids.some(id => typeof id !== 'string' || !id.trim()) || new Set(ids).size !== ids.length) return saved
  if (Object.values(report.failed).some(code => typeof code !== 'string' || !code.trim())) return saved
  if (Object.keys(report.failed).length > 0) return { saved: true, state: 'failed' }
  if (!report.verified || body.ok !== true) return saved
  if (applied.includes(botId)) return { saved: true, state: 'applied' }
  if (stopped.includes(botId)) return { saved: true, state: 'notRunning' }
  return saved
}

// Only this endpoint consumes a saved-but-not-applied conflict as a receipt.
// Other conflicts, authorization failures and transport failures still reject.
export async function saveStrategyWithReceipt(url: string, botId: string, options: RequestInit): Promise<StrategySaveReceipt> {
  try {
    return readStrategySaveReceipt(await fetchWithAuth(url, options), botId)
  } catch (error: unknown) {
    if (record(error) && error.status === 409 && record(error.responseBody)
      && error.responseBody.error === 'runtime_configuration_apply_failed') {
      const receipt = readStrategySaveReceipt(error.responseBody, botId)
      if (receipt.saved) return { saved: true, state: 'failed' }
    }
    throw error
  }
}

export function strategyReceiptFeedback(receipt: StrategySaveReceipt) {
  return {
    title: `strategySave.${receipt.saved ? receipt.state : 'unknownSave'}`,
    status: receipt.state === 'applied' && receipt.saved ? 'success' as const : 'warning' as const,
  }
}
