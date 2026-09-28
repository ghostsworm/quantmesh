import React from 'react'
import { useTranslation } from 'react-i18next'
import { Box, Text, Spinner, Center } from '@chakra-ui/react'
import DCAVisualization from './DCAVisualization'
import TrendFollowingVisualization from './TrendFollowingVisualization'
import MeanReversionVisualization from './MeanReversionVisualization'
import GridVisualization from './GridVisualization'
import type {
  DCAVisualizationData,
  GridVisualizationData,
  MeanReversionVisualizationData,
  StrategyRuntimeStatus,
  TrendFollowingVisualizationData,
} from '../../services/strategy'

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasNumericField(data: unknown, fields: string[]): boolean {
  return isRecord(data) && fields.some(field => typeof data[field] === 'number' && Number.isFinite(data[field]))
}

function isDcaData(data: unknown): data is DCAVisualizationData {
  return (isRecord(data) && Array.isArray(data.layers)) || hasNumericField(data, ['avgEntryPrice', 'totalCost', 'nextBuyPrice'])
}

function isTrendData(data: unknown): data is TrendFollowingVisualizationData {
  return hasNumericField(data, ['fastMA', 'slowMA', 'entryPrice', 'maDiff'])
}

function isMeanReversionData(data: unknown): data is MeanReversionVisualizationData {
  return hasNumericField(data, ['upperBand', 'middleBand', 'lowerBand', 'positionInBand'])
}

function isGridData(data: unknown): data is GridVisualizationData {
  return (isRecord(data) && Array.isArray(data.slots)) || hasNumericField(data, ['slotCount', 'filledCount', 'emptyCount'])
}

interface StrategyVisualizationProps {
  strategy: StrategyRuntimeStatus
  exchange?: string
  symbol?: string
}

const StrategyVisualization: React.FC<StrategyVisualizationProps> = ({
  strategy,
  exchange,
  symbol,
}) => {
  const { t } = useTranslation()
  if (!strategy.visualizationData) {
    return (
      <Center h="200px">
        <Text color="gray.500" fontSize="sm">{t('strategyVisualization.noData')}</Text>
      </Center>
    )
  }

  // 根据策略类型路由到对应的可视化组件
  const strategyType = strategy.type.toLowerCase()
  
  if ((strategyType.includes('dca') || strategyType.includes('定投')) && isDcaData(strategy.visualizationData)) {
    return (
      <DCAVisualization
        data={strategy.visualizationData}
        exchange={exchange}
        symbol={symbol}
      />
    )
  }
  
  if ((strategyType.includes('trend') || strategyType.includes('趋势') || strategyType.includes('trending')) && isTrendData(strategy.visualizationData)) {
    return (
      <TrendFollowingVisualization
        data={strategy.visualizationData}
        exchange={exchange}
        symbol={symbol}
      />
    )
  }
  
  if ((strategyType.includes('mean') || strategyType.includes('均值') || strategyType.includes('reversion')) && isMeanReversionData(strategy.visualizationData)) {
    return (
      <MeanReversionVisualization
        data={strategy.visualizationData}
        exchange={exchange}
        symbol={symbol}
      />
    )
  }
  
  if ((strategyType.includes('grid') || strategyType.includes('网格')) && isGridData(strategy.visualizationData)) {
    return (
      <GridVisualization
        data={strategy.visualizationData}
        exchange={exchange}
        symbol={symbol}
      />
    )
  }

  // 默认显示原始数据
  return (
    <Box p={4}>
      <Text fontSize="sm" color="gray.500" mb={2}>{t('strategyVisualization.strategyType')}: {strategy.type}</Text>
      <Text fontSize="xs" color="gray.400" fontFamily="mono">
        {JSON.stringify(strategy.visualizationData, null, 2)}
      </Text>
    </Box>
  )
}

export default StrategyVisualization
