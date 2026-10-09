import React from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertIcon, Badge, SimpleGrid, Stat, StatLabel, StatNumber, Text, VStack } from '@chakra-ui/react'
import type { FundingCarryVisualizationData } from '../../services/strategy'

interface FundingCarryVisualizationProps {
  data: FundingCarryVisualizationData
}

const recoveryReasonKeys = new Set([
  'execution_reconciliation_required',
  'startup_recovery_failed',
  'runtime_state_write_failed',
  'final_close_verification_pending',
  'repayment_confirmation_pending',
  'cover_order_reconciliation_pending',
  'exposure_unknown',
  'financial_intent_in_flight',
])

function formatQuantity(value: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 12 }).format(value)
}

const FundingCarryVisualization: React.FC<FundingCarryVisualizationProps> = ({ data }) => {
  const { t } = useTranslation()
  return (
    <VStack align="stretch" spacing={4}>
      {data.reconciliation_required && (
        <Alert status="warning" alignItems="flex-start">
          <AlertIcon />
          <VStack align="stretch" spacing={1}>
            <Badge colorScheme="orange" width="fit-content">{t('strategyViz.fundingCarry.reconciliationRequired')}</Badge>
            {data.reconciliation_reasons.map(reason => (
              <Text key={reason} fontSize="sm">
                {t(`strategyViz.fundingCarry.reasons.${recoveryReasonKeys.has(reason) ? reason : 'unknown'}`)}
              </Text>
            ))}
          </VStack>
        </Alert>
      )}
      <SimpleGrid columns={{ base: 1, md: 3 }} spacing={3}>
        <Stat>
          <StatLabel>{t('strategyViz.fundingCarry.recordedDebt')}</StatLabel>
          <StatNumber fontSize="lg">{formatQuantity(data.margin_debt)}</StatNumber>
        </Stat>
        <Stat>
          <StatLabel>{t('strategyViz.fundingCarry.recordedSpotQuantity')}</StatLabel>
          <StatNumber fontSize="lg">{formatQuantity(data.spot_qty)}</StatNumber>
        </Stat>
        <Stat>
          <StatLabel>{t('strategyViz.fundingCarry.recordedFuturesQuantity')}</StatLabel>
          <StatNumber fontSize="lg">{formatQuantity(data.futures_qty)}</StatNumber>
        </Stat>
      </SimpleGrid>
      {data.margin_borrow_transfer_id > 0 && (
        <Text fontSize="sm">
          {t('strategyViz.fundingCarry.borrowTransfer')}: {data.margin_borrow_transfer_id}
          {data.margin_borrowed_at ? ` · ${data.margin_borrowed_at}` : ''}
        </Text>
      )}
      <Text fontSize="sm" color="orange.600">
        {t('strategyViz.fundingCarry.ledgerWarning')}
      </Text>
    </VStack>
  )
}

export default FundingCarryVisualization
