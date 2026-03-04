import React, { useEffect, useState } from 'react'
import {
  Box,
  VStack,
  Text,
  RadioGroup,
  Radio,
  Stack,
  CheckboxGroup,
  Checkbox,
  Slider,
  SliderTrack,
  SliderFilledTrack,
  SliderThumb,
  HStack,
  Button,
  SimpleGrid,
  Spinner,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import { getStrategies, getStrategyTemplates, type StrategyTemplate } from '../../services/strategy'
import type { StrategyInfo } from '../../types/strategy'
import type { StrategyTypeCategory } from './StrategyTypeSelector'

interface StrategyPickerProps {
  strategyType: StrategyTypeCategory
  selectedSingle: string | null
  selectedCombo: string[]
  comboWeights: Record<string, number>
  hedgePrimary: string | null
  hedgeSecondary: string | null
  hedgeRatio: number
  onSingleChange: (id: string) => void
  onComboChange: (ids: string[], weights: Record<string, number>) => void
  onHedgeChange: (primary: string, secondary: string, ratio: number) => void
}

const StrategyPicker: React.FC<StrategyPickerProps> = ({
  strategyType,
  selectedSingle,
  selectedCombo,
  comboWeights,
  hedgePrimary,
  hedgeSecondary,
  hedgeRatio,
  onSingleChange,
  onComboChange,
  onHedgeChange,
}) => {
  const { t } = useTranslation()
  const [strategies, setStrategies] = useState<StrategyInfo[]>([])
  const [templates, setTemplates] = useState<StrategyTemplate[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const load = async () => {
      try {
        const [stratRes, tmplRes] = await Promise.all([
          getStrategies().catch(() => ({ strategies: [] })),
          getStrategyTemplates().catch(() => ({ templates: [] })),
        ])
        setStrategies(stratRes.strategies || [])
        setTemplates(tmplRes.templates || [])
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  const baseStrategies = strategies.filter((s) =>
    ['grid', 'dca', 'dca_enhanced', 'martingale', 'trend_following', 'mean_reversion', 'breakout'].includes(s.id)
  )

  const applyTemplate = (tmpl: StrategyTemplate) => {
    if (tmpl.type === 'combo' && tmpl.strategies?.length) {
      const weights: Record<string, number> = {}
      const w = tmpl.weights || tmpl.strategies.map(() => 1 / tmpl.strategies.length)
      tmpl.strategies.forEach((s, i) => {
        weights[s] = w[i] ?? 1 / tmpl.strategies.length
      })
      onComboChange(tmpl.strategies, weights)
    } else if (tmpl.type === 'hedge' && tmpl.strategies?.length >= 2) {
      onHedgeChange(tmpl.strategies[0], tmpl.strategies[1], 0.5)
    }
  }

  if (loading) {
    return (
      <Box textAlign="center" py={8}>
        <Spinner size="lg" />
      </Box>
    )
  }

  if (strategyType === 'single') {
    return (
      <VStack align="stretch" spacing={4}>
        <Text fontWeight="medium">{t('botCreate.strategyPicker.singleTitle')}</Text>
        <RadioGroup value={selectedSingle || ''} onChange={onSingleChange}>
          <Stack spacing={2}>
            {baseStrategies.map((s) => (
              <Radio key={s.id} value={s.id}>
                {t(`strategyNames.${s.id}`, { defaultValue: s.name })}
              </Radio>
            ))}
          </Stack>
        </RadioGroup>
      </VStack>
    )
  }

  if (strategyType === 'combo') {
    const comboTemplates = templates.filter((tmpl) => tmpl.type === 'combo')
    return (
      <VStack align="stretch" spacing={4}>
        {comboTemplates.length > 0 && (
          <Box>
            <Text fontWeight="medium" mb={2}>{t('botCreate.strategyPicker.templateTitle')}</Text>
            <HStack spacing={2} flexWrap="wrap">
              {comboTemplates.map((tmpl) => (
                <Button
                  key={tmpl.id}
                  size="sm"
                  variant="outline"
                  onClick={() => applyTemplate(tmpl)}
                >
                  {t(`botCreate.strategyTemplate.${tmpl.id}`, { defaultValue: tmpl.name })}
                </Button>
              ))}
            </HStack>
          </Box>
        )}
        <Text fontWeight="medium">{t('botCreate.strategyPicker.comboTitle')}</Text>
        <CheckboxGroup
          value={selectedCombo}
          onChange={(vals) => {
            const ids = (vals as string[]).filter(Boolean)
            const weights: Record<string, number> = {}
            ids.forEach((id, i) => {
              weights[id] = comboWeights[id] ?? 1 / ids.length
            })
            onComboChange(ids, weights)
          }}
        >
          <Stack spacing={2}>
            {baseStrategies.map((s) => (
              <Checkbox key={s.id} value={s.id}>
                {t(`strategyNames.${s.id}`, { defaultValue: s.name })}
              </Checkbox>
            ))}
          </Stack>
        </CheckboxGroup>
        {selectedCombo.length > 0 && (
          <Box>
            <Text fontSize="sm" mb={2}>{t('botCreate.strategyPicker.weights')}</Text>
            {selectedCombo.map((id) => (
              <HStack key={id} mb={2}>
                <Text fontSize="sm" w="120px">{t(`strategyNames.${id}`, id)}</Text>
                <Slider
                  min={0}
                  max={100}
                  value={(comboWeights[id] ?? 1 / selectedCombo.length) * 100}
                  onChange={(v) => {
                    const w = { ...comboWeights, [id]: v / 100 }
                    onComboChange(selectedCombo, w)
                  }}
                >
                  <SliderTrack><SliderFilledTrack /></SliderTrack>
                  <SliderThumb />
                </Slider>
                <Text fontSize="sm" w="40px">{Math.round((comboWeights[id] ?? 0) * 100)}%</Text>
              </HStack>
            ))}
          </Box>
        )}
      </VStack>
    )
  }

  if (strategyType === 'hedge') {
    const hedgeTemplates = templates.filter((tmpl) => tmpl.type === 'hedge')
    return (
      <VStack align="stretch" spacing={4}>
        {hedgeTemplates.length > 0 && (
          <Box>
            <Text fontWeight="medium" mb={2}>{t('botCreate.strategyPicker.templateTitle')}</Text>
            <HStack spacing={2} flexWrap="wrap">
              {hedgeTemplates.map((tmpl) => (
                <Button
                  key={tmpl.id}
                  size="sm"
                  variant="outline"
                  onClick={() => applyTemplate(tmpl)}
                >
                  {t(`botCreate.strategyTemplate.${tmpl.id}`, { defaultValue: tmpl.name })}
                </Button>
              ))}
            </HStack>
          </Box>
        )}
        <Text fontWeight="medium">{t('botCreate.strategyPicker.hedgeTitle')}</Text>
        <SimpleGrid columns={2} spacing={4}>
          <Box>
            <Text fontSize="sm" mb={1}>{t('botCreate.strategyPicker.hedgeFutures')}</Text>
            <RadioGroup value={hedgePrimary || ''} onChange={(v) => onHedgeChange(v, hedgeSecondary || '', hedgeRatio)}>
              <Stack spacing={1}>
                {baseStrategies.map((s) => (
                  <Radio key={s.id} value={s.id}>{t(`strategyNames.${s.id}`, s.name)}</Radio>
                ))}
              </Stack>
            </RadioGroup>
          </Box>
          <Box>
            <Text fontSize="sm" mb={1}>{t('botCreate.strategyPicker.hedgeSpot')}</Text>
            <RadioGroup value={hedgeSecondary || ''} onChange={(v) => onHedgeChange(hedgePrimary || '', v, hedgeRatio)}>
              <Stack spacing={1}>
                {baseStrategies.map((s) => (
                  <Radio key={s.id} value={s.id}>{t(`strategyNames.${s.id}`, s.name)}</Radio>
                ))}
              </Stack>
            </RadioGroup>
          </Box>
        </SimpleGrid>
        <Box>
          <Text fontSize="sm" mb={2}>{t('botCreate.strategyPicker.hedgeRatio')} ({Math.round(hedgeRatio * 100)}%)</Text>
          <Slider
            min={0}
            max={100}
            value={hedgeRatio * 100}
            onChange={(v) => onHedgeChange(hedgePrimary || '', hedgeSecondary || '', v / 100)}
          >
            <SliderTrack><SliderFilledTrack /></SliderTrack>
            <SliderThumb />
          </Slider>
        </Box>
      </VStack>
    )
  }

  return null
}

export default StrategyPicker
