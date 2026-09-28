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
import type { GridVisualizationData } from '../../services/strategy'

interface GridVisualizationProps {
  data: GridVisualizationData
  exchange?: string
  symbol?: string
}

const GridVisualization: React.FC<GridVisualizationProps> = ({ data }) => {
  const { t } = useTranslation()
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')
  const slotBgColor = useColorModeValue('gray.50', 'gray.700')

  // 计算填充率
  const fillRate = data.slotCount && data.filledCount
    ? (data.filledCount / data.slotCount) * 100
    : 0

  return (
    <VStack spacing={4} align="stretch">
      {/* 关键指标 */}
      <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">{t('strategyViz.grid.totalSlots')}</StatLabel>
          <StatNumber fontSize="lg">{data.slotCount || 0}</StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">{t('strategyViz.grid.filled')}</StatLabel>
          <StatNumber fontSize="lg" color="green.500">
            {data.filledCount || 0}
          </StatNumber>
          <StatHelpText fontSize="xs">{t('strategyViz.grid.fillRate')}: {fillRate.toFixed(1)}%</StatHelpText>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">{t('strategyViz.grid.priceRange')}</StatLabel>
          <StatNumber fontSize="lg">
            ${data.minPrice?.toFixed(2) || '—'} - ${data.maxPrice?.toFixed(2) || '—'}
          </StatNumber>
        </Stat>
        <Stat p={3} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <StatLabel fontSize="xs">{t('strategyViz.grid.priceInterval')}</StatLabel>
          <StatNumber fontSize="lg">${data.priceInterval?.toFixed(2) || '—'}</StatNumber>
        </Stat>
      </SimpleGrid>

      {/* 槽位状态概览 */}
      {data.slots && data.slots.length > 0 && (
        <Box p={4} bg={bgColor} borderRadius="lg" border="1px solid" borderColor={borderColor}>
          <Text fontSize="sm" fontWeight="bold" mb={3}>{t('strategyViz.grid.slotStatusOverview')}</Text>
          <VStack spacing={2} align="stretch" maxH="300px" overflowY="auto">
            {/* 只显示前20个槽位，避免列表过长 */}
            {data.slots.slice(0, 20).map((slot, index) => (
              <Box
                key={index}
                p={2}
                bg={slotBgColor}
                borderRadius="md"
                borderLeft="4px solid"
                borderLeftColor={
                  slot.positionStatus === 'FILLED'
                    ? 'green.500'
                    : slot.slotStatus === 'LOCKED'
                    ? 'red.500'
                    : 'gray.300'
                }
              >
                <HStack justify="space-between" fontSize="sm">
                  <HStack>
                    <Text fontWeight="bold">${slot.price.toFixed(2)}</Text>
                    <Badge
                      colorScheme={
                        slot.positionStatus === 'FILLED'
                          ? 'green'
                          : slot.slotStatus === 'LOCKED'
                          ? 'red'
                          : 'gray'
                      }
                      fontSize="xs"
                    >
                      {slot.positionStatus === 'FILLED'
                        ? t('strategyViz.grid.statusFilled')
                        : slot.slotStatus === 'LOCKED'
                        ? t('strategyViz.grid.statusLocked')
                        : t('strategyViz.grid.statusIdle')}
                    </Badge>
                  </HStack>
                  {slot.positionQty > 0 && (
                    <Text fontSize="xs" color="gray.600">
                      {t('strategyViz.grid.quantity')}: {slot.positionQty.toFixed(4)}
                    </Text>
                  )}
                </HStack>
              </Box>
            ))}
            {data.slots.length > 20 && (
              <Text fontSize="xs" color="gray.500" textAlign="center" mt={2}>
                {t('strategyViz.grid.showingSlots', { shown: 20, total: data.slots.length })}
              </Text>
            )}
          </VStack>
        </Box>
      )}
    </VStack>
  )
}

export default GridVisualization
