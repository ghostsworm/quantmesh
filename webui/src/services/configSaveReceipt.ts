import { fetchWithAuth } from './apiTransport'

export type ConfigSaveReceipt = {
  saved: boolean
  state: 'applied' | 'failed' | 'unverified' | 'restartRequired' | 'notRunning'
  applied: string[]
  requires_restart: boolean
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function readConfigSaveReceipt(body: unknown): ConfigSaveReceipt {
  const receipt: ConfigSaveReceipt = { saved: false, state: 'unverified', applied: [], requires_restart: false }
  if (!record(body) || body.config_saved !== true) return receipt
  receipt.saved = true
  receipt.requires_restart = body.requires_restart === true
  if (body.hot_reload_failed === true || body.error === 'runtime_configuration_apply_failed') {
    receipt.state = 'failed'
    return receipt
  }
  const report = body.runtime_update
  if (!record(report) || report.verified !== true || !record(report.failed)
    || !Array.isArray(report.applied) || !Array.isArray(report.not_running)) return receipt
  const ids: unknown[] = [...report.applied, ...report.not_running, ...Object.keys(report.failed)]
  if (ids.some(id => typeof id !== 'string' || !id.trim()) || new Set(ids).size !== ids.length
    || Object.values(report.failed).some(code => typeof code !== 'string' || !code.trim())) return receipt
  if (Object.keys(report.failed).length > 0) {
    receipt.state = 'failed'
    return receipt
  }
  // A trading-parameter report does not prove that every global setting was applied.
  if (body.ok !== true || typeof body.requires_restart !== 'boolean') return receipt
  receipt.applied = report.applied as string[]
  if (receipt.requires_restart) receipt.state = 'restartRequired'
  else if (report.not_running.length > 0 || receipt.applied.length === 0) receipt.state = 'notRunning'
  else receipt.state = 'applied'
  return receipt
}

export async function saveConfigWithReceipt(url: string, options: RequestInit): Promise<ConfigSaveReceipt> {
  try {
    return readConfigSaveReceipt(await fetchWithAuth(url, options))
  } catch (error: unknown) {
    if (record(error) && error.status === 409 && record(error.responseBody)
      && error.responseBody.config_saved === true
      && error.responseBody.error === 'runtime_configuration_apply_failed') {
      return readConfigSaveReceipt(error.responseBody)
    }
    throw error
  }
}

export function configReceiptFeedback(receipt: ConfigSaveReceipt) {
  return {
    title: `configSave.${receipt.saved ? receipt.state : 'unknownSave'}`,
    status: receipt.saved && receipt.state === 'applied' ? 'success' as const : 'warning' as const,
  }
}

export async function startAfterVerifiedConfigSave(receipt: ConfigSaveReceipt, targetBotId: string | undefined, start: () => Promise<unknown>): Promise<boolean> {
  if (!targetBotId || !receipt.saved || receipt.state !== 'applied' || receipt.requires_restart
    || !receipt.applied.includes(targetBotId)) return false
  await start()
  return true
}
