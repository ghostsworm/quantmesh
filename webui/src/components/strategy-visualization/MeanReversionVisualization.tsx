import React, { useMemo } from 'react'
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
  Progress,
  useColorModeValue,
} from '@chakra-ui/react'
import { TriangleUpIcon, TriangleDownIcon } from '@chakra-ui/icons'
import PriceChart from './PriceChart'
import type { MeanReversionVisualizationData } from '../../services/strategy'

interface MeanReversionVisualizationProps {
  data: MeanReversionVisualizationData
  exchange?: string
  symbol?: string
}

const MeanReversionVisualization: React.FC<MeanReversionVisualizationProps> = ({ data }) => {
  const { t } = useTranslation()
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  // 生成价格图表数据
  const chartData = useMemo(() => {
    if (!data.currentPrice) return []
    const prices: Array<{ time: string; price: number }> = []
    const basePrice = data.currentPrice
    for (let i = 20; i >= 0; i--) {
      prices.push({
        time: `${i}`,
        price: basePrice * (1 + (Math.random() - 0.5) * 0.02),
      })
    }
    return prices
  }, [data.currentPrice])

  // 计算价格在布林带中的位置百分比
  const positionPercent = data.positionInBand || 50

  return (
    <VStack spacing={4} align="stretch">
      {/* 关键指标 */}
      <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">{t('strategyViz.meanReversion.currentPrice')}</StatLabel>
          <StatNumber fontSize="lg">${data.currentPrice?.toFixed(2) || '—'}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">{t('strategyViz.meanReversion.upperBand')}</StatLabel>
          <StatNumber fontSize="lg">${data.upperBand?.toFixed(2) || '—'}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">{t('strategyViz.meanReversion.middleBand')}</StatLabel>
          <StatNumber fontSize="lg">${data.middleBand?.toFixed(2) || '—'}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">{t('strategyViz.meanReversion.lowerBand')}</StatLabel>
          <StatNumber fontSize="lg">${data.lowerBand?.toFixed(2) || '—'}</StatNumber>
        </Stat>
      </SimpleGrid>

      {/* 价格在布林带中的位置 */}
      {data.upperBand && data.lowerBand && (
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>{t('strategyViz.meanReversion.positionInBand')}</Text>
          <VStack spacing={2}>
            <HStack w="100%" justify="space-between" fontSize="xs" color="gray.600">
              <Text>{t('strategyViz.meanReversion.lowerBand')} (${data.lowerBand.toFixed(2)})</Text>
              <Text>{t('strategyViz.meanReversion.upperBand')} (${data.upperBand.toFixed(2)})</Text>
            </HStack>
            <Box w="100%" position="relative">
              <Progress
                value={positionPercent}
                colorScheme={positionPercent < 20 ? 'green' : positionPercent > 80 ? 'red' : 'blue'}
                height="30px"
                borderRadius="md"
              />
              <Box
                position="absolute"
                left={`${positionPercent}%`}
                top="50%"
                transform="translate(-50%, -50%)"
                bg="white"
                px={2}
                py={1}
                borderRadius="md"
                boxShadow="md"
                fontSize="xs"
                fontWeight="bold"
              >
                ${data.currentPrice?.toFixed(2)}
              </Box>
            </Box>
            <Text fontSize="xs" color="gray.500">
              {t('strategyViz.meanReversion.position')}: {positionPercent.toFixed(1)}% ({positionPercent < 20 ? t('strategyViz.meanReversion.nearLower') : positionPercent > 80 ? t('strategyViz.meanReversion.nearUpper') : t('strategyViz.meanReversion.middleZone')})
            </Text>
          </VStack>
        </Box>
      )}

      {/* 价格图表 */}
      {chartData.length > 0 && (
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>{t('strategyViz.meanReversion.priceAndBollinger')}</Text>
          <PriceChart data={chartData} height={250} />
        </Box>
      )}

      {/* 交易信号和持仓状态 */}
      <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>{t('strategyViz.meanReversion.tradeSignals')}</Text>
          <VStack spacing={2} align="stretch">
            <HStack justify="space-between">
              <Text fontSize="sm" color="gray.600">{t('strategyViz.meanReversion.buySignal')}</Text>
              <Badge colorScheme={data.buySignal ? 'green' : 'gray'}>
                {data.buySignal ? t('strategyViz.meanReversion.yes') : t('strategyViz.meanReversion.no')}
              </Badge>
            </HStack>
            <HStack justify="space-between">
              <Text fontSize="sm" color="gray.600">{t('strategyViz.meanReversion.sellSignal')}</Text>
              <Badge colorScheme={data.sellSignal ? 'red' : 'gray'}>
                {data.sellSignal ? t('strategyViz.meanReversion.yes') : t('strategyViz.meanReversion.no')}
              </Badge>
            </HStack>
            {data.touchesLowerBand && (
              <HStack>
                <TriangleDownIcon color="green.500" />
                <Text fontSize="sm" color="green.600">{t('strategyViz.meanReversion.touchesLower')}</Text>
              </HStack>
            )}
            {data.touchesUpperBand && (
              <HStack>
                <TriangleUpIcon color="red.500" />
                <Text fontSize="sm" color="red.600">{t('strategyViz.meanReversion.touchesUpper')}</Text>
              </HStack>
            )}
            {data.distanceToBuy !== undefined && !data.hasPosition && (
              <HStack justify="space-between">
                <Text fontSize="sm" color="gray.600">{t('strategyViz.meanReversion.distanceToBuy')}</Text>
                <Text fontSize="sm" fontWeight="bold">
                  {data.distanceToBuy >= 0 ? '+' : ''}{data.distanceToBuy.toFixed(2)}%
                </Text>
              </HStack>
            )}
            {data.distanceToSell !== undefined && data.hasPosition && (
              <HStack justify="space-between">
                <Text fontSize="sm" color="gray.600">{t('strategyViz.meanReversion.distanceToSell')}</Text>
                <Text fontSize="sm" fontWeight="bold">
                  {data.distanceToSell >= 0 ? '+' : ''}{data.distanceToSell.toFixed(2)}%
                </Text>
              </HStack>
            )}
          </VStack>
        </Box>

        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>{t('strategyViz.meanReversion.positionStatus')}</Text>
          <VStack spacing={2} align="stretch">
            <HStack justify="space-between">
              <Text fontSize="sm" color="gray.600">{t('strategyViz.meanReversion.positionStatus')}</Text>
              <Badge colorScheme={data.hasPosition ? 'green' : 'gray'}>
                {data.hasPosition ? t('strategyViz.meanReversion.hasPosition') : t('strategyViz.meanReversion.noPosition')}
              </Badge>
            </HStack>
            {data.hasPosition && data.entryPrice && (
              <>
                <HStack justify="space-between">
                  <Text fontSize="sm" color="gray.600">{t('strategyViz.meanReversion.entryPrice')}</Text>
                  <Text fontSize="sm" fontWeight="bold">${data.entryPrice.toFixed(2)}</Text>
                </HStack>
                {data.pnlPercent !== undefined && (
                  <HStack justify="space-between">
                    <Text fontSize="sm" color="gray.600">{t('strategyViz.meanReversion.currentPnL')}</Text>
                    <HStack>
                      {data.pnlPercent >= 0 ? (
                        <TriangleUpIcon color="green.500" />
                      ) : (
                        <TriangleDownIcon color="red.500" />
                      )}
                      <Text
                        fontSize="sm"
                        fontWeight="bold"
                        color={data.pnlPercent >= 0 ? 'green.500' : 'red.500'}
                      >
                        {data.pnlPercent >= 0 ? '+' : ''}{data.pnlPercent.toFixed(2)}%
                      </Text>
                    </HStack>
                  </HStack>
                )}
              </>
            )}
            <HStack justify="space-between">
              <Text fontSize="sm" color="gray.600">{t('strategyViz.meanReversion.period')}</Text>
              <Text fontSize="sm">{data.period}</Text>
            </HStack>
            <HStack justify="space-between">
              <Text fontSize="sm" color="gray.600">{t('strategyViz.meanReversion.stdMultiplier')}</Text>
              <Text fontSize="sm">{data.stdMultiplier}</Text>
            </HStack>
          </VStack>
        </Box>
      </SimpleGrid>
    </VStack>
  )
}

export default MeanReversionVisualization
