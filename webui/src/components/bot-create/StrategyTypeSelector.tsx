import React from 'react'
import {
  Box,
  VStack,
  Text,
  SimpleGrid,
  useColorModeValue,
  Icon,
} from '@chakra-ui/react'
import { CopyIcon, AddIcon, RepeatIcon, StarIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'

export type StrategyTypeCategory = 'single' | 'combo' | 'hedge' | 'funding'

interface StrategyTypeOption {
  id: StrategyTypeCategory
  icon: typeof CopyIcon
  riskKey: string
}

const OPTIONS: StrategyTypeOption[] = [
  { id: 'single', icon: CopyIcon, riskKey: 'low' },
  { id: 'combo', icon: AddIcon, riskKey: 'medium' },
  { id: 'hedge', icon: RepeatIcon, riskKey: 'medium' },
  { id: 'funding', icon: StarIcon, riskKey: 'low' },
]

interface StrategyTypeSelectorProps {
  value: StrategyTypeCategory | null
  onChange: (value: StrategyTypeCategory) => void
}

const StrategyTypeSelector: React.FC<StrategyTypeSelectorProps> = ({ value, onChange }) => {
  const { t } = useTranslation()
  const cardBg = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')
  const selectedBorderColor = useColorModeValue('blue.500', 'blue.400')
  const hoverBorderColor = useColorModeValue('blue.300', 'blue.500')

  return (
    <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={4}>
      {OPTIONS.map((opt) => {
        const isSelected = value === opt.id
        return (
          <Box
            key={opt.id}
            as="button"
            type="button"
            textAlign="left"
            p={6}
            borderRadius="xl"
            borderWidth="2px"
            borderColor={isSelected ? selectedBorderColor : borderColor}
            bg={cardBg}
            _hover={{ borderColor: hoverBorderColor, shadow: 'md' }}
            _focus={{ outline: 'none', ring: 2, ringColor: 'blue.400' }}
            onClick={() => onChange(opt.id)}
            transition="all 0.2s"
          >
            <VStack align="stretch" spacing={3}>
              <Box display="flex" alignItems="center" gap={2}>
                <Icon as={opt.icon} boxSize={6} color="blue.500" />
                <Text fontWeight="bold" fontSize="lg">
                  {t(`botCreate.strategyType.${opt.id}.title`)}
                </Text>
              </Box>
              <Text fontSize="sm" color="gray.600" noOfLines={2}>
                {t(`botCreate.strategyType.${opt.id}.desc`)}
              </Text>
              <Text fontSize="xs" color="gray.500">
                {t(`botCreate.strategyType.${opt.id}.scene`)}
              </Text>
              <Text fontSize="xs" fontWeight="medium" color={opt.riskKey === 'high' ? 'red.500' : opt.riskKey === 'medium' ? 'orange.500' : 'green.500'}>
                {t(`botCreate.riskLevel.${opt.riskKey}`)}
              </Text>
            </VStack>
          </Box>
        )
      })}
    </SimpleGrid>
  )
}

export default StrategyTypeSelector
