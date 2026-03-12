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
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalFooter,
  ModalBody,
  ModalCloseButton,
  useDisclosure,
  Switch,
  Divider,
} from '@chakra-ui/react'
import { ChevronLeftIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { getConfig, type Config, type ExchangeConfig } from '../services/config'
import { getExchangeSymbols } from '../services/setup'
import { getExchanges, getBots, createBot, createBotGroup, getMarketTicker } from '../services/api'
import DecimalNumberInput from './DecimalNumberInput'
import StrategyTypeSelector, { type StrategyTypeCategory } from './bot-create/StrategyTypeSelector'
import StrategyPicker from './bot-create/StrategyPicker'
import StrategyParamForm from './bot-create/StrategyParamForm'
import StrategyTemplates from './StrategyTemplates'
import { getStrategyTemplateById } from '../services/strategy'

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
  const [hedgeShortNotionalRatio, setHedgeShortNotionalRatio] = useState<number | string>(0.25)
  const [hedgeTriggerLayers, setHedgeTriggerLayers] = useState<number | string>(3)
  const [hedgeRebalanceInterval, setHedgeRebalanceInterval] = useState<number | string>(3600)
  const [strategyParams, setStrategyParams] = useState<Record<string, Record<string, unknown>>>({})

  const [form, setForm] = useState<{
    exchange: string
    symbol: string
    market_type: 'spot' | 'futures'
    name: string
    price_interval: number | string
    order_quantity: number | string
    buy_window_size: number | string
    sell_window_size: number | string
    direction: string
    enable_risk_control?: boolean
    stop_loss_ratio?: number | string
    take_profit_trigger_ratio?: number | string
    enable_trend_filter?: boolean
    rocket_tiered_grid_enabled?: boolean
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
    enable_risk_control: false,
    stop_loss_ratio: 0.2,
    take_profit_trigger_ratio: 0.08,
    enable_trend_filter: false,
    rocket_tiered_grid_enabled: false,
  })

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [marketTicker, setMarketTicker] = useState<{
    mark_price: number
    last_price: number
    high_24h: number
    low_24h: number
  } | null>(null)

  // Template modal
  const { isOpen: isTemplateModalOpen, onOpen: onTemplateModalOpen, onClose: onTemplateModalClose } = useDisclosure()

  const handleSelectTemplate = async (templateId: string) => {
    try {
      const template = await getStrategyTemplateById(templateId)
      if (!template) return

      // Apply template configuration
      if (template.strategy_type === 'single') {
        setStrategyType('single')
        setSelectedSingle(template.strategy_type)
      } else if (template.strategy_type === 'combo') {
        setStrategyType('combo')
        // Handle combo template
        const strategies = template.config?.strategies as Array<{ type: string; weight: number }> || []
        const ids = strategies.map(s => s.type)
        const weights: Record<string, number> = {}
        strategies.forEach(s => {
          weights[s.type] = s.weight
        })
        setSelectedCombo(ids)
        setComboWeights(weights)
      } else if (template.strategy_type === 'hedge' && template.config?.strategies?.length >= 2) {
        setStrategyType('hedge')
        const strategies = template.config.strategies as string[]
        setHedgePrimary(strategies[0])
        setHedgeSecondary(strategies[1])
        setHedgeRatio(template.config.hedge_ratio ?? 0.5)
        if (strategies[1] === 'spot_short' || strategies[1] === 'spot_long' || strategies[1] === 'futures_short') {
          setHedgeShortNotionalRatio(template.config.short_notional_ratio ?? 0.25)
          setHedgeTriggerLayers(template.config.hedge_trigger_layers ?? 3)
          setHedgeRebalanceInterval(template.config.rebalance_interval ?? 3600)
        }
        if (strategies[1] === 'spot_long') {
          setForm(f => ({ ...f, direction: 'SHORT' }))
        }
      }

      // Apply form defaults from template
      if (template.config) {
        const cfg = template.config as any
        if (cfg.price_interval) setForm(f => ({ ...f, price_interval: cfg.price_interval }))
        if (cfg.order_quantity) setForm(f => ({ ...f, order_quantity: cfg.order_quantity }))
        if (cfg.buy_window_size) setForm(f => ({ ...f, buy_window_size: cfg.buy_window_size }))
        if (cfg.sell_window_size) setForm(f => ({ ...f, sell_window_size: cfg.sell_window_size }))
        if (cfg.direction) setForm(f => ({ ...f, direction: cfg.direction }))
      }

      // Apply strategy params from template
      if (template.params) {
        const params: Record<string, Record<string, unknown>> = {}
        for (const [key, param] of Object.entries(template.params)) {
          params[key] = { [key]: param.default }
        }
        setStrategyParams(params)
      }

      onTemplateModalClose()
      // Move to step 2 after selecting template
      setStep(2)
    } catch (err) {
      console.error('Failed to load template:', err)
      toast({ title: t('template.loadFailed'), status: 'error', duration: 3000 })
    }
  }

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

  const normalizeConfigForApi = (cfg: Record<string, unknown>): Record<string, unknown> => {
    const out: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(cfg)) {
      if (typeof v === 'string' && v !== '' && !Number.isNaN(parseFloat(v))) {
        out[k] = parseFloat(v)
      } else if (typeof v === 'object' && v !== null && !Array.isArray(v)) {
        out[k] = normalizeConfigForApi(v as Record<string, unknown>)
      } else {
        out[k] = v
      }
    }
    return out
  }

  const buildStrategies = () => {
    const ids = getStrategyIds()
    if (ids.length === 0) return [{ type: 'grid', weight: 1.0, config: {} }]
    if (strategyType === 'combo') {
      const total = ids.reduce((s, id) => s + (comboWeights[id] ?? 1 / ids.length), 0) || 1
      return ids.map((id) => ({
        type: id,
        weight: (comboWeights[id] ?? 1 / ids.length) / total,
        config: normalizeConfigForApi((strategyParams[id] || {}) as Record<string, unknown>),
      }))
    }
    return ids.map((id) => ({
      type: id,
      weight: 1 / ids.length,
      config: normalizeConfigForApi((strategyParams[id] || {}) as Record<string, unknown>),
    }))
  }

  const handleSubmit = async () => {
    if (!form.exchange || !form.symbol) {
      toast({ title: t('botCreate.fillRequired'), status: 'error', duration: 3000 })
      return
    }
    setSaving(true)
    try {
      const toNum = (v: number | string | undefined, def: number) => {
        if (typeof v === 'number' && !Number.isNaN(v)) return v
        if (v == null || v === '') return def
        const n = parseFloat(String(v))
        return Number.isNaN(n) ? def : n
      }
      const baseReq = {
        exchange: form.exchange,
        symbol: form.symbol,
        market_type: form.market_type,
        name: form.name?.trim() || undefined,
        price_interval: toNum(form.price_interval, 2),
        order_quantity: toNum(form.order_quantity, 30),
        buy_window_size: toNum(form.buy_window_size, 10),
        sell_window_size: toNum(form.sell_window_size, 10),
        direction: form.direction || 'LONG',
        strategies: buildStrategies(),
        // 网格风控配置
        grid_risk_control_enabled: form.enable_risk_control,
        grid_risk_control_stop_loss_ratio: form.enable_risk_control ? toNum(form.stop_loss_ratio, 0.2) : undefined,
        grid_risk_control_take_profit_trigger_ratio: form.enable_risk_control ? toNum(form.take_profit_trigger_ratio, 0.08) : undefined,
        grid_risk_control_trend_filter_enabled: form.enable_trend_filter,
        // 三级火箭网格
        rocket_tiered_grid: form.rocket_tiered_grid_enabled
          ? {
              enabled: true,
              tiers: [
                { filled_threshold: 4, interval: 100, profit_spread: 100 },
                { filled_threshold: 8, interval: 300, profit_spread: 300 },
                { filled_threshold: 0, interval: 600, profit_spread: 600 },
              ],
            }
          : undefined,
      }

      if (strategyType === 'hedge') {
        const isSpotGridFuturesHedge = hedgePrimary === 'grid' && hedgeSecondary === 'futures_short'
        const groupType = isSpotGridFuturesHedge ? 'spot_grid_futures_hedge' : 'futures_spot_hedge'
        const shortNotional = toNum(hedgeShortNotionalRatio, 0.25)
        const triggerLayers = Math.max(1, Math.min(20, Math.round(toNum(hedgeTriggerLayers, 3))))
        const rebalanceInt = Math.max(60, Math.round(toNum(hedgeRebalanceInterval, 3600)))
        const hedgeConfig = {
          hedge_ratio: hedgeRatio,
          short_notional_ratio: shortNotional,
          hedge_trigger_layers: triggerLayers,
          rebalance_interval: rebalanceInt,
        }
        const futuresStrategies = hedgePrimary
          ? [{ type: hedgePrimary, weight: 1.0, config: normalizeConfigForApi((strategyParams[hedgePrimary] || {}) as Record<string, unknown>) }]
          : [{ type: 'grid', weight: 1.0, config: {} }]
        const spotStrategies = hedgeSecondary
          ? [{ type: hedgeSecondary, weight: 1.0, config: normalizeConfigForApi((strategyParams[hedgeSecondary] || {}) as Record<string, unknown>) }]
          : [{ type: 'grid', weight: 1.0, config: {} }]
        await createBotGroup({
          name: form.name?.trim() || `${form.symbol} Hedge`,
          type: groupType,
          hedge_config: hedgeConfig,
          futures_bot: isSpotGridFuturesHedge
            ? { ...baseReq, market_type: 'futures', strategies: spotStrategies }
            : { ...baseReq, market_type: 'futures', strategies: futuresStrategies },
          spot_bot: isSpotGridFuturesHedge
            ? { ...baseReq, market_type: 'spot', strategies: futuresStrategies }
            : { ...baseReq, market_type: 'spot', strategies: spotStrategies },
        })
        toast({ title: t('botCreate.success'), status: 'success', duration: 2000 })
        navigate('/bots')
      } else {
        const res = await createBot(baseReq)
        toast({ title: t('botCreate.success'), status: 'success', duration: 2000 })
        if (res?.bot_id) navigate(`/bots/${res.bot_id}`)
        else navigate('/bots')
      }
    } catch (err) {
      const e = err as Error & { errorKey?: string; groupName?: string; botId?: string }
      const msg =
        e.errorKey
          ? t(e.errorKey, { groupName: e.groupName ?? '', botId: e.botId ?? '' })
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
      <HStack justify="space-between" align="center" mb={6}>
        <Heading size="lg">
          {t('botCreate.title')}
        </Heading>
        <Button
          leftIcon={<ChevronLeftIcon />}
          variant="outline"
          size="sm"
          onClick={onTemplateModalOpen}
        >
          {t('template.selectTemplate')}
        </Button>
      </HStack>

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
              {/* 网格方向放在最前面，先选方向再配网格 */}
              <FormControl>
                <FormLabel>{t('botCreate.direction')}</FormLabel>
                <Select
                  value={form.direction || 'LONG'}
                  onChange={(e) => setForm((f) => ({ ...f, direction: e.target.value }))}
                >
                  <option value="LONG">{t('botDetail.strategy.directionLong')}</option>
                  <option value="SHORT">{t('botDetail.strategy.directionShort')}</option>
                  <option value="BOTH">{t('botDetail.strategy.directionBoth')}</option>
                </Select>
                <Text fontSize="xs" color="gray.500" mt={1}>
                  {t('botCreate.directionHint')}
                </Text>
              </FormControl>
              <StrategyParamForm
                strategyIds={getStrategyIds()}
                value={strategyParams}
                onChange={setStrategyParams}
              />
              {strategyType === 'hedge' && (hedgeSecondary === 'spot_short' || hedgeSecondary === 'spot_long' || hedgeSecondary === 'futures_short') && (
                <>
                  <Divider my={2} />
                  {hedgeSecondary === 'spot_short' && (form.direction === 'SHORT' || form.direction === 'BOTH') && (
                    <Box p={3} bg="orange.50" borderRadius="md" fontSize="sm" borderWidth={1} borderColor="orange.200">
                      <Text color="orange.800">
                        {form.direction === 'SHORT'
                          ? t('botCreate.hedgeSpotShortShortWarning')
                          : t('botCreate.hedgeSpotShortBothWarning')}
                      </Text>
                    </Box>
                  )}
                  {hedgeSecondary === 'spot_long' && form.direction !== 'SHORT' && (
                    <Box p={3} bg="orange.50" borderRadius="md" fontSize="sm" borderWidth={1} borderColor="orange.200">
                      <Text color="orange.800">{t('botCreate.hedgeSpotLongDirectionWarning')}</Text>
                    </Box>
                  )}
                  <Text fontWeight="medium" fontSize="sm">
                    {hedgeSecondary === 'futures_short'
                      ? t('botCreate.hedgeSpotGridFuturesConfig')
                      : hedgeSecondary === 'spot_short'
                        ? (form.direction === 'LONG' ? t('botCreate.hedgeSpotShortConfig') : t('botCreate.hedgeSpotShortConfigLongOnly'))
                        : t('botCreate.hedgeSpotLongConfig')}
                  </Text>
                  <FormControl>
                    <FormLabel>{hedgeSecondary === 'spot_long' ? t('botCreate.longNotionalRatio') : t('botCreate.shortNotionalRatio')}</FormLabel>
                    <DecimalNumberInput
                      value={hedgeShortNotionalRatio ?? 0.25}
                      min={0.05}
                      max={1}
                      step={0.05}
                      precision={2}
                      onChange={(v) => setHedgeShortNotionalRatio(v ?? 0.25)}
                    />
                    <Text fontSize="xs" color="gray.500" mt={1}>
                      {hedgeSecondary === 'spot_long' ? t('botCreate.longNotionalRatioHint') : t('botCreate.shortNotionalRatioHint')}
                    </Text>
                  </FormControl>
                  <FormControl>
                    <FormLabel>{t('botCreate.hedgeTriggerLayers')}</FormLabel>
                    <DecimalNumberInput
                      value={hedgeTriggerLayers ?? 3}
                      min={1}
                      max={20}
                      step={1}
                      precision={0}
                      onChange={(v) => setHedgeTriggerLayers(v ?? 3)}
                    />
                    <Text fontSize="xs" color="gray.500" mt={1}>
                      {t('botCreate.hedgeTriggerLayersHint')}
                    </Text>
                  </FormControl>
                  <FormControl>
                    <FormLabel>{t('botCreate.hedgeRebalanceInterval')}</FormLabel>
                    <DecimalNumberInput
                      value={hedgeRebalanceInterval ?? 3600}
                      min={60}
                      step={60}
                      precision={0}
                      onChange={(v) => setHedgeRebalanceInterval(v ?? 3600)}
                    />
                    <Text fontSize="xs" color="gray.500" mt={1}>
                      {t('botCreate.hedgeRebalanceIntervalHint')}
                    </Text>
                  </FormControl>
                </>
              )}
              <FormControl>
                <FormLabel>{t('botCreate.priceInterval')}</FormLabel>
                <DecimalNumberInput
                  value={form.price_interval ?? 2}
                  min={0.01}
                  step={0.1}
                  precision={4}
                  onChange={(v) => setForm((f) => ({ ...f, price_interval: v ?? 2 }))}
                />
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.orderQuantity')}</FormLabel>
                <DecimalNumberInput
                  value={form.order_quantity ?? 30}
                  min={0.01}
                  step={0.01}
                  precision={2}
                  onChange={(v) => setForm((f) => ({ ...f, order_quantity: v ?? 30 }))}
                />
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.buyWindowSize')}</FormLabel>
                <DecimalNumberInput
                  value={form.buy_window_size ?? 10}
                  min={0.01}
                  step={0.01}
                  precision={2}
                  onChange={(v) => setForm((f) => ({ ...f, buy_window_size: v ?? 10 }))}
                />
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.sellWindowSize')}</FormLabel>
                <DecimalNumberInput
                  value={form.sell_window_size ?? 10}
                  min={0.01}
                  step={0.01}
                  precision={2}
                  onChange={(v) => setForm((f) => ({ ...f, sell_window_size: v ?? 10 }))}
                />
              </FormControl>

              <Divider my={2} />
              <Text fontWeight="medium" fontSize="sm">{t('botCreate.advancedSettings')}</Text>

              {/* 三级火箭网格 */}
              <FormControl>
                <FormLabel>{t('botCreate.rocketTieredGrid')}</FormLabel>
                <Switch
                  isChecked={form.rocket_tiered_grid_enabled || false}
                  onChange={(e) => setForm((f) => ({ ...f, rocket_tiered_grid_enabled: e.target.checked }))}
                />
                <Text fontSize="xs" color="gray.500" mt={1}>
                  {t('botCreate.rocketTieredGridHint')}
                </Text>
              </FormControl>

              {/* 网格风控设置 - 简化版，只显示关键选项 */}
              <Text fontWeight="medium" fontSize="sm" mt={2}>{t('botCreate.gridRiskControl')}</Text>
              <FormControl>
                <FormLabel>{t('botCreate.enableRiskControl')}</FormLabel>
                <Switch
                  isChecked={form.enable_risk_control || false}
                  onChange={(e) => setForm((f) => ({ ...f, enable_risk_control: e.target.checked }))}
                />
                <Text fontSize="xs" color="gray.500" mt={1}>
                  {t('botCreate.enableRiskControlHint')}
                </Text>
              </FormControl>

              <FormControl>
                <FormLabel>{t('botCreate.enableTrendFilter')}</FormLabel>
                <Switch
                  isChecked={form.enable_trend_filter || false}
                  onChange={(e) => setForm((f) => ({ ...f, enable_trend_filter: e.target.checked }))}
                />
                <Text fontSize="xs" color="gray.500" mt={1}>
                  {t('botCreate.enableTrendFilterHint')}
                </Text>
              </FormControl>

              {form.enable_risk_control && (
                <>
                  <FormControl>
                    <FormLabel>{t('botCreate.stopLossRatio')}</FormLabel>
                    <DecimalNumberInput
                      value={form.stop_loss_ratio ?? 0.2}
                      min={0}
                      max={1}
                      step={0.01}
                      precision={2}
                      onChange={(v) => setForm((f) => ({ ...f, stop_loss_ratio: v ?? 0.2 }))}
                    />
                    <Text fontSize="xs" color="gray.500" mt={1}>
                      {t('botCreate.stopLossRatioHint')}
                    </Text>
                  </FormControl>

                  <FormControl>
                    <FormLabel>{t('botCreate.takeProfitTriggerRatio')}</FormLabel>
                    <DecimalNumberInput
                      value={form.take_profit_trigger_ratio ?? 0.08}
                      min={0}
                      max={1}
                      step={0.01}
                      precision={2}
                      onChange={(v) => setForm((f) => ({ ...f, take_profit_trigger_ratio: v ?? 0.08 }))}
                    />
                    <Text fontSize="xs" color="gray.500" mt={1}>
                      {t('botCreate.takeProfitTriggerRatioHint')}
                    </Text>
                  </FormControl>
                </>
              )}
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
                {strategyType === 'hedge' && (hedgeSecondary === 'spot_short' || hedgeSecondary === 'spot_long' || hedgeSecondary === 'futures_short') && (
                  <Box mt={2} pl={2} borderLeftWidth={2} borderColor="blue.300">
                    <Text fontWeight="medium" mb={1}>
                      {hedgeSecondary === 'futures_short' ? t('botCreate.hedgeSpotGridFuturesConfig') : hedgeSecondary === 'spot_short' ? (form.direction === 'LONG' ? t('botCreate.hedgeSpotShortConfig') : t('botCreate.hedgeSpotShortConfigLongOnly')) : t('botCreate.hedgeSpotLongConfig')}
                    </Text>
                    <Text pl={2}><strong>{t('botCreate.shortNotionalRatio')}:</strong> {hedgeShortNotionalRatio}</Text>
                    <Text pl={2}><strong>{t('botCreate.hedgeTriggerLayers')}:</strong> {hedgeTriggerLayers}</Text>
                    <Text pl={2}><strong>{t('botCreate.hedgeRebalanceInterval')}:</strong> {hedgeRebalanceInterval} s</Text>
                  </Box>
                )}
                <Text mt={2}><strong>{t('botCreate.direction')}:</strong> {form.direction === 'LONG' ? t('botDetail.strategy.directionLong') : form.direction === 'SHORT' ? t('botDetail.strategy.directionShort') : t('botDetail.strategy.directionBoth')}</Text>
                <Text><strong>{t('botCreate.priceInterval')}:</strong> {form.price_interval}</Text>
                <Text><strong>{t('botCreate.orderQuantity')}:</strong> {form.order_quantity}</Text>
                <Text><strong>{t('botCreate.buyWindowSize')}:</strong> {form.buy_window_size}</Text>
                <Text><strong>{t('botCreate.sellWindowSize')}:</strong> {form.sell_window_size}</Text>
                {form.rocket_tiered_grid_enabled && (
                  <Text><strong>{t('botCreate.rocketTieredGrid')}:</strong> {t('common.enabled')}</Text>
                )}
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

      {/* Template Selection Modal */}
      <Modal
        isOpen={isTemplateModalOpen}
        onClose={onTemplateModalClose}
        size="full"
        scrollBehavior="inside"
      >
        <ModalOverlay />
        <ModalContent maxW="90vw" h="90vh">
          <ModalHeader>{t('template.selectTemplate')}</ModalHeader>
          <ModalCloseButton />
          <ModalBody pb={6}>
            <StrategyTemplates
              onSelectTemplate={handleSelectTemplate}
              selectedSymbol={form.symbol}
              selectedExchange={form.exchange}
            />
          </ModalBody>
        </ModalContent>
      </Modal>
    </Box>
  )
}

export default BotCreateWizard
