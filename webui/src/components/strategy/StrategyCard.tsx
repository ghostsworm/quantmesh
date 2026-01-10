import React from 'react'
import {
  Box,
  Flex,
  HStack,
  VStack,
  Text,
  Badge,
  Button,
  Icon,
  Tooltip,
  useColorModeValue,
} from '@chakra-ui/react'
import {
  LockIcon,
  CheckCircleIcon,
  WarningIcon,
  InfoOutlineIcon,
  StarIcon,
} from '@chakra-ui/icons'
import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import type { StrategyInfo, RiskLevel } from '../../types/strategy'

const MotionBox = motion(Box)

interface StrategyCardProps {
  strategy: StrategyInfo
  onEnable: (strategyId: string) => void
  onDisable: (strategyId: string) => void
  onConfigure: (strategyId: string) => void
  onViewDetail: (strategyId: string) => void
}

const getRiskLevelColor = (level: RiskLevel) => {
  switch (level) {
    case 'low':
      return 'green'
    case 'medium':
      return 'orange'
    case 'high':
      return 'red'
    default:
      return 'gray'
  }
}

const getRiskLevelIcon = (level: RiskLevel) => {
  switch (level) {
    case 'low':
      return CheckCircleIcon
    case 'medium':
      return InfoOutlineIcon
    case 'high':
      return WarningIcon
    default:
      return InfoOutlineIcon
  }
}

const getStrategyTypeColor = (type: string) => {
  switch (type) {
    case 'grid':
      return 'blue'
    case 'dca':
      return 'purple'
    case 'martingale':
      return 'red'
    case 'trend':
      return 'teal'
    case 'mean_reversion':
      return 'cyan'
    case 'combo':
      return 'pink'
    default:
      return 'gray'
  }
}

const StrategyCard: React.FC<StrategyCardProps> = ({
  strategy,
  onEnable,
  onDisable,
  onConfigure,
  onViewDetail,
}) => {
  const { t } = useTranslation()
  const cardBg = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')
  const hoverBorderColor = useColorModeValue('blue.300', 'blue.400')

  return (
    <MotionBox
      bg={cardBg}
      borderWidth="1px"
      borderColor={borderColor}
      borderRadius="xl"
      p={5}
      cursor="pointer"
      onClick={() => onViewDetail(strategy.id)}
      whileHover={{ y: -4, boxShadow: '0 12px 24px rgba(0, 0, 0, 0.1)' }}
      transition={{ duration: 0.2 }}
      _hover={{ borderColor: hoverBorderColor }}
      position="relative"
      overflow="hidden"
    >
      {/* Premium Badge */}
      {strategy.isPremium && (
        <Box
          position="absolute"
          top={0}
          right={0}
          bg="linear-gradient(135deg, #667eea 0%, #764ba2 100%)"
          color="white"
          px={3}
          py={1}
          borderBottomLeftRadius="lg"
          fontSize="xs"
          fontWeight="bold"
        >
          <HStack spacing={1}>
            <Icon as={StarIcon} boxSize={3} />
            <Text>PRO</Text>
          </HStack>
        </Box>
      )}

      {/* Locked Overlay */}
      {strategy.isPremium && !strategy.isEnabled && (
        <Box
          position="absolute"
          top={0}
          left={0}
          right={0}
          bottom={0}
          bg="blackAlpha.50"
          display="flex"
          alignItems="center"
          justifyContent="center"
          borderRadius="xl"
          pointerEvents="none"
        >
          <Icon as={LockIcon} boxSize={8} color="gray.400" opacity={0.5} />
        </Box>
      )}

      <VStack align="stretch" spacing={4}>
        {/* Header */}
        <Flex justify="space-between" align="flex-start">
          <VStack align="start" spacing={1}>
            <Text fontSize="lg" fontWeight="bold" color="gray.800">
              {strategy.name}
            </Text>
            <HStack spacing={2}>
              <Badge
                colorScheme={getStrategyTypeColor(strategy.type)}
                fontSize="xs"
                borderRadius="full"
                px={2}
              >
                {t(`strategyMarket.types.${strategy.type}`, strategy.type)}
              </Badge>
              <Tooltip label={t(`strategyMarket.riskLevels.${strategy.riskLevel}`)}>
                <Badge
                  colorScheme={getRiskLevelColor(strategy.riskLevel)}
                  fontSize="xs"
                  borderRadius="full"
                  px={2}
                  display="flex"
                  alignItems="center"
                  gap={1}
                >
                  <Icon as={getRiskLevelIcon(strategy.riskLevel)} boxSize={3} />
                  {t(`strategyMarket.riskLevels.${strategy.riskLevel}`)}
                </Badge>
              </Tooltip>
            </HStack>
          </VStack>

          {strategy.isEnabled && (
            <Badge colorScheme="green" variant="subtle" fontSize="xs" borderRadius="full" px={2}>
              {t('strategyMarket.enabled')}
            </Badge>
          )}
        </Flex>

        {/* Description */}
        <Text fontSize="sm" color="gray.600" noOfLines={2}>
          {strategy.description}
        </Text>

        {/* Features */}
        <HStack spacing={1} flexWrap="wrap">
          {strategy.features.slice(0, 3).map((feature, index) => (
            <Badge
              key={index}
              variant="outline"
              colorScheme="gray"
              fontSize="xs"
              borderRadius="full"
              px={2}
            >
              {feature}
            </Badge>
          ))}
          {strategy.features.length > 3 && (
            <Badge variant="outline" colorScheme="gray" fontSize="xs" borderRadius="full" px={2}>
              +{strategy.features.length - 3}
            </Badge>
          )}
        </HStack>

        {/* Capital Info */}
        <Flex justify="space-between" align="center" fontSize="sm" color="gray.500">
          <Text>
            {t('strategyMarket.minCapital')}: {strategy.minCapital} USDT
          </Text>
          <Text>
            {t('strategyMarket.recommended')}: {strategy.recommendedCapital} USDT
          </Text>
        </Flex>

        {/* Actions */}
        <HStack spacing={2} pt={2}>
          {strategy.isEnabled ? (
            <>
              <Button
                size="sm"
                colorScheme="blue"
                flex={1}
                onClick={(e) => {
                  e.stopPropagation()
                  onConfigure(strategy.id)
                }}
              >
                {t('strategyMarket.configure')}
              </Button>
              <Button
                size="sm"
                variant="outline"
                colorScheme="red"
                onClick={(e) => {
                  e.stopPropagation()
                  onDisable(strategy.id)
                }}
              >
                {t('strategyMarket.disable')}
              </Button>
            </>
          ) : (
            <Button
              size="sm"
              colorScheme="blue"
              flex={1}
              leftIcon={strategy.isPremium ? <LockIcon /> : undefined}
              onClick={(e) => {
                e.stopPropagation()
                onEnable(strategy.id)
              }}
            >
              {strategy.isPremium ? t('strategyMarket.unlock') : t('strategyMarket.enable')}
            </Button>
          )}
        </HStack>
      </VStack>
    </MotionBox>
  )
}

export default StrategyCard
