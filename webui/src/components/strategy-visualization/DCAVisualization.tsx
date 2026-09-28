import React from 'react'
import { useTranslation } from 'react-i18next'
import {
  Box,
  VStack,
  HStack,
  Text,
  SimpleGrid,
  Badge,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  useColorModeValue,
} from '@chakra-ui/react'
import { TriangleUpIcon, TriangleDownIcon, WarningIcon } from '@chakra-ui/icons'
import type { DCAVisualizationData } from '../../services/strategy'

interface DCAVisualizationProps {
  data: DCAVisualizationData
  exchange?: string
  symbol?: string
}

const DCAVisualization: React.FC<DCAVisualizationProps> = ({ data }) => {
  const { t } = useTranslation()
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')
  const layerBgColor = useColorModeValue('gray.50', 'gray.700')

  return (
    <VStack spacing={4} align="stretch">
      {/* 关键指标 */}
      <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">{t('strategyViz.dca.currentPrice')}</StatLabel>
          <StatNumber fontSize="lg">${data.currentPrice?.toFixed(2) || '—'}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">{t('strategyViz.dca.avgCost')}</StatLabel>
          <StatNumber fontSize="lg">${data.avgEntryPrice?.toFixed(2) || '—'}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">{t('strategyViz.dca.atrValue')}</StatLabel>
          <StatNumber fontSize="lg">{data.atr?.toFixed(4) || '—'}</StatNumber>
          <StatHelpText fontSize="xs">{t('strategyViz.dca.dynamicSpacing')}: {data.dynamicInterval?.toFixed(2)}%</StatHelpText>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">{t('strategyViz.dca.currentLayer')}</StatLabel>
          <StatNumber fontSize="lg">
            {data.currentLayer || 0}/{data.maxLayers || 0}
          </StatNumber>
        </Stat>
      </SimpleGrid>

      {/* 分层持仓 */}
      {data.layers && data.layers.length > 0 && (
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>{t('strategyViz.dca.layeredPositions')}</Text>
          <VStack spacing={2} align="stretch" maxH="300px" overflowY="auto">
            {data.layers.map((layer, index) => {
              const isProfit = layer.pnl >= 0
              return (
                <Box
                  key={index}
                  p={3}
                  bg={layerBgColor}
                  borderRadius="md"
                  borderLeft="4px solid"
                  borderLeftColor={isProfit ? 'green.500' : 'red.500'}
                >
                  <HStack justify="space-between" mb={2}>
                    <HStack>
                      <Badge colorScheme={layer.status === 'filled' ? 'green' : 'gray'}>
                        {t('strategyViz.dca.layerN', { n: layer.index })}
                      </Badge>
                      <Text fontSize="sm" fontWeight="bold">
                        ${layer.price.toFixed(2)}
                      </Text>
                    </HStack>
                    <HStack>
                      {isProfit ? (
                        <TriangleUpIcon color="green.500" />
                      ) : (
                        <TriangleDownIcon color="red.500" />
                      )}
                      <Text fontSize="sm" color={isProfit ? 'green.500' : 'red.500'}>
                        {isProfit ? '+' : ''}{layer.pnlPercent.toFixed(2)}%
                      </Text>
                    </HStack>
                  </HStack>
                  <SimpleGrid columns={3} spacing={2} fontSize="xs" color="gray.600">
                    <Text>{t('strategyViz.dca.quantity')}: {layer.quantity.toFixed(4)}</Text>
                    <Text>{t('strategyViz.dca.cost')}: ${layer.cost.toFixed(2)}</Text>
                    <Text>{t('strategyViz.dca.pnl')}: ${layer.pnl.toFixed(2)}</Text>
                  </SimpleGrid>
                </Box>
              )
            })}
          </VStack>
        </Box>
      )}

      {/* 决策依据 */}
      <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
        <Text fontSize="sm" fontWeight="bold" mb={3}>{t('strategyViz.dca.decisionBasis')}</Text>
        <VStack spacing={2} align="stretch" fontSize="sm">
          {typeof data.firstOrderTakeProfit === 'number' && Number.isFinite(data.firstOrderTakeProfit) && data.firstOrderTakeProfit > 0 && (
            <HStack justify="space-between">
              <Text color="gray.600">{t('strategyViz.dca.firstOrderTP')}</Text>
              <Text fontWeight="bold">${data.firstOrderTakeProfit.toFixed(2)}</Text>
            </HStack>
          )}
          {typeof data.totalTakeProfit === 'number' && Number.isFinite(data.totalTakeProfit) && data.totalTakeProfit > 0 && (
            <HStack justify="space-between">
              <Text color="gray.600">{t('strategyViz.dca.totalTP')}</Text>
              <Text fontWeight="bold">${data.totalTakeProfit.toFixed(2)}</Text>
            </HStack>
          )}
          {typeof data.stopLoss === 'number' && Number.isFinite(data.stopLoss) && data.stopLoss > 0 && (
            <HStack justify="space-between">
              <Text color="gray.600">{t('strategyViz.dca.stopLoss')}</Text>
              <Text fontWeight="bold" color="red.500">${data.stopLoss.toFixed(2)}</Text>
            </HStack>
          )}
          {data.nextBuyPrice && data.distanceToNextBuy !== undefined && (
            <HStack justify="space-between">
              <Text color="gray.600">{t('strategyViz.dca.nextBuyPoint')}</Text>
              <HStack>
                <Text fontWeight="bold">${data.nextBuyPrice.toFixed(2)}</Text>
                <Text color="gray.500">
                  ({data.distanceToNextBuy >= 0 ? '+' : ''}{data.distanceToNextBuy.toFixed(2)}%)
                </Text>
              </HStack>
            </HStack>
          )}
          {data.isPaused && (
            <HStack>
              <WarningIcon color="orange.500" />
              <Text color="orange.600">{t('strategyViz.dca.cascadeProtectionActive')}</Text>
            </HStack>
          )}
          {data.trendFilterEnabled !== undefined && (
            <HStack justify="space-between">
              <Text color="gray.600">{t('strategyViz.dca.trendFilter')}</Text>
              <Badge colorScheme={data.isTrendUp ? 'green' : 'red'}>
                {data.isTrendUp ? t('strategyViz.dca.trendUp') : t('strategyViz.dca.trendDown')}
              </Badge>
            </HStack>
          )}
          {data.takeProfitTriggered && (
            <HStack>
              <Text color="green.600">{t('strategyViz.dca.trailingTPActive')}</Text>
              {typeof data.highestProfit === 'number' && Number.isFinite(data.highestProfit) && (
                <Text color="gray.500">{t('strategyViz.dca.highestProfit')}: {data.highestProfit.toFixed(2)}%</Text>
              )}
            </HStack>
          )}
        </VStack>
      </Box>
    </VStack>
  )
}

export default DCAVisualization
