import React, { useEffect, useState } from 'react'
import {
  Box,
  FormControl,
  FormLabel,
  Input,
  Select,
  Switch,
  VStack,
  Spinner,
} from '@chakra-ui/react'
import DecimalNumberInput from '../DecimalNumberInput'
import { useTranslation } from 'react-i18next'
import { getStrategyDetail } from '../../services/strategy'

interface StrategyParamFormProps {
  strategyIds: string[]
  value: Record<string, Record<string, unknown>>
  onChange: (params: Record<string, Record<string, unknown>>) => void
}

interface ParamDef {
  name: string
  type: string
  default: unknown
  min?: number
  max?: number
  description?: string
  required?: boolean
  displayOrder?: number
}

const StrategyParamForm: React.FC<StrategyParamFormProps> = ({ strategyIds, value, onChange }) => {
  const { t } = useTranslation()
  const [paramsByStrategy, setParamsByStrategy] = useState<Record<string, ParamDef[]>>({})
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const load = async () => {
      const result: Record<string, ParamDef[]> = {}
      for (const id of strategyIds) {
        try {
          const res = await getStrategyDetail(id)
          const params = (res.strategy?.parameters || []) as Array<{
            name: string
            type: string
            default?: unknown
            min?: number
            max?: number
            description?: string
            required?: boolean
            displayOrder?: number
          }>
          result[id] = params
            .map((p) => ({
              name: p.name,
              type: p.type || 'number',
              default: p.default,
              min: p.min,
              max: p.max,
              description: p.description,
              required: p.required,
              displayOrder: p.displayOrder ?? 99,
            }))
            .sort((a, b) => (a.displayOrder ?? 99) - (b.displayOrder ?? 99))
        } catch {
          result[id] = []
        }
      }
      setParamsByStrategy(result)
      setLoading(false)
    }
    load()
  }, [strategyIds.join(',')])

  const updateParam = (strategyId: string, paramName: string, val: unknown) => {
    const next = { ...value }
    if (!next[strategyId]) next[strategyId] = {}
    next[strategyId] = { ...next[strategyId], [paramName]: val }
    onChange(next)
  }

  if (loading) {
    return (
      <Box textAlign="center" py={4}>
        <Spinner size="md" />
      </Box>
    )
  }

  const hasParams = Object.values(paramsByStrategy).some((p) => p.length > 0)
  if (!hasParams) return null

  return (
    <VStack align="stretch" spacing={4}>
      {strategyIds.map((sid) => {
        const params = paramsByStrategy[sid] || []
        if (params.length === 0) return null
        return (
          <Box key={sid} p={4} bg="gray.50" borderRadius="md">
            <FormLabel fontWeight="medium">{t(`strategyNames.${sid}`, sid)}</FormLabel>
            <VStack align="stretch" spacing={3} mt={2}>
              {params.map((p) => {
                const current = (value[sid] || {})[p.name] ?? p.default
                const key = `${sid}.${p.name}`
                return (
                  <FormControl key={key}>
                    <FormLabel fontSize="sm">{t(`botCreate.strategyParams.${p.name}`, { defaultValue: p.name })}</FormLabel>
                    {p.type === 'number' && (
                      <DecimalNumberInput
                        value={current !== undefined && current !== null ? current : (p.default ?? 0)}
                        min={p.min}
                        max={p.max}
                        onChange={(v) => updateParam(sid, p.name, v)}
                      />
                    )}
                    {p.type === 'boolean' && (
                      <Switch
                        isChecked={Boolean(current)}
                        onChange={(e) => updateParam(sid, p.name, e.target.checked)}
                      />
                    )}
                    {p.type === 'string' && (
                      <Input
                        value={String(current ?? '')}
                        onChange={(e) => updateParam(sid, p.name, e.target.value)}
                      />
                    )}
                    {p.type === 'select' && (
                      <Select
                        value={String(current ?? '')}
                        onChange={(e) => updateParam(sid, p.name, e.target.value)}
                      >
                        {/* Options would come from param schema if available */}
                      </Select>
                    )}
                  </FormControl>
                )
              })}
            </VStack>
          </Box>
        )
      })}
    </VStack>
  )
}

export default StrategyParamForm
