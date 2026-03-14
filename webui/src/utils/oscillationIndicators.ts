/**
 * 震荡分析指标计算（基于约 100 根 K 线）
 * - 震荡强度指数（Shake Strength）：Mean(|close-mid|)/mid，百分比
 * - 网格友好指数（Grid Friendly）：min(上面积,下面积)/max(上面积,下面积)，0~1
 */

export function computeShakeStrength(closes: number[]): number {
  if (closes.length === 0) return 0
  const maxC = Math.max(...closes)
  const minC = Math.min(...closes)
  const mid = (maxC + minC) / 2
  if (mid <= 0) return 0
  const absDiffs = closes.map((c) => Math.abs(c - mid))
  const meanAbsDiff = absDiffs.reduce((a, b) => a + b, 0) / absDiffs.length
  return (meanAbsDiff / mid) * 100
}

export function computeGridFriendly(closes: number[]): number {
  if (closes.length === 0) return 0
  const maxC = Math.max(...closes)
  const minC = Math.min(...closes)
  const mid = (maxC + minC) / 2
  let upperArea = 0
  let lowerArea = 0
  for (const c of closes) {
    const diff = c - mid
    if (diff > 0) upperArea += diff
    else if (diff < 0) lowerArea += -diff
  }
  const maxArea = Math.max(upperArea, lowerArea)
  const minArea = Math.min(upperArea, lowerArea)
  if (maxArea <= 0) return 1
  return minArea / maxArea
}
