import React from 'react'
import { Box, Text, Spinner, Center } from '@chakra-ui/react'
import DCAVisualization from './DCAVisualization'
import TrendFollowingVisualization from './TrendFollowingVisualization'
import MeanReversionVisualization from './MeanReversionVisualization'
import GridVisualization from './GridVisualization'
import type { StrategyRuntimeStatus } from '../../services/strategy'

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
  if (!strategy.visualizationData) {
    return (
      <Center h="200px">
        <Text color="gray.500" fontSize="sm">暂无可视化数据</Text>
      </Center>
    )
  }

  // 根据策略类型路由到对应的可视化组件
  const strategyType = strategy.type.toLowerCase()
  
  if (strategyType.includes('dca') || strategyType.includes('定投')) {
    return (
      <DCAVisualization
        data={strategy.visualizationData}
        exchange={exchange}
        symbol={symbol}
      />
    )
  }
  
  if (strategyType.includes('trend') || strategyType.includes('趋势') || strategyType.includes('trending')) {
    return (
      <TrendFollowingVisualization
        data={strategy.visualizationData}
        exchange={exchange}
        symbol={symbol}
      />
    )
  }
  
  if (strategyType.includes('mean') || strategyType.includes('均值') || strategyType.includes('reversion')) {
    return (
      <MeanReversionVisualization
        data={strategy.visualizationData}
        exchange={exchange}
        symbol={symbol}
      />
    )
  }
  
  if (strategyType.includes('grid') || strategyType.includes('网格')) {
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
      <Text fontSize="sm" color="gray.500" mb={2}>策略类型: {strategy.type}</Text>
      <Text fontSize="xs" color="gray.400" fontFamily="mono">
        {JSON.stringify(strategy.visualizationData, null, 2)}
      </Text>
    </Box>
  )
}

export default StrategyVisualization
