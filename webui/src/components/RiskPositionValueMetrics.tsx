import { Badge, Box, Text } from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import type { PositionStatus } from '../services/api'

type ValueStatus = Pick<PositionStatus, 'total_position_value' | 'total_actual_margin' | 'max_position_value' | 'reached_limit_value' | 'leverage'> & { valuation_available?: boolean }

export function RiskPositionValueMetrics({ status }: { status?: ValueStatus | null }) {
  const { t } = useTranslation()
  const valued = status?.valuation_available !== false
  return (
    <>
      <Box data-testid="risk-notional-value">
        <Text fontSize="sm" color="gray.500">{t('botRiskControl.totalPositionValue')}</Text>
        <Text fontSize="lg" fontWeight="bold">
          ${valued ? status?.total_position_value?.toFixed(2) ?? '-' : '-'}
          {!!status?.max_position_value && (
            <Text as="span" fontSize="sm" color="gray.500">
              {' '} / ${status.max_position_value}
            </Text>
          )}
        </Text>
        {status?.reached_limit_value && (
          <Badge colorScheme="red" size="sm">{t('botRiskControl.reachedLimitValue')}</Badge>
        )}
      </Box>
      <Box data-testid="risk-margin-value">
        <Text fontSize="sm" color="gray.500">{t('botRiskControl.totalActualMargin')}</Text>
        <Text fontSize="lg" fontWeight="bold">
          ${valued ? status?.total_actual_margin?.toFixed(2) ?? '-' : '-'}
        </Text>
        {!!status?.leverage && status.leverage > 1 && (
          <Text fontSize="xs" color="gray.500">
            {t('dashboard.leverage')} {t('dashboard.leverageTimes', { count: status.leverage })}
          </Text>
        )}
      </Box>
    </>
  )
}
