import React from 'react'
import { Box, HStack, Text, Icon, Tooltip, useColorModeValue } from '@chakra-ui/react'
import { StarIcon, CheckCircleIcon, TimeIcon, LockIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'

interface PremiumBadgeProps {
  tier?: 'basic' | 'pro' | 'enterprise'
  licensed?: boolean
  expiresAt?: string
  variant?: 'solid' | 'subtle' | 'outline'
  size?: 'sm' | 'md' | 'lg'
}

const getTierColor = (tier: string) => {
  switch (tier) {
    case 'basic':
      return 'blue'
    case 'pro':
      return 'purple'
    case 'enterprise':
      return 'yellow'
    default:
      return 'gray'
  }
}

const getTierGradient = (tier: string) => {
  switch (tier) {
    case 'basic':
      return 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)'
    case 'pro':
      return 'linear-gradient(135deg, #f093fb 0%, #f5576c 100%)'
    case 'enterprise':
      return 'linear-gradient(135deg, #f5af19 0%, #f12711 100%)'
    default:
      return 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)'
  }
}

const PremiumBadge: React.FC<PremiumBadgeProps> = ({
  tier = 'pro',
  licensed = false,
  expiresAt,
  variant = 'solid',
  size = 'md',
}) => {
  const { t } = useTranslation()

  const sizeConfig = {
    sm: { fontSize: 'xs', px: 2, py: 0.5, iconSize: 3 },
    md: { fontSize: 'sm', px: 3, py: 1, iconSize: 4 },
    lg: { fontSize: 'md', px: 4, py: 1.5, iconSize: 5 },
  }

  const config = sizeConfig[size]

  const isExpiring = expiresAt ? new Date(expiresAt).getTime() - Date.now() < 7 * 24 * 60 * 60 * 1000 : false
  const isExpired = expiresAt ? new Date(expiresAt).getTime() < Date.now() : false

  const getStatusIcon = () => {
    if (!licensed) return LockIcon
    if (isExpired) return LockIcon
    if (isExpiring) return TimeIcon
    return CheckCircleIcon
  }

  const getTooltipLabel = () => {
    if (!licensed) return t('premium.notLicensed')
    if (isExpired) return t('premium.expired')
    if (isExpiring) return t('premium.expiringSoon', { date: new Date(expiresAt!).toLocaleDateString() })
    if (expiresAt) return t('premium.validUntil', { date: new Date(expiresAt).toLocaleDateString() })
    return t('premium.licensed')
  }

  if (variant === 'solid') {
    return (
      <Tooltip label={getTooltipLabel()} hasArrow>
        <Box
          bg={licensed && !isExpired ? getTierGradient(tier) : 'gray.400'}
          color="white"
          px={config.px}
          py={config.py}
          borderRadius="full"
          display="inline-flex"
          alignItems="center"
          gap={1}
          cursor="pointer"
          _hover={{ opacity: 0.9 }}
          transition="opacity 0.2s"
        >
          <Icon as={licensed && !isExpired ? StarIcon : LockIcon} boxSize={config.iconSize} />
          <Text fontSize={config.fontSize} fontWeight="bold" textTransform="uppercase">
            {tier}
          </Text>
          {licensed && !isExpired && (
            <Icon as={getStatusIcon()} boxSize={config.iconSize} />
          )}
        </Box>
      </Tooltip>
    )
  }

  if (variant === 'outline') {
    return (
      <Tooltip label={getTooltipLabel()} hasArrow>
        <Box
          borderWidth="2px"
          borderColor={licensed && !isExpired ? getTierColor(tier) + '.400' : 'gray.400'}
          color={licensed && !isExpired ? getTierColor(tier) + '.500' : 'gray.500'}
          px={config.px}
          py={config.py}
          borderRadius="full"
          display="inline-flex"
          alignItems="center"
          gap={1}
          cursor="pointer"
          _hover={{ bg: licensed && !isExpired ? getTierColor(tier) + '.50' : 'gray.50' }}
          transition="background 0.2s"
        >
          <Icon as={licensed && !isExpired ? StarIcon : LockIcon} boxSize={config.iconSize} />
          <Text fontSize={config.fontSize} fontWeight="bold" textTransform="uppercase">
            {tier}
          </Text>
        </Box>
      </Tooltip>
    )
  }

  // subtle variant
  return (
    <Tooltip label={getTooltipLabel()} hasArrow>
      <Box
        bg={licensed && !isExpired ? getTierColor(tier) + '.100' : 'gray.100'}
        color={licensed && !isExpired ? getTierColor(tier) + '.700' : 'gray.600'}
        px={config.px}
        py={config.py}
        borderRadius="full"
        display="inline-flex"
        alignItems="center"
        gap={1}
        cursor="pointer"
        _hover={{ bg: licensed && !isExpired ? getTierColor(tier) + '.200' : 'gray.200' }}
        transition="background 0.2s"
      >
        <Icon as={licensed && !isExpired ? StarIcon : LockIcon} boxSize={config.iconSize} />
        <Text fontSize={config.fontSize} fontWeight="bold" textTransform="uppercase">
          {tier}
        </Text>
      </Box>
    </Tooltip>
  )
}

export default PremiumBadge
