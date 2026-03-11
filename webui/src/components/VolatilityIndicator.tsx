import React, { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Box,
  Flex,
  Text,
  Tooltip,
  Badge,
  HStack,
  VStack,
  Divider,
} from '@chakra-ui/react'
import { InfoIcon } from '@chakra-ui/icons'

export interface KlinePoint {
  time: number
  open: number
  high: number
  low: number
  close: number
  volume?: number
}

export interface VolatilityMetrics {
  /** ATR 绝对值（价格单位）*/
  atr: number
  /** ATR 百分比（相对于当前价格）*/
  atrPct: number
  /** 均线上方偏差均值（百分比），即价格超出MA的平均幅度 */
  upsideDev: number
  /** 均线下方偏差均值（百分比），即价格低于MA的平均幅度 */
  downsideDev: number
  /** 偏向系数：正值=偏多，负值=偏空（= upsideDev - downsideDev）*/
  biasFactor: number
  /** 总波动强度：= upsideDev + downsideDev */
  totalIntensity: number
  /** 参与计算的K线数量 */
  sampleCount: number
  /** 当前价格 */
  currentPrice: number
  /** MA周期 */
  maPeriod: number
}

/**
 * 计算简单移动平均（SMA）
 */
function calcSMA(prices: number[], period: number): number[] {
  const result: number[] = []
  for (let i = 0; i < prices.length; i++) {
    if (i < period - 1) {
      result.push(NaN)
    } else {
      let sum = 0
      for (let j = 0; j < period; j++) {
        sum += prices[i - j]
      }
      result.push(sum / period)
    }
  }
  return result
}

/**
 * 计算波动率指标
 *
 * 数学原理：
 * 1. ATR = mean(TrueRange_i) where TrueRange = max(H-L, |H-prevC|, |L-prevC|)
 * 2. 均线偏差面积：
 *    - upsideDev  = mean(max(0, close_i - MA_i) / MA_i) × 100   当 MA_i 有效时
 *    - downsideDev = mean(max(0, MA_i - close_i) / MA_i) × 100   当 MA_i 有效时
 *    这两个值构成"二维波动率向量"，描述价格围绕均线的上/下偏离程度
 *    biasFactor = upsideDev - downsideDev （正为多头偏向，负为空头偏向）
 *    totalIntensity = upsideDev + downsideDev （总波动烈度）
 */
export function calcVolatilityMetrics(
  klines: KlinePoint[],
  maPeriod = 20
): VolatilityMetrics | null {
  if (!klines || klines.length < maPeriod + 1) return null

  const closes = klines.map((k) => k.close)
  const highs = klines.map((k) => k.high)
  const lows = klines.map((k) => k.low)
  const n = klines.length

  // 计算 ATR
  const trueRanges: number[] = []
  for (let i = 1; i < n; i++) {
    const hl = highs[i] - lows[i]
    const hc = Math.abs(highs[i] - closes[i - 1])
    const lc = Math.abs(lows[i] - closes[i - 1])
    trueRanges.push(Math.max(hl, hc, lc))
  }
  const atr = trueRanges.reduce((acc, v) => acc + v, 0) / trueRanges.length
  const currentPrice = closes[n - 1]
  const atrPct = currentPrice > 0 ? (atr / currentPrice) * 100 : 0

  // 计算 MA
  const maValues = calcSMA(closes, maPeriod)

  // 计算均线偏差面积
  let upsideSum = 0
  let downsideSum = 0
  let validCount = 0

  for (let i = 0; i < n; i++) {
    const ma = maValues[i]
    if (isNaN(ma) || ma <= 0) continue
    const deviation = (closes[i] - ma) / ma
    if (deviation > 0) {
      upsideSum += deviation
    } else {
      downsideSum += Math.abs(deviation)
    }
    validCount++
  }

  if (validCount === 0) return null

  const upsideDev = (upsideSum / validCount) * 100
  const downsideDev = (downsideSum / validCount) * 100

  return {
    atr,
    atrPct,
    upsideDev,
    downsideDev,
    biasFactor: upsideDev - downsideDev,
    totalIntensity: upsideDev + downsideDev,
    sampleCount: n,
    currentPrice,
    maPeriod,
  }
}

/**
 * 波动等级判断
 * 基于 ATR% 进行分档，阈值来源于 BTC 历史经验
 */
export type VolatilityLevel = 'calm' | 'normal' | 'active' | 'extreme'

export function getVolatilityLevel(atrPct: number): VolatilityLevel {
  if (atrPct < 0.2) return 'calm'
  if (atrPct < 0.5) return 'normal'
  if (atrPct < 1.2) return 'active'
  return 'extreme'
}

interface VolatilityIndicatorProps {
  klines: KlinePoint[]
  /** MA 周期，默认 20 */
  maPeriod?: number
}

const VolatilityIndicator: React.FC<VolatilityIndicatorProps> = ({
  klines,
  maPeriod = 20,
}) => {
  const { t } = useTranslation()

  const metrics = useMemo(
    () => calcVolatilityMetrics(klines, maPeriod),
    [klines, maPeriod]
  )

  if (!metrics) return null

  const level = getVolatilityLevel(metrics.atrPct)

  const levelColors: Record<VolatilityLevel, string> = {
    calm: 'green',
    normal: 'blue',
    active: 'orange',
    extreme: 'red',
  }

  const levelKey: Record<VolatilityLevel, string> = {
    calm: 'volatilityIndicator.level.calm',
    normal: 'volatilityIndicator.level.normal',
    active: 'volatilityIndicator.level.active',
    extreme: 'volatilityIndicator.level.extreme',
  }

  const biasColor =
    metrics.biasFactor > 0.02
      ? '#16a34a'
      : metrics.biasFactor < -0.02
      ? '#dc2626'
      : '#6b7280'

  const biasLabel =
    metrics.biasFactor > 0.02
      ? t('volatilityIndicator.bias.bullish')
      : metrics.biasFactor < -0.02
      ? t('volatilityIndicator.bias.bearish')
      : t('volatilityIndicator.bias.neutral')

  return (
    <Box
      mb={4}
      px={4}
      py={3}
      borderRadius="lg"
      border="1px solid"
      borderColor="gray.200"
      bg="gray.50"
      fontSize="sm"
    >
      <Flex align="center" justify="space-between" wrap="wrap" gap={3}>
        {/* 标题 + 等级徽章 */}
        <HStack spacing={2}>
          <Text fontWeight="600" color="gray.700">
            {t('volatilityIndicator.title')}
          </Text>
          <Badge colorScheme={levelColors[level]} borderRadius="full" px={2}>
            {t(levelKey[level])}
          </Badge>
          <Tooltip
            label={t('volatilityIndicator.tooltip', { period: maPeriod, count: metrics.sampleCount })}
            placement="top"
            hasArrow
          >
            <InfoIcon color="gray.400" boxSize="14px" cursor="help" />
          </Tooltip>
        </HStack>

        <Flex gap={5} wrap="wrap" align="center">
          {/* ATR% */}
          <VStack spacing={0} align="center" minW="80px">
            <Text fontSize="xs" color="gray.500">
              {t('volatilityIndicator.atrPct')}
            </Text>
            <Text fontWeight="700" color="gray.800" fontSize="md">
              {metrics.atrPct.toFixed(3)}%
            </Text>
          </VStack>

          <Divider orientation="vertical" h="36px" />

          {/* 二维偏差向量 */}
          <VStack spacing={0} align="center" minW="80px">
            <Text fontSize="xs" color="gray.500">
              {t('volatilityIndicator.upsideDev')}
            </Text>
            <Text fontWeight="700" color="#16a34a" fontSize="md">
              +{metrics.upsideDev.toFixed(3)}%
            </Text>
          </VStack>

          <VStack spacing={0} align="center" minW="80px">
            <Text fontSize="xs" color="gray.500">
              {t('volatilityIndicator.downsideDev')}
            </Text>
            <Text fontWeight="700" color="#dc2626" fontSize="md">
              -{metrics.downsideDev.toFixed(3)}%
            </Text>
          </VStack>

          <Divider orientation="vertical" h="36px" />

          {/* 偏向 + 总强度 */}
          <VStack spacing={0} align="center" minW="80px">
            <Text fontSize="xs" color="gray.500">
              {t('volatilityIndicator.biasLabel')}
            </Text>
            <Text fontWeight="700" color={biasColor} fontSize="md">
              {biasLabel}
            </Text>
          </VStack>

          <VStack spacing={0} align="center" minW="80px">
            <Text fontSize="xs" color="gray.500">
              {t('volatilityIndicator.intensity')}
            </Text>
            <Text fontWeight="700" color="purple.600" fontSize="md">
              {metrics.totalIntensity.toFixed(3)}%
            </Text>
          </VStack>
        </Flex>
      </Flex>
    </Box>
  )
}

export default VolatilityIndicator
