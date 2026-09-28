import { Alert, AlertIcon, Box, Heading, SimpleGrid, Text } from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import type { ExecutionExposure } from '../services/executionExposure'

export function ExecutionExposureMetrics({ exposure }: { exposure?: ExecutionExposure }) {
  const { t, i18n } = useTranslation()
  const reason = ['not_initialized', 'reconciliation_required', 'quote_unavailable'].includes(exposure?.reason_code ?? '')
    ? exposure?.reason_code : 'unknown'
  const hasAdmissionStatus = typeof exposure?.opening_available === 'boolean' && typeof exposure?.new_lot_available === 'boolean'
  const message = !exposure ? 'missing' : !exposure.ready ? reason : !hasAdmissionStatus ? 'unknown' : !exposure.opening_available
    ? 'exhausted' : !exposure.new_lot_available ? 'layersFull' : 'ready'
  const number = (value?: number) => Number.isFinite(value) ? value?.toLocaleString(i18n.resolvedLanguage ?? i18n.language, { maximumFractionDigits: 8 }) : t('executionExposure.unavailable')
  const knownInventory = exposure?.reason_code !== 'not_initialized'
  const metrics = exposure ? [
    { key: 'position', value: knownInventory ? exposure.position_quantity : undefined },
    { key: 'pending', value: knownInventory ? exposure.pending_quantity : undefined },
    { key: 'projected', value: knownInventory ? exposure.projected_quantity : undefined, limit: exposure.limits.quantity },
    { key: 'notional', value: exposure.ready ? exposure.projected_notional : undefined, limit: exposure.limits.notional },
    { key: 'layers', value: knownInventory ? exposure.layers : undefined, limit: exposure.limits.layers },
  ] : []
  return (
    <Box mb={4} data-testid="execution-exposure">
      <Heading size="sm" mb={2}>{t('executionExposure.title')}</Heading>
      <Alert status={!exposure?.ready ? 'warning' : exposure.opening_available && exposure.new_lot_available ? 'info' : 'warning'} borderRadius="md" mb={3}>
        <AlertIcon /><Text fontSize="sm">{t(`executionExposure.${message}`)}</Text>
      </Alert>
      <SimpleGrid columns={{ base: 1, md: 3 }} spacing={3}>
        {metrics.map(({ key, value, limit }) => (
          <Box key={key} data-testid={`exposure-${key}`}>
            <Text fontSize="sm" color="gray.500">{t(`executionExposure.${key}`)}</Text>
            <Text fontWeight="bold">{number(value)}</Text>
            {limit !== undefined && <Text fontSize="xs">{limit > 0 ? t('executionExposure.limit', { value: number(limit) }) : t('executionExposure.unlimited')}</Text>}
          </Box>
        ))}
      </SimpleGrid>
      <Text fontSize="xs" mt={2} color="gray.500">{t('executionExposure.note')}</Text>
    </Box>
  )
}
