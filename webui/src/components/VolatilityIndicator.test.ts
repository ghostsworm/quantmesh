import { describe, expect, it } from 'vitest'
import {
  calcVolatilityMetrics,
  getVolatilityLevel,
  type KlinePoint,
} from './VolatilityIndicator'

// 生成模拟 K 线数据的工具函数
function makeKlines(closes: number[], spread = 0.005): KlinePoint[] {
  return closes.map((close, i) => ({
    time: 1700000000 + i * 60,
    open: close * (1 - spread / 2),
    high: close * (1 + spread),
    low: close * (1 - spread),
    close,
    volume: 100,
  }))
}

// 生成 N 根价格围绕基准震荡的 K 线
function makeOscillatingKlines(
  n: number,
  base: number,
  amplitude: number
): KlinePoint[] {
  const closes: number[] = []
  for (let i = 0; i < n; i++) {
    closes.push(base + amplitude * Math.sin((i * Math.PI) / 10))
  }
  return makeKlines(closes, 0.001)
}

describe('calcVolatilityMetrics', () => {
  it('数据不足时返回 null', () => {
    const result = calcVolatilityMetrics(makeKlines([100, 200]), 20)
    expect(result).toBeNull()
  })

  it('正常数据返回有效指标', () => {
    const klines = makeOscillatingKlines(50, 80000, 500)
    const result = calcVolatilityMetrics(klines, 20)

    expect(result).not.toBeNull()
    expect(result!.atr).toBeGreaterThan(0)
    expect(result!.atrPct).toBeGreaterThan(0)
    expect(result!.upsideDev).toBeGreaterThanOrEqual(0)
    expect(result!.downsideDev).toBeGreaterThanOrEqual(0)
    expect(result!.sampleCount).toBe(50)
    expect(result!.maPeriod).toBe(20)
  })

  it('totalIntensity = upsideDev + downsideDev', () => {
    const klines = makeOscillatingKlines(50, 80000, 300)
    const result = calcVolatilityMetrics(klines)!
    expect(result.totalIntensity).toBeCloseTo(
      result.upsideDev + result.downsideDev,
      8
    )
  })

  it('biasFactor = upsideDev - downsideDev', () => {
    const klines = makeOscillatingKlines(50, 80000, 300)
    const result = calcVolatilityMetrics(klines)!
    expect(result.biasFactor).toBeCloseTo(
      result.upsideDev - result.downsideDev,
      8
    )
  })

  it('单调上涨时 upsideDev > downsideDev（偏多）', () => {
    // 持续上涨的价格会让收盘价一直高于滞后的均线
    const closes = Array.from({ length: 50 }, (_, i) => 80000 + i * 100)
    const klines = makeKlines(closes)
    const result = calcVolatilityMetrics(klines, 10)!
    expect(result.upsideDev).toBeGreaterThan(result.downsideDev)
    expect(result.biasFactor).toBeGreaterThan(0)
  })

  it('单调下跌时 downsideDev > upsideDev（偏空）', () => {
    const closes = Array.from({ length: 50 }, (_, i) => 80000 - i * 100)
    const klines = makeKlines(closes)
    const result = calcVolatilityMetrics(klines, 10)!
    expect(result.downsideDev).toBeGreaterThan(result.upsideDev)
    expect(result.biasFactor).toBeLessThan(0)
  })

  it('完全水平价格时 totalIntensity 接近 0', () => {
    // 价格固定不变，均线 = 价格，偏差为 0
    const closes = Array.from({ length: 50 }, () => 80000)
    const klines = makeKlines(closes, 0)
    const result = calcVolatilityMetrics(klines, 10)!
    expect(result.totalIntensity).toBeCloseTo(0, 5)
  })

  it('currentPrice 等于最后一根 K 线的收盘价', () => {
    const closes = Array.from({ length: 50 }, (_, i) => 80000 + i * 50)
    const klines = makeKlines(closes)
    const result = calcVolatilityMetrics(klines, 10)!
    expect(result.currentPrice).toBe(closes[closes.length - 1])
  })

  it('ATR 不超过均值价格的 10%（正常市场假设）', () => {
    const klines = makeOscillatingKlines(100, 80000, 1000)
    const result = calcVolatilityMetrics(klines, 20)!
    expect(result.atrPct).toBeLessThan(10)
  })

  it('支持自定义 MA 周期', () => {
    const klines = makeOscillatingKlines(50, 50000, 200)
    const r5 = calcVolatilityMetrics(klines, 5)!
    const r20 = calcVolatilityMetrics(klines, 20)!
    expect(r5.maPeriod).toBe(5)
    expect(r20.maPeriod).toBe(20)
    // 短周期 MA 追踪价格更紧，偏差面积通常更小
    expect(r5.totalIntensity).toBeLessThanOrEqual(r20.totalIntensity + 0.001)
  })
})

describe('getVolatilityLevel', () => {
  it('atrPct < 0.2 => calm', () => {
    expect(getVolatilityLevel(0.1)).toBe('calm')
    expect(getVolatilityLevel(0.19)).toBe('calm')
  })

  it('0.2 <= atrPct < 0.5 => normal', () => {
    expect(getVolatilityLevel(0.2)).toBe('normal')
    expect(getVolatilityLevel(0.49)).toBe('normal')
  })

  it('0.5 <= atrPct < 1.2 => active', () => {
    expect(getVolatilityLevel(0.5)).toBe('active')
    expect(getVolatilityLevel(1.19)).toBe('active')
  })

  it('atrPct >= 1.2 => extreme', () => {
    expect(getVolatilityLevel(1.2)).toBe('extreme')
    expect(getVolatilityLevel(5.0)).toBe('extreme')
  })
})
