import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { ChakraProvider } from '@chakra-ui/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import OptimResultModal from './OptimResultModal'
import ReplayWalkForwardForm from './ReplayWalkForwardForm'
import ReplayWalkForwardResults from './ReplayWalkForwardResults'
import { replayWalkForwardLocales, withReplayWalkForward } from '../i18n/replayWalkForward'
import enUS from '../i18n/locales/en-US.json'
import type { OptimResult } from '../services/backtest'
import type { ReplayEvaluation, ReplayWalkForwardResult } from '../services/replayWalkForward'
import { buildReplaySearchSpace, createReplayFormValues } from '../utils/replayWalkForward'

// Modal portals do not render on the server. Keep the real table, controls and
// result components, replacing only portal/context wrappers for this test.
vi.mock('@chakra-ui/react', async (importOriginal) => {
  const original = await importOriginal<typeof import('@chakra-ui/react')>()
  const Wrapper = ({ children }: { children?: ReactNode }) => <div>{children}</div>
  return { ...original, Modal: Wrapper, ModalContent: Wrapper, ModalHeader: Wrapper, ModalBody: Wrapper, ModalFooter: Wrapper, ModalOverlay: () => null, ModalCloseButton: () => null }
})

function evaluation(seed: number): ReplayEvaluation {
  return { net_pnl: seed, total_return: seed / 100, max_drawdown: seed / 10, fills: seed + 2, closed_grids: seed + 1,
    fees_maker: seed / 2, fees_taker: seed / 4, fees_total: seed * 0.75, funding_paid: -seed / 5,
    unrealized_pnl: seed / 3, final_net_qty: seed / 1000, score: -seed }
}
function fixture(): ReplayWalkForwardResult {
  const built = buildReplaySearchSpace(createReplayFormValues(), '2026-01-01', '2026-01-07', '1h')
  if (!built.ok || !built.searchSpace.replay_walk_forward) throw new Error('Invalid fixture')
  const config = built.searchSpace.replay_walk_forward
  return {
    protocol: 'grid_replay_walk_forward_v1', engine: 'replay', score_rule: 'net_return_pct_minus_max_drawdown_pct',
    warmup_policy: 'closed_candles_inside_each_window_only', window_policy: 'independent_flat_start_fixed_capital_mark_to_market',
    config, config_sha256: 'config-digest', data_sha256: 'data-digest', experiment_sha256: 'experiment-digest', baseline_sha256: 'baseline-digest',
    data_bars: 168, data_start: Date.parse('2026-01-01T00:00:00Z'), data_end: Date.parse('2026-01-08T00:00:00Z'), candle_interval_ms: 3600000,
    baseline_params: config.base_params, limitations: ['no_historical_order_book', 'binance_vision_um_1m_checksum_verified'],
    data_source: 'binance_vision_um_1m',
    verified_files: [{ name: 'BTCUSDT/1m/BTCUSDT-1m-2026-01-01.csv', sha256: 'candle-file-digest' }],
    funding_files: [{ name: 'BTCUSDT/fundingRate/BTCUSDT-fundingRate-2026-01.csv', sha256: 'funding-file-digest' }],
    folds: [{ index: 0, train_start: Date.parse('2026-01-01T00:00:00Z'), train_end: Date.parse('2026-01-04T00:00:00Z'),
      test_start: Date.parse('2026-01-04T00:00:00Z'), test_end: Date.parse('2026-01-05T00:00:00Z'),
      train_data_sha256: 'train-digest', test_data_sha256: 'test-digest', params_sha256: 'params-digest',
      selected_params: { ...config.base_params, grid_spacing: 200 },
      train: evaluation(11), test: evaluation(22), baseline_train: evaluation(33), baseline_test: evaluation(44), test_excess_return: -0.22,
      candidates: [{ params: { grid_spacing: 200 }, params_sha256: 'candidate-digest', train: evaluation(11) }],
    }],
  }
}
async function render(node: ReactNode, language = 'en-US') {
  const i18n = createInstance()
  await i18n.init({ lng: language, resources: { [language]: { translation: withReplayWalkForward(language, enUS) } } })
  return renderToStaticMarkup(<ChakraProvider><I18nextProvider i18n={i18n}>{node}</I18nextProvider></ChakraProvider>)
}

describe('replay result presentation', () => {
  it('displays all four evaluations without mixing training and out-of-sample metrics', async () => {
    const html = await render(<ReplayWalkForwardResults result={fixture()} />)
    for (const label of ['Selected · training', 'Selected · out of sample', 'Baseline · training', 'Baseline · out of sample']) expect(html).toContain(label)
    const row = html.split('data-testid="replay-metric-net_pnl"')[1].split('</tr>')[0]
    expect(row).toMatch(/>11<.*>22<.*>33<.*>44</)
    for (const field of ['total_return', 'max_drawdown', 'fills', 'closed_grids', 'fees_maker', 'fees_taker', 'fees_total', 'funding_paid', 'unrealized_pnl', 'final_net_qty']) {
      expect(html).toContain(`data-testid="replay-metric-${field}"`)
    }
    expect(html).toContain('including partial fills')
    expect(html).toContain('Out-of-sample return above baseline: -0.22 percentage points')
    expect(html).not.toContain('<select')
    expect(html).toContain('Training candidates (not an out-of-sample ranking)')
  })

  it('shows frozen parameters, audit digests, and localized limitations', async () => {
    const html = await render(<ReplayWalkForwardResults result={fixture()} />, 'zh-CN')
    for (const digest of ['data-digest', 'config-digest', 'experiment-digest', 'baseline-digest', 'train-digest', 'test-digest', 'params-digest', 'candle-file-digest', 'funding-file-digest']) expect(html).toContain(digest)
    expect(html).toContain('BTCUSDT/1m/BTCUSDT-1m-2026-01-01.csv')
    expect(html).toContain(replayWalkForwardLocales['zh-CN'].binance_vision_um_1m_checksum_verified)
    for (const key of ['frozenConfig', 'frozenBaseline', 'frozenParams', 'windowPolicy', 'synthetic_ohlc_path', 'no_historical_order_book', 'zero_latency_no_market_impact', 'no_historical_predicted_funding'] as const) {
      expect(html).toContain(replayWalkForwardLocales['zh-CN'][key])
    }
    expect(html).not.toContain('replayWalkForward.')
    expect(html).toContain('grid_spacing')
    expect(html).toContain('200')
  })

  it('branches the actual modal away from legacy sorting, best badges and CSV export', async () => {
    const result: OptimResult = { task_id: 'replay', strategy: 'grid', all_results: [], elapsed: 1, total_combos: 7, completed: 7, replay_walk_forward: fixture() }
    const html = await render(<OptimResultModal isOpen onClose={() => {}} result={result} />)
    expect(html).toContain('data-testid="replay-results"')
    expect(html).not.toContain('<select')
    expect(html).not.toContain(enUS.optimResult.exportCsv)
    expect(html).not.toContain(enUS.optimResult.maxDrawdownFilter)
  })

  it('keeps old task results readable with their original filters and export', async () => {
    const old: OptimResult = { task_id: 'legacy', strategy: 'grid', all_results: [{ params: { grid_count: 20 }, total_return: 12, max_drawdown: 3, sharpe_ratio: 1, win_rate: 60, total_trades: 45 }], elapsed: 1, total_combos: 1, completed: 1 }
    const html = await render(<OptimResultModal isOpen onClose={() => {}} result={old} />)
    expect(html).not.toContain('data-testid="replay-results"')
    expect(html).toContain(enUS.optimResult.exportCsv)
    expect(html).toContain(enUS.optimResult.maxDrawdownFilter)
    expect(html).toContain('12.0000')
  })

  it('shows empty folds without fabricated performance', async () => {
    const html = await render(<ReplayWalkForwardResults result={{ ...fixture(), folds: [] }} />)
    expect(html).toContain(replayWalkForwardLocales['en-US'].noFolds)
    expect(html).not.toContain('data-testid="replay-excess"')
  })

  it('renders independent baseline fields and precise fee defaults in the grid form', async () => {
    const html = await render(<ReplayWalkForwardForm value={createReplayFormValues()} onChange={() => {}} startDate="2026-01-01" endDate="2026-01-07" interval="1h" />)
    expect(html).toContain('4 candidates')
    expect(html).toContain('4 estimated folds')
    expect(html).toContain('Baseline spacing (quote currency)')
    expect(html).toContain('Baseline order amount (USDT)')
    expect(html).toContain('0.0002')
    expect(html).toContain('0.0005')
    expect(html).not.toMatch(/risk_volume_multiplier|risk_average_window/)
  })

  it('provides complete Chinese, Traditional Chinese and English with explicit fallback', async () => {
    for (const locale of Object.values(replayWalkForwardLocales)) {
      expect(Object.keys(locale).sort()).toEqual(Object.keys(replayWalkForwardLocales['en-US']).sort())
      for (const [key, text] of Object.entries(locale)) {
        expect(text.trim()).not.toBe('')
        expect(text.match(/{{\w+}}/g) ?? []).toEqual(replayWalkForwardLocales['en-US'][key as keyof typeof locale].match(/{{\w+}}/g) ?? [])
      }
    }
    expect(withReplayWalkForward('de-DE', {}).replayWalkForward).toEqual(replayWalkForwardLocales['en-US'])
    const html = await render(<ReplayWalkForwardResults result={fixture()} />, 'zh-TW')
    expect(html).toContain('凍結入選參數')
    expect(html).not.toContain('replayWalkForward.')
  })
})
