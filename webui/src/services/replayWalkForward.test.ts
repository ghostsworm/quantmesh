import { afterEach, describe, expect, it, vi } from 'vitest'
import { buildReplaySearchSpace, createReplayFormValues } from '../utils/replayWalkForward'

vi.mock('./api', () => ({ fetchWithAuth: vi.fn() }))
afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

describe('replay optimization API contract', () => {
  it('sends the exact persisted nested protocol through the existing optim task endpoint', async () => {
    vi.stubGlobal('window', { location: { origin: 'https://example.test' } })
    const { postOptimTask, getOptimTaskResult } = await import('./backtest')
    const { fetchWithAuth } = await import('./api')
    const request = buildReplaySearchSpace(createReplayFormValues(), '2026-01-01', '2026-01-07', '1h')
    if (!request.ok) throw new Error(request.error)
    vi.mocked(fetchWithAuth).mockResolvedValueOnce({ success: true, task_id: 'task/one' })
    await postOptimTask({ strategy: 'grid', symbol: 'BTCUSDT', interval: '1h', total_capital: 10000,
      start_time: request.startTime, end_time: request.endTime, search_space: request.searchSpace })
    expect(fetchWithAuth).toHaveBeenCalledWith('https://example.test/api/backtest/optim/tasks', expect.objectContaining({ method: 'POST' }))
    const init = vi.mocked(fetchWithAuth).mock.calls[0][1]
    const body = JSON.parse(String(init?.body)) as { search_space: Record<string, unknown> }
    expect(body.search_space).toEqual(request.searchSpace)
    expect(Object.keys(body.search_space.replay_walk_forward as object).sort()).toEqual(['base_params', 'leverage', 'step_days', 'test_days', 'train_days'])

    const response = { task_id: 'task/one', all_results: [], replay_walk_forward: { folds: [], experiment_sha256: 'frozen-hash' } }
    vi.mocked(fetchWithAuth).mockResolvedValueOnce(response)
    expect(await getOptimTaskResult('task/one')).toEqual(response)
    expect(fetchWithAuth).toHaveBeenLastCalledWith('https://example.test/api/backtest/optim/tasks/task%2Fone/result')
  })
})
