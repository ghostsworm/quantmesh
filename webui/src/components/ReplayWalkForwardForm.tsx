import { useId } from 'react'
import { Box, FormControl, FormLabel, Heading, Select, SimpleGrid, Switch, Text, VStack } from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import DecimalNumberInput from './DecimalNumberInput'
import {
  buildReplaySearchSpace, REPLAY_FEATURES, type ReplayFormValues, type ReplayNumericField,
} from '../utils/replayWalkForward'

interface Props {
  value: ReplayFormValues
  onChange: (value: ReplayFormValues) => void
  startDate: string
  endDate: string
  interval: string
  disabled?: boolean
}

export default function ReplayWalkForwardForm({ value, onChange, startDate, endDate, interval, disabled }: Props) {
  const { t } = useTranslation()
  const id = useId()
  const preview = buildReplaySearchSpace(value, startDate, endDate, interval)
  const numericFields = (fields: ReplayNumericField[]) => (
    <SimpleGrid columns={{ base: 1, sm: 3 }} spacing={3}>
      {fields.map((field) => (
        <FormControl key={field} isDisabled={disabled}>
          <FormLabel htmlFor={`${id}-${field}`} fontSize="sm">{t(`replayWalkForward.${field}`)}</FormLabel>
          <DecimalNumberInput
            id={`${id}-${field}`} size="sm" value={value[field]} precision={8}
            step={field.includes('Fee') || field === 'fundingRate' ? 0.0001 : 1}
            onChange={(next) => onChange({ ...value, [field]: next })}
          />
        </FormControl>
      ))}
    </SimpleGrid>
  )
  return (
    <VStack align="stretch" spacing={4} data-testid="replay-form">
      <Text fontSize="sm">{t('replayWalkForward.description')}</Text>
      <Text fontSize="xs" color="gray.500">{t('replayWalkForward.source')}</Text>
      <Heading size="sm">{t('replayWalkForward.protocol')}</Heading>
      {numericFields(['trainDays', 'testDays', 'leverage'])}
      <Text fontSize="xs" color="gray.500">{t('replayWalkForward.windowHelp')}</Text>
      <Heading size="sm">{t('replayWalkForward.search')}</Heading>
      {numericFields(['spacingMin', 'spacingMax', 'spacingStep', 'quantityMin', 'quantityMax', 'quantityStep'])}
      <Heading size="sm">{t('replayWalkForward.baseline')}</Heading>
      <Text fontSize="xs" color="gray.500">{t('replayWalkForward.baselineHelp')}</Text>
      {numericFields(['baselineSpacing', 'baselineQuantity', 'profitSpread', 'buyWindow', 'sellWindow', 'makerFee', 'takerFee'])}
      <FormControl isDisabled={disabled}>
        <FormLabel htmlFor={`${id}-direction`} fontSize="sm">{t('replayWalkForward.direction')}</FormLabel>
        <Select id={`${id}-direction`} size="sm" value={value.direction} onChange={(e) => onChange({ ...value, direction: e.target.value as ReplayFormValues['direction'] })}>
          <option value="LONG">{t('replayWalkForward.long')}</option>
          <option value="SHORT">{t('replayWalkForward.short')}</option>
        </Select>
      </FormControl>
      <Text fontSize="xs" color="gray.500">{t('replayWalkForward.feeHelp')}</Text>
      <FormControl display="flex" alignItems="center" isDisabled={disabled}>
        <FormLabel htmlFor={`${id}-funding`} mb={0} fontSize="sm">{t('replayWalkForward.fundingEnabled')}</FormLabel>
        <Switch id={`${id}-funding`} isChecked={value.fundingEnabled} onChange={(e) => onChange({ ...value, fundingEnabled: e.target.checked })} />
      </FormControl>
      {value.fundingEnabled && numericFields(['fundingRate'])}
      <Box as="details">
        <Box as="summary" cursor="pointer" fontSize="sm">{t('replayWalkForward.features')}</Box>
        <Text fontSize="xs" color="gray.500" my={2}>{t('replayWalkForward.featureHelp')}</Text>
        <VStack align="stretch" spacing={2}>
          {REPLAY_FEATURES.map((feature) => (
            <FormControl key={feature} display="flex" alignItems="center" isDisabled={disabled}>
              <FormLabel htmlFor={`${id}-${feature}`} mb={0} fontSize="sm">{t(`replayWalkForward.${feature}`)}</FormLabel>
              <Switch id={`${id}-${feature}`} isChecked={value.features[feature]} onChange={(e) => onChange({ ...value, features: { ...value.features, [feature]: e.target.checked } })} />
            </FormControl>
          ))}
        </VStack>
      </Box>
      <Text fontSize="sm" role="status" color={preview.ok ? undefined : 'orange.500'}>
        {preview.ok ? t('replayWalkForward.estimate', preview) : t(`replayWalkForward.${preview.error}`)}
      </Text>
      <Text fontSize="xs" color="gray.500">{t('replayWalkForward.windowPolicy')}</Text>
    </VStack>
  )
}
