import React from 'react'
import { SimpleGrid, Box, Text, Center, Spinner, Icon } from '@chakra-ui/react'
import { InfoOutlineIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import StrategyCard from './StrategyCard'
import type { StrategyInfo } from '../../types/strategy'

interface StrategyGridProps {
  strategies: StrategyInfo[]
  loading?: boolean
  error?: string | null
  onEnable: (strategyId: string) => void
  onDisable: (strategyId: string) => void
  onConfigure: (strategyId: string) => void
  onViewDetail: (strategyId: string) => void
}

const StrategyGrid: React.FC<StrategyGridProps> = ({
  strategies,
  loading,
  error,
  onEnable,
  onDisable,
  onConfigure,
  onViewDetail,
}) => {
  const { t } = useTranslation()

  if (loading) {
    return (
      <Center py={12}>
        <Spinner size="xl" thickness="4px" color="blue.500" />
      </Center>
    )
  }

  if (error) {
    return (
      <Center py={12}>
        <Box textAlign="center" color="red.500">
          <Icon as={InfoOutlineIcon} boxSize={8} mb={2} />
          <Text>{error}</Text>
        </Box>
      </Center>
    )
  }

  if (strategies.length === 0) {
    return (
      <Center py={12}>
        <Box textAlign="center" color="gray.500">
          <Icon as={InfoOutlineIcon} boxSize={8} mb={2} />
          <Text>{t('strategyMarket.noStrategies')}</Text>
        </Box>
      </Center>
    )
  }

  return (
    <SimpleGrid columns={{ base: 1, md: 2, lg: 3, xl: 4 }} spacing={6}>
      {strategies.map((strategy) => (
        <StrategyCard
          key={strategy.id}
          strategy={strategy}
          onEnable={onEnable}
          onDisable={onDisable}
          onConfigure={onConfigure}
          onViewDetail={onViewDetail}
        />
      ))}
    </SimpleGrid>
  )
}

export default StrategyGrid
