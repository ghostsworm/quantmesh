import React, { useState, useEffect } from 'react'
import {
  Box,
  Button,
  Card,
  CardBody,
  FormControl,
  FormLabel,
  Heading,
  Input,
  NumberInput,
  NumberInputField,
  Select,
  Stepper,
  Step,
  StepDescription,
  StepIndicator,
  StepNumber,
  StepSeparator,
  StepStatus,
  StepTitle,
  useToast,
  VStack,
  Text,
  HStack,
  Spinner,
} from '@chakra-ui/react'
import { ChevronLeftIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { getConfig, type Config, type ExchangeConfig } from '../services/config'
import { getExchangeSymbols } from '../services/setup'
import { getExchanges, getBots, createBot, createBotGroup, getMarketTicker } from '../services/api'
import StrategyTypeSelector, { type StrategyTypeCategory } from './bot-create/StrategyTypeSelector'
import StrategyPicker from './bot-create/StrategyPicker'
import StrategyParamForm from './bot-create/StrategyParamForm'

const STEPS = 5

const BotCreateWizard: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const toast = useToast()
  const [step, setStep] = useState(0)
  const [config, setConfig] = useState<Config | null>(null)
  const [exchanges, setExchanges] = useState<string[]>([])
  const [symbols, setSymbols] = useState<string[]>([])

  const [strategyType, setStrategyType] = useState<StrategyTypeCategory | null>(null)
  const [selectedSingle, setSelectedSingle] = useState<string | null>(null)
  const [selectedCombo, setSelectedCombo] = useState<string[]>([])
  const [comboWeights, setComboWeights] = useState<Record<string, number>>({})
  const [hedgePrimary, setHedgePrimary] = useState<string | null>(null)
  const [hedgeSecondary, setHedgeSecondary] = useState<string | null>(null)
  const [hedgeRatio, setHedgeRatio] = useState(0.5)
  const [strategyParams, setStrategyParams] = useState<Record<string, Record<string, unknown>>>({})

  const [form, setForm] = useState<{
    exchange: string
    symbol: string
    market_type: 'spot' | 'futures'
    name: string
    price_interval: number
    order_quantity: number
    buy_window_size: number
    sell_window_size: number
    direction: string
  }>({
    exchange: 'binance',
    symbol: '',
    market_type: 'futures',
    name: '',
    price_interval: 2,
    order_quantity: 30,
    buy_window_size: 10,
    sell_window_size: 10,
    direction: 'LONG',
  })

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [marketTicker, setMarketTicker] = useState<{
    mark_price: number
    last_price: number
    high_24h: number
    low_24h: number
  } | null>(null)

  useEffect(() => {
    const load = async () => {
      try {
        const [cfg, exRes] = await Promise.all([
          getConfig().catch(() => null),
          getExchanges().catch(() => ({ exchanges: [] })),
        ])
        setConfig(cfg)
        setExchanges(exRes.exchanges || [])
      } catch (err) {
        console.error(err)
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  useEffect(() => {
    if (!form.exchange || !config?.exchanges) return
    const exCfg = (config.exchanges as Record<string, ExchangeConfig>)?.[form.exchange]
    if (!exCfg?.api_key || !exCfg?.secret_key) {
      setSymbols([])
      return
    }
    const load = async () => {
      try {
        const res = await getExchangeSymbols({
          exchange: form.exchange,
          api_key: String(exCfg.api_key),
          secret_key: String(exCfg.secret_key),
          passphrase: String(exCfg.passphrase || ''),
          market_type: form.market_type === 'spot' ? 'spot' : 'futures',
        })
        setSymbols(res.symbols || [])
      } catch {
        setSymbols([])
      }
    }
    load()
  }, [form.exchange, form.market_type, config?.exchanges])

  useEffect(() => {
    if (!form.exchange || !form.symbol) {
      setMarketTicker(null)
      return
    }
    const load = async () => {
      try {
        const res = await getMarketTicker(form.exchange, form.symbol, form.market_type)
        setMarketTicker({
          mark_price: res.mark_price,
          last_price: res.last_price,
          high_24h: res.high_24h,
          low_24h: res.low_24h,
        })
      } catch {
        setMarketTicker(null)
      }
    }
    load()
  }, [form.exchange, form.symbol, form.market_type])

  const getStrategyIds = (): string[] => {
    if (strategyType === 'single' && selectedSingle) return [selectedSingle]
    if (strategyType === 'combo' && selectedCombo.length > 0) return selectedCombo
    if (strategyType === 'hedge') {
      const ids: string[] = []
      if (hedgePrimary) ids.push(hedgePrimary)
      if (hedgeSecondary) ids.push(hedgeSecondary)
      return ids
    }
    return []
  }

  const buildStrategies = () => {
    const ids = getStrategyIds()
    if (ids.length === 0) return [{ type: 'grid', weight: 1.0, config: {} }]
    if (strategyType === 'combo') {
      const total = ids.reduce((s, id) => s + (comboWeights[id] ?? 1 / ids.length), 0) || 1
      return ids.map((id) => ({
        type: id,
        weight: (comboWeights[id] ?? 1 / ids.length) / total,
        config: strategyParams[id] || {},
      }))
    }
    return ids.map((id) => ({
      type: id,
      weight: 1 / ids.length,
      config: strategyParams[id] || {},
    }))
  }

  const handleSubmit = async () => {
    if (!form.exchange || !form.symbol) {
      toast({ title: t('botCreate.fillRequired'), status: 'error', duration: 3000 })
      return
    }
    setSaving(true)
    try {
      const baseReq = {
        exchange: form.exchange,
        symbol: form.symbol,
        market_type: form.market_type,
        name: form.name?.trim() || undefined,
        price_interval: form.price_interval ?? 2,
        order_quantity: form.order_quantity ?? 30,
        buy_window_size: form.buy_window_size ?? 10,
        sell_window_size: form.sell_window_size ?? 10,
        direction: form.direction || 'LONG',
        strategies: buildStrategies(),
      }

      if (strategyType === 'hedge') {
        await createBotGroup({
          name: form.name?.trim() || `${form.symbol} Hedge`,
          type: 'futures_spot_hedge',
          hedge_config: { hedge_ratio: hedgeRatio, rebalance_interval: 3600 },
          futures_bot: { ...baseReq, market_type: 'futures' },
          spot_bot: { ...baseReq, market_type: 'spot' },
        })
        toast({ title: t('botCreate.success'), status: 'success', duration: 2000 })
        navigate('/bots')
      } else {
        await createBot(baseReq)
        toast({ title: t('botCreate.success'), status: 'success', duration: 2000 })
        const botsRes = await getBots()
        const bot = (botsRes.bots || []).find(
          (b) =>
            b.exchange === form.exchange &&
            b.symbol === form.symbol &&
            b.market_type === (form.market_type || 'futures')
        )
        if (bot) navigate(`/bots/${bot.bot_id}`)
        else navigate('/bots')
      }
    } catch (err) {
      const e = err as Error & { errorKey?: string; groupName?: string }
      const msg =
        e.errorKey
          ? t(e.errorKey, { groupName: e.groupName ?? '' })
          : t('botCreate.failed')
      toast({ title: msg, status: 'error', duration: 4000 })
    } finally {
      setSaving(false)
    }
  }

  const canProceedStep0 = strategyType !== null
  const canProceedStep1 =
    (strategyType === 'single' && selectedSingle) ||
    (strategyType === 'combo' && selectedCombo.length > 0) ||
    (strategyType === 'hedge' && hedgePrimary && hedgeSecondary)
  const canProceedStep2 = form.exchange && form.symbol

  if (loading) {
    return (
      <Box textAlign="center" py={12}>
        <Spinner size="lg" />
      </Box>
    )
  }

  const stepTitles = [
    t('botCreate.step0Title'),
    t('botCreate.step1Title'),
    t('botCreate.step2Title'),
    t('botCreate.step3Title'),
    t('botCreate.step4Title'),
  ]
  const stepDescs = [
    t('botCreate.step0Desc'),
    t('botCreate.step1Desc'),
    t('botCreate.step2Desc'),
    t('botCreate.step3Desc'),
    t('botCreate.step4Desc'),
  ]

  return (
    <Box>
      <Button as={Link} to="/bots" leftIcon={<ChevronLeftIcon />} variant="ghost" size="sm" mb={4}>
        {t('common.back')}
      </Button>
      <Heading size="lg" mb={6}>
        {t('botCreate.title')}
      </Heading>

      <Stepper index={step} mb={8}>
        {Array.from({ length: STEPS }, (_, i) => (
          <Step key={i}>
            <StepIndicator>
              <StepStatus complete={<StepNumber />} incomplete={<StepNumber />} active={<StepNumber />} />
            </StepIndicator>
            <Box flexShrink="0">
              <StepTitle>{stepTitles[i]}</StepTitle>
              <StepDescription>{stepDescs[i]}</StepDescription>
            </Box>
            <StepSeparator />
          </Step>
        ))}
      </Stepper>

      <Card>
        <CardBody>
          {step === 0 && (
            <StrategyTypeSelector value={strategyType} onChange={setStrategyType} />
          )}

          {step === 1 && (
            <StrategyPicker
              strategyType={strategyType!}
              selectedSingle={selectedSingle}
              selectedCombo={selectedCombo}
              comboWeights={comboWeights}
              hedgePrimary={hedgePrimary}
              hedgeSecondary={hedgeSecondary}
              hedgeRatio={hedgeRatio}
              onSingleChange={setSelectedSingle}
              onComboChange={(ids, w) => {
                setSelectedCombo(ids)
                setComboWeights(w)
              }}
              onHedgeChange={(p, s, r) => {
                setHedgePrimary(p)
                setHedgeSecondary(s)
                setHedgeRatio(r)
              }}
            />
          )}

          {step === 2 && (
            <VStack spacing={4} align="stretch">
              <FormControl isRequired>
                <FormLabel>{t('configSetup.exchange')}</FormLabel>
                <Select
                  value={form.exchange || ''}
                  onChange={(e) => setForm((f) => ({ ...f, exchange: e.target.value }))}
                >
                  {exchanges.map((ex) => (
                    <option key={ex} value={ex}>{ex.toUpperCase()}</option>
                  ))}
                </Select>
              </FormControl>
              {strategyType !== 'hedge' && (
                <FormControl isRequired>
                  <FormLabel>{t('botCreate.marketType')}</FormLabel>
                  <Select
                    value={form.market_type || 'futures'}
                    onChange={(e) => setForm((f) => ({ ...f, market_type: e.target.value as 'spot' | 'futures' }))}
                  >
                    <option value="futures">{t('symbolManager.futures')}</option>
                    <option value="spot">{t('symbolManager.spot')}</option>
                  </Select>
                </FormControl>
              )}
              {strategyType === 'hedge' && (
                <Text fontSize="sm" color="gray.600">{t('botCreate.hedgeMarketHint')}</Text>
              )}
              <FormControl isRequired>
                <FormLabel>{t('configSetup.symbol')}</FormLabel>
                <Select
                  value={form.symbol || ''}
                  onChange={(e) => setForm((f) => ({ ...f, symbol: e.target.value }))}
                  placeholder={t('botCreate.selectSymbol')}
                >
                  {symbols.map((s) => (
                    <option key={s} value={s}>{s}</option>
                  ))}
                </Select>
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.botName')}</FormLabel>
                <Input
                  value={form.name || ''}
                  onChange={(e) => setForm((f) => ({ ...f, name: e.target.value.trim() }))}
                  placeholder={t('botCreate.botNamePlaceholder')}
                />
              </FormControl>
              {marketTicker && (
                <Box p={3} bg="blue.50" borderRadius="md" fontSize="sm">
                  <Text fontWeight="medium" mb={2}>{t('botCreate.marketData')}</Text>
                  <HStack spacing={4} flexWrap="wrap">
                    <Text><strong>{t('botCreate.markPrice')}:</strong> {marketTicker.mark_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                    <Text><strong>{t('botCreate.last24hHigh')}:</strong> {marketTicker.high_24h.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                    <Text><strong>{t('botCreate.last24hLow')}:</strong> {marketTicker.low_24h.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                  </HStack>
                </Box>
              )}
            </VStack>
          )}

          {step === 3 && (
            <VStack spacing={4} align="stretch">
              {marketTicker && (
                <Box p={3} bg="blue.50" borderRadius="md" fontSize="sm">
                  <Text fontWeight="medium" mb={2}>{t('botCreate.marketData')}</Text>
                  <HStack spacing={4} flexWrap="wrap">
                    <Text><strong>{t('botCreate.markPrice')}:</strong> {marketTicker.mark_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                    <Text><strong>{t('botCreate.last24hHigh')}:</strong> {marketTicker.high_24h.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                    <Text><strong>{t('botCreate.last24hLow')}:</strong> {marketTicker.low_24h.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                  </HStack>
                </Box>
              )}
              <StrategyParamForm
                strategyIds={getStrategyIds()}
                value={strategyParams}
                onChange={setStrategyParams}
              />
              <FormControl>
                <FormLabel>{t('botCreate.priceInterval')}</FormLabel>
                <NumberInput
                  value={form.price_interval ?? 2}
                  min={0.01}
                  step={0.1}
                  onChange={(_, v) => setForm((f) => ({ ...f, price_interval: v }))}
                >
                  <NumberInputField />
                </NumberInput>
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.orderQuantity')}</FormLabel>
                <NumberInput
                  value={form.order_quantity ?? 30}
                  min={1}
                  onChange={(_, v) => setForm((f) => ({ ...f, order_quantity: v }))}
                >
                  <NumberInputField />
                </NumberInput>
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.buyWindowSize')}</FormLabel>
                <NumberInput
                  value={form.buy_window_size ?? 10}
                  min={1}
                  onChange={(_, v) => setForm((f) => ({ ...f, buy_window_size: v }))}
                >
                  <NumberInputField />
                </NumberInput>
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.sellWindowSize')}</FormLabel>
                <NumberInput
                  value={form.sell_window_size ?? 10}
                  min={1}
                  onChange={(_, v) => setForm((f) => ({ ...f, sell_window_size: v }))}
                >
                  <NumberInputField />
                </NumberInput>
              </FormControl>
            </VStack>
          )}

          {step === 4 && (
            <VStack spacing={4} align="stretch">
              <Text>{t('botCreate.reviewDesc')}</Text>
              <Box bg="gray.50" p={4} borderRadius="md" fontSize="sm">
                {form.name && <Text><strong>{t('botCreate.botName')}:</strong> {form.name}</Text>}
                <Text><strong>{t('configSetup.exchange')}:</strong> {form.exchange}</Text>
                <Text><strong>{t('configSetup.symbol')}:</strong> {form.symbol}</Text>
                <Text><strong>{t('botCreate.strategyTypeLabel')}:</strong> {t(`botCreate.strategyType.${strategyType}.title`)}</Text>
                {strategyType === 'single' && selectedSingle && (
                  <Text><strong>{t('botCreate.strategyLabel')}:</strong> {t(`strategyNames.${selectedSingle}`, selectedSingle)}</Text>
                )}
                {strategyType === 'combo' && (
                  <Text><strong>{t('botCreate.strategyLabel')}:</strong> {selectedCombo.map((s) => t(`strategyNames.${s}`, s)).join(', ')}</Text>
                )}
                {strategyType === 'hedge' && (
                  <Text><strong>{t('botCreate.strategyLabel')}:</strong> {t('botCreate.hedgeMode')} ({hedgePrimary} + {hedgeSecondary})</Text>
                )}
                {getStrategyIds().map((sid) => {
                  const params = strategyParams[sid]
                  if (!params || Object.keys(params).length === 0) return null
                  return (
                    <Box key={sid} mt={2} pl={2} borderLeftWidth={2} borderColor="gray.300">
                      <Text fontWeight="medium" mb={1}>{t(`strategyNames.${sid}`, sid)}</Text>
                      {Object.entries(params).map(([k, v]) => (
                        <Text key={k} pl={2}>
                          <strong>{t(`strategyParams.${sid}.${k}.name`, { defaultValue: k })}:</strong> {String(v)}
                        </Text>
                      ))}
                    </Box>
                  )
                })}
                <Text mt={2}><strong>{t('botCreate.priceInterval')}:</strong> {form.price_interval}</Text>
                <Text><strong>{t('botCreate.orderQuantity')}:</strong> {form.order_quantity}</Text>
                <Text><strong>{t('botCreate.buyWindowSize')}:</strong> {form.buy_window_size}</Text>
                <Text><strong>{t('botCreate.sellWindowSize')}:</strong> {form.sell_window_size}</Text>
              </Box>
            </VStack>
          )}

          <HStack mt={6} justify="space-between">
            <Button isDisabled={step === 0} variant="ghost" onClick={() => setStep((s) => Math.max(0, s - 1))}>
              {t('common.back')}
            </Button>
            {step < STEPS - 1 ? (
              <Button
                colorScheme="blue"
                onClick={() => setStep((s) => Math.min(STEPS - 1, s + 1))}
                isDisabled={
                  (step === 0 && !canProceedStep0) ||
                  (step === 1 && !canProceedStep1) ||
                  (step === 2 && !canProceedStep2)
                }
              >
                {t('botCreate.next')}
              </Button>
            ) : (
              <Button colorScheme="green" onClick={handleSubmit} isLoading={saving}>
                {t('botCreate.create')}
              </Button>
            )}
          </HStack>
        </CardBody>
      </Card>
    </Box>
  )
}

export default BotCreateWizard
