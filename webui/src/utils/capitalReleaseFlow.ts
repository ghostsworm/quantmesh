import type { CapitalReleaseSummary } from '../services/strategyCapitalApi'

export type CapitalReleaseNotice = {
  titleKey: string
  descriptionKey: string
  status: 'success' | 'warning' | 'error'
  amount?: string
}

export async function runCapitalRelease(
  request: () => Promise<CapitalReleaseSummary>,
  refresh: () => Promise<void>,
  notify: (notice: CapitalReleaseNotice) => void,
): Promise<void> {
  try {
    const result = await request()
    const outcome = result.success ? 'success' : result.partial ? 'partial' : 'blocked'
    notify({
      titleKey: `capitalRelease.${outcome}Title`, descriptionKey: `capitalRelease.${outcome}Detail`,
      status: result.success ? 'success' : result.partial ? 'warning' : 'error',
      amount: result.total_released > 0 && result.total_released < 0.01
        ? result.total_released.toString()
        : result.total_released.toFixed(2),
    })
  } catch {
    notify({ titleKey: 'capitalRelease.unknownTitle', descriptionKey: 'capitalRelease.unknownDetail', status: 'error' })
  } finally {
    // Even an interrupted response can have committed one or more runtimes.
    try {
      await refresh()
    } catch {
      notify({ titleKey: 'capitalRelease.refreshTitle', descriptionKey: 'capitalRelease.refreshDetail', status: 'warning' })
    }
  }
}
