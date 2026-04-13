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
  Alert,
  AlertIcon,
  AlertDescription,
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

function getExchangeConfigRow(
  cfg: Config | null,
  exchange: string
): ExchangeConfig | undefined {
  if (!cfg?.exchanges || !exchange) return undefined
  const raw = cfg.exchanges as Record<string, ExchangeConfig>
  const key =
    Object.keys(raw).find((k) => k.toLowerCase() === exchange.toLowerCase()) ?? ''
  return key ? raw[key] : undefined
}

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

  /** 雙永续跨所：兩腿交易所+合約符號 */
  const [perpLegA, setPerpLegA] = useState({ exchange: 'binance', symbol: '' })
  const [perpLegB, setPerpLegB] = useState({ exchange: 'okx', symbol: '' })
  const [symbolsLegA, setSymbolsLegA] = useState<string[]>([])
  const [symbolsLegB, setSymbolsLegB] = useState<string[]>([])

  const [form, setForm] = useState<{
    exchange: string
    symbol: string
    market_type: 'spot' | 'futures' | 'funding_carry' | 'funding_perp_spread'
    name: string
    price_interval: number | string
    profit_spread: number | string
    order_quantity: number | string
    buy_window_size: number | string
    sell_window_size: number | string
    direction: string
    enable_risk_control?: boolean
    stop_loss_ratio?: number | string
    take_profit_trigger_ratio?: number | string
    enable_trend_filter?: boolean
    rocket_tiered_grid_enabled?: boolean
    spot_inventory_policy: 'conservative' | 'adopt_all'
  }>({
    exchange: 'binance',
    symbol: '',
    market_type: 'futures',
    name: '',
    price_interval: 2,
    profit_spread: '',
    order_quantity: 30,
    buy_window_size: 10,
    sell_window_size: 10,
    direction: 'LONG',
    enable_risk_control: false,
    stop_loss_ratio: 0.2,
    take_profit_trigger_ratio: 0.08,
    enable_trend_filter: false,
    rocket_tiered_grid_enabled: false,
    spot_inventory_policy: 'conservative',
  })

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [symbolsError, setSymbolsError] = useState<string | null>(null)
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
        if (strategies[1] === 'spot_short' || strategies[1] === 'spot_long' || strategies[1] === 'futures_short' || strategies[1] === 'futures_long') {
          setHedgeShortNotionalRatio(template.config.short_notional_ratio ?? 0.25)
          setHedgeTriggerLayers(template.config.hedge_trigger_layers ?? 3)
          setHedgeRebalanceInterval(template.config.rebalance_interval ?? 3600)
        }
        if (strategies[1] === 'spot_long' || strategies[1] === 'futures_long') {
          setForm(f => ({ ...f, direction: 'SHORT' }))
        }
      }

      // Apply form defaults from template
      if (template.config) {
        const cfg = template.config as any
        if (cfg.price_interval) setForm(f => ({ ...f, price_interval: cfg.price_interval }))
        if (cfg.profit_spread != null && cfg.profit_spread !== '') setForm(f => ({ ...f, profit_spread: cfg.profit_spread }))
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
    if (strategyType === 'funding') {
      setForm((f) => ({ ...f, market_type: 'funding_carry' }))
      setSelectedSingle('funding_carry')
      setStrategyParams((p) => ({
        ...p,
        funding_carry: p.funding_carry ?? {
          min_funding_rate: 0.0004,
          exit_funding_rate: 0.0002,
          max_basis_pct: 0.5,
        },
      }))
    }
  }, [strategyType])

  useEffect(() => {
    if (strategyType === 'funding_perp') {
      setSelectedSingle('funding_perp_spread')
      setStrategyParams((p) => ({
        ...p,
        funding_perp_spread: p.funding_perp_spread ?? {
          min_funding_spread: 0.0001,
          exit_funding_spread: 0.00005,
          max_basis_pct: 1.0,
        },
      }))
    }
  }, [strategyType])

  useEffect(() => {
    if (strategyType !== 'funding_perp') return
    setForm((f) => ({
      ...f,
      market_type: 'funding_perp_spread',
      exchange: perpLegA.exchange,
      symbol: perpLegA.symbol,
    }))
  }, [strategyType, perpLegA.exchange, perpLegA.symbol])

  useEffect(() => {
    if (!form.exchange || !config?.exchanges) return
    if (strategyType === 'funding_perp') return
    const exCfg = getExchangeConfigRow(config, form.exchange)
    if (!exCfg?.api_key || !exCfg?.secret_key) {
      setSymbols([])
      setSymbolsError(null)
      return
    }
    setSymbolsError(null)
    const load = async () => {
      try {
        const symMt = form.market_type === 'spot' ? 'spot' : 'futures'
        const res = await getExchangeSymbols({
          exchange: form.exchange,
          api_key: String(exCfg.api_key),
          secret_key: String(exCfg.secret_key),
          passphrase: String(exCfg.passphrase || ''),
          market_type: symMt,
        })
        setSymbols(res.symbols || [])
        setSymbolsError(null)
      } catch (e) {
        console.error('[BotCreate] exchange-symbols failed:', e)
        setSymbols([])
        const msg = e instanceof Error ? e.message : String(e)
        setSymbolsError(msg)
      }
    }
    load()
  }, [form.exchange, form.market_type, strategyType, config?.exchanges])

  useEffect(() => {
    if (strategyType !== 'funding_perp' || !config?.exchanges) return
    const loadLeg = async (exchange: string, setSyms: (s: string[]) => void) => {
      const exCfg = getExchangeConfigRow(config, exchange)
      if (!exCfg?.api_key || !exCfg?.secret_key) {
        setSyms([])
        return
      }
      try {
        const res = await getExchangeSymbols({
          exchange,
          api_key: String(exCfg.api_key),
          secret_key: String(exCfg.secret_key),
          passphrase: String(exCfg.passphrase || ''),
          market_type: 'futures',
        })
        setSyms(res.symbols || [])
      } catch (e) {
        console.error('[BotCreate] perp leg symbols failed:', e)
        setSyms([])
      }
    }
    void loadLeg(perpLegA.exchange, setSymbolsLegA)
    void loadLeg(perpLegB.exchange, setSymbolsLegB)
  }, [strategyType, perpLegA.exchange, perpLegB.exchange, config?.exchanges])

  useEffect(() => {
    if (strategyType === 'funding_perp') {
      if (!perpLegA.exchange || !perpLegA.symbol) {
        setMarketTicker(null)
        return
      }
      const load = async () => {
        try {
          const res = await getMarketTicker(perpLegA.exchange, perpLegA.symbol, 'futures')
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
      void load()
      return
    }
    if (!form.exchange || !form.symbol) {
      setMarketTicker(null)
      return
    }
    const load = async () => {
      try {
        const tickerMt = form.market_type === 'spot' ? 'spot' : 'futures'
        const res = await getMarketTicker(form.exchange, form.symbol, tickerMt)
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
    void load()
  }, [strategyType, perpLegA.exchange, perpLegA.symbol, form.exchange, form.symbol, form.market_type])

  const getStrategyIds = (): string[] => {
    if (strategyType === 'funding') return ['funding_carry']
    if (strategyType === 'funding_perp') return ['funding_perp_spread']
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
    if (strategyType === 'funding_perp') {
      if (!legsDistinct) {
        toast({ title: t('botCreate.fillRequired'), status: 'error', duration: 3000 })
        return
      }
    } else if (!form.exchange || !form.symbol) {
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
        total_allocated_capital:
          strategyType === 'funding' || strategyType === 'funding_perp'
            ? toNum(form.order_quantity, 300)
            : undefined,
        price_interval: toNum(form.price_interval, 2),
        profit_spread: toNum(form.profit_spread, 0),
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
        ...(form.market_type === 'spot'
          ? { spot_inventory_policy: form.spot_inventory_policy }
          : {}),
      }

      if (strategyType === 'hedge') {
        const isSpotGridFuturesHedge = hedgePrimary === 'grid' && hedgeSecondary === 'futures_short'
        const isSpotGridShortFuturesLongHedge = hedgePrimary === 'grid' && hedgeSecondary === 'futures_long'
        const groupType = isSpotGridShortFuturesLongHedge
          ? 'spot_grid_short_futures_long_hedge'
          : isSpotGridFuturesHedge
            ? 'spot_grid_futures_hedge'
            : 'futures_spot_hedge'
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
          futures_bot: (isSpotGridFuturesHedge || isSpotGridShortFuturesLongHedge)
            ? { ...baseReq, market_type: 'futures', strategies: spotStrategies }
            : { ...baseReq, market_type: 'futures', strategies: futuresStrategies },
          spot_bot: (isSpotGridFuturesHedge || isSpotGridShortFuturesLongHedge)
            ? { ...baseReq, market_type: 'spot', strategies: futuresStrategies }
            : { ...baseReq, market_type: 'spot', strategies: spotStrategies },
        })
        toast({ title: t('botCreate.success'), status: 'success', duration: 2000 })
        navigate('/bots')
      } else if (strategyType === 'funding_perp') {
        const sp = strategyParams.funding_perp_spread || {}
        const minS = (sp.min_funding_spread as number) ?? 0.0001
        const exitS = (sp.exit_funding_spread as number) ?? 0.00005
        const maxB = (sp.max_basis_pct as number) ?? 1.0
        const res = await createBot({
          exchange: perpLegA.exchange.trim(),
          symbol: perpLegA.symbol.trim(),
          market_type: 'funding_perp_spread',
          name: form.name?.trim() || undefined,
          total_allocated_capital: toNum(form.order_quantity, 400),
          order_quantity: toNum(form.order_quantity, 400),
          price_interval: 0,
          profit_spread: 0,
          buy_window_size: 0,
          sell_window_size: 0,
          direction: 'LONG',
          strategies: [
            {
              type: 'funding_perp_spread',
              weight: 1,
              config: normalizeConfigForApi((strategyParams.funding_perp_spread || {}) as Record<string, unknown>),
            },
          ],
          funding_perp_spread: {
            leg_a: { exchange: perpLegA.exchange.trim(), symbol: perpLegA.symbol.trim() },
            leg_b: { exchange: perpLegB.exchange.trim(), symbol: perpLegB.symbol.trim() },
            min_funding_spread: minS,
            exit_funding_spread: exitS,
            max_basis_pct: maxB,
          },
        })
        toast({ title: t('botCreate.success'), status: 'success', duration: 2000 })
        if (res?.bot_id) navigate(`/bots/${res.bot_id}`)
        else navigate('/bots')
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
    strategyType === 'funding' ||
    strategyType === 'funding_perp' ||
    (strategyType === 'single' && selectedSingle) ||
    (strategyType === 'combo' && selectedCombo.length > 0) ||
    (strategyType === 'hedge' && hedgePrimary && hedgeSecondary)
  const legsDistinct =
    perpLegA.exchange &&
    perpLegA.symbol &&
    perpLegB.exchange &&
    perpLegB.symbol &&
    (perpLegA.exchange.trim().toLowerCase() !== perpLegB.exchange.trim().toLowerCase() ||
      perpLegA.symbol.trim().toLowerCase() !== perpLegB.symbol.trim().toLowerCase())
  const canProceedStep2 =
    strategyType === 'funding_perp'
      ? Boolean(legsDistinct)
      : Boolean(form.exchange && form.symbol)

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

          {step === 1 && strategyType === 'funding' && (
            <Box p={4} borderRadius="md" bg="blue.50" borderWidth={1} borderColor="blue.100">
              <Text fontWeight="medium" mb={2}>{t('botCreate.strategyType.funding.title')}</Text>
              <Text fontSize="sm" color="gray.700">{t('botCreate.strategyType.funding.desc')}</Text>
            </Box>
          )}

          {step === 1 && strategyType === 'funding_perp' && (
            <Box p={4} borderRadius="md" bg="purple.50" borderWidth={1} borderColor="purple.100">
              <Text fontWeight="medium" mb={2}>{t('botCreate.strategyType.funding_perp.title')}</Text>
              <Text fontSize="sm" color="gray.700">{t('botCreate.strategyType.funding_perp.desc')}</Text>
            </Box>
          )}

          {step === 1 && strategyType !== 'funding' && strategyType !== 'funding_perp' && (
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

          {step === 2 && strategyType === 'funding_perp' && (
            <VStack spacing={4} align="stretch">
              <Text fontSize="sm" color="gray.600">{t('botCreate.perpSpreadLegHint')}</Text>
              <Text fontWeight="semibold">{t('botCreate.perpSpreadLegA')}</Text>
              <HStack align="flex-start" spacing={4} flexWrap="wrap">
                <FormControl isRequired flex="1" minW="200px">
                  <FormLabel>{t('configSetup.exchange')}</FormLabel>
                  <Select
                    value={perpLegA.exchange || ''}
                    onChange={(e) =>
                      setPerpLegA((l) => ({ ...l, exchange: e.target.value }))
                    }
                  >
                    {exchanges.map((ex) => (
                      <option key={ex} value={ex}>{ex.toUpperCase()}</option>
                    ))}
                  </Select>
                </FormControl>
                <FormControl isRequired flex="1" minW="200px">
                  <FormLabel>{t('configSetup.symbol')}</FormLabel>
                  <Select
                    value={perpLegA.symbol || ''}
                    onChange={(e) =>
                      setPerpLegA((l) => ({ ...l, symbol: e.target.value }))
                    }
                    placeholder={t('botCreate.selectSymbol')}
                  >
                    {symbolsLegA.map((s) => (
                      <option key={s} value={s}>{s}</option>
                    ))}
                  </Select>
                </FormControl>
              </HStack>
              <Text fontWeight="semibold">{t('botCreate.perpSpreadLegB')}</Text>
              <HStack align="flex-start" spacing={4} flexWrap="wrap">
                <FormControl isRequired flex="1" minW="200px">
                  <FormLabel>{t('configSetup.exchange')}</FormLabel>
                  <Select
                    value={perpLegB.exchange || ''}
                    onChange={(e) =>
                      setPerpLegB((l) => ({ ...l, exchange: e.target.value }))
                    }
                  >
                    {exchanges.map((ex) => (
                      <option key={ex} value={ex}>{ex.toUpperCase()}</option>
                    ))}
                  </Select>
                </FormControl>
                <FormControl isRequired flex="1" minW="200px">
                  <FormLabel>{t('configSetup.symbol')}</FormLabel>
                  <Select
                    value={perpLegB.symbol || ''}
                    onChange={(e) =>
                      setPerpLegB((l) => ({ ...l, symbol: e.target.value }))
                    }
                    placeholder={t('botCreate.selectSymbol')}
                  >
                    {symbolsLegB.map((s) => (
                      <option key={s} value={s}>{s}</option>
                    ))}
                  </Select>
                </FormControl>
              </HStack>
              {!legsDistinct && perpLegA.symbol && perpLegB.symbol && (
                <Alert status="warning" borderRadius="md">
                  <AlertIcon />
                  <AlertDescription>{t('botCreate.perpSpreadLegHint')}</AlertDescription>
                </Alert>
              )}
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
                  <Text fontWeight="medium" mb={2}>{t('botCreate.marketData')} ({perpLegA.exchange} / {perpLegA.symbol})</Text>
                  <HStack spacing={4} flexWrap="wrap">
                    <Text><strong>{t('botCreate.markPrice')}:</strong> {marketTicker.mark_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                    <Text><strong>{t('botCreate.last24hHigh')}:</strong> {marketTicker.high_24h.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                    <Text><strong>{t('botCreate.last24hLow')}:</strong> {marketTicker.low_24h.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                  </HStack>
                </Box>
              )}
            </VStack>
          )}

          {step === 2 && strategyType !== 'funding_perp' && (
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
                    isDisabled={strategyType === 'funding'}
                    onChange={(e) =>
                      setForm((f) => ({
                        ...f,
                        market_type: e.target.value as 'spot' | 'futures' | 'funding_carry',
                      }))
                    }
                  >
                    <option value="futures">{t('symbolManager.futures')}</option>
                    <option value="spot">{t('symbolManager.spot')}</option>
                    <option value="funding_carry">{t('botCreate.marketTypeFundingCarry')}</option>
                  </Select>
                  {strategyType === 'funding' && (
                    <Text fontSize="xs" color="gray.500" mt={1}>
                      {t('botCreate.fundingMarketLockedHint')}
                    </Text>
                  )}
                </FormControl>
              )}
              {strategyType === 'hedge' && (
                <Text fontSize="sm" color="gray.600">{t('botCreate.hedgeMarketHint')}</Text>
              )}
              {config && form.exchange && !getExchangeConfigRow(config, form.exchange)?.api_key && (
                <Alert status="warning" borderRadius="md">
                  <AlertIcon />
                  <AlertDescription>{t('botCreate.symbolListNoCredentials')}</AlertDescription>
                </Alert>
              )}
              {symbolsError && (
                <Alert status="error" borderRadius="md">
                  <AlertIcon />
                  <Box>
                    <Text fontWeight="semibold" mb={1}>{t('botCreate.symbolListLoadFailed')}</Text>
                    <AlertDescription>{symbolsError}</AlertDescription>
                  </Box>
                </Alert>
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

          {step === 3 && strategyType === 'funding_perp' && (
            <VStack spacing={4} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <AlertDescription>{t('botCreate.perpSpreadRiskHint')}</AlertDescription>
              </Alert>
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
              <FormControl isRequired>
                <FormLabel>{t('botCreate.fundingAllocatedCapital')}</FormLabel>
                <DecimalNumberInput
                  value={form.order_quantity ?? 400}
                  min={200}
                  step={10}
                  precision={2}
                  onChange={(v) => setForm((f) => ({ ...f, order_quantity: v ?? 400 }))}
                />
                <Text fontSize="xs" color="gray.500" mt={1}>
                  {t('botCreate.fundingAllocatedCapitalHint')}
                </Text>
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.perpSpreadMinSpread')}</FormLabel>
                <DecimalNumberInput
                  value={(strategyParams.funding_perp_spread?.min_funding_spread as number) ?? 0.0001}
                  min={0.00001}
                  max={0.01}
                  step={0.0001}
                  precision={6}
                  onChange={(v) =>
                    setStrategyParams((p) => ({
                      ...p,
                      funding_perp_spread: { ...p.funding_perp_spread, min_funding_spread: v ?? 0.0001 },
                    }))
                  }
                />
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.perpSpreadExitSpread')}</FormLabel>
                <DecimalNumberInput
                  value={(strategyParams.funding_perp_spread?.exit_funding_spread as number) ?? 0.00005}
                  min={0.00001}
                  max={0.01}
                  step={0.0001}
                  precision={6}
                  onChange={(v) =>
                    setStrategyParams((p) => ({
                      ...p,
                      funding_perp_spread: { ...p.funding_perp_spread, exit_funding_spread: v ?? 0.00005 },
                    }))
                  }
                />
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.perpSpreadMaxBasis')}</FormLabel>
                <DecimalNumberInput
                  value={(strategyParams.funding_perp_spread?.max_basis_pct as number) ?? 1.0}
                  min={0.05}
                  max={10}
                  step={0.05}
                  precision={2}
                  onChange={(v) =>
                    setStrategyParams((p) => ({
                      ...p,
                      funding_perp_spread: { ...p.funding_perp_spread, max_basis_pct: v ?? 1.0 },
                    }))
                  }
                />
              </FormControl>
            </VStack>
          )}

          {step === 3 && strategyType === 'funding' && (
            <VStack spacing={4} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <AlertDescription>{t('botCreate.fundingRiskHint')}</AlertDescription>
              </Alert>
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
              <FormControl isRequired>
                <FormLabel>{t('botCreate.fundingAllocatedCapital')}</FormLabel>
                <DecimalNumberInput
                  value={form.order_quantity ?? 300}
                  min={200}
                  step={10}
                  precision={2}
                  onChange={(v) => setForm((f) => ({ ...f, order_quantity: v ?? 300 }))}
                />
                <Text fontSize="xs" color="gray.500" mt={1}>
                  {t('botCreate.fundingAllocatedCapitalHint')}
                </Text>
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.fundingMinRate')}</FormLabel>
                <DecimalNumberInput
                  value={(strategyParams.funding_carry?.min_funding_rate as number) ?? 0.0004}
                  min={0.00001}
                  max={0.01}
                  step={0.0001}
                  precision={6}
                  onChange={(v) =>
                    setStrategyParams((p) => ({
                      ...p,
                      funding_carry: { ...p.funding_carry, min_funding_rate: v ?? 0.0004 },
                    }))
                  }
                />
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.fundingExitRate')}</FormLabel>
                <DecimalNumberInput
                  value={(strategyParams.funding_carry?.exit_funding_rate as number) ?? 0.0002}
                  min={0.00001}
                  max={0.01}
                  step={0.0001}
                  precision={6}
                  onChange={(v) =>
                    setStrategyParams((p) => ({
                      ...p,
                      funding_carry: { ...p.funding_carry, exit_funding_rate: v ?? 0.0002 },
                    }))
                  }
                />
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.fundingMaxBasis')}</FormLabel>
                <DecimalNumberInput
                  value={(strategyParams.funding_carry?.max_basis_pct as number) ?? 0.5}
                  min={0.05}
                  max={5}
                  step={0.05}
                  precision={2}
                  onChange={(v) =>
                    setStrategyParams((p) => ({
                      ...p,
                      funding_carry: { ...p.funding_carry, max_basis_pct: v ?? 0.5 },
                    }))
                  }
                />
              </FormControl>
            </VStack>
          )}

          {step === 3 && strategyType !== 'funding' && strategyType !== 'funding_perp' && (
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
              {form.market_type === 'spot' && (
                <FormControl>
                  <FormLabel>{t('botCreate.spotInventoryPolicy')}</FormLabel>
                  <Select
                    value={form.spot_inventory_policy}
                    onChange={(e) =>
                      setForm((f) => ({
                        ...f,
                        spot_inventory_policy: e.target.value as 'conservative' | 'adopt_all',
                      }))
                    }
                  >
                    <option value="conservative">{t('botCreate.spotInventoryConservative')}</option>
                    <option value="adopt_all">{t('botCreate.spotInventoryAdoptAll')}</option>
                  </Select>
                  <Text fontSize="xs" color="gray.500" mt={1}>
                    {t('botCreate.spotInventoryPolicyHint')}
                  </Text>
                </FormControl>
              )}
              <StrategyParamForm
                strategyIds={getStrategyIds()}
                value={strategyParams}
                onChange={setStrategyParams}
              />
              {strategyType === 'hedge' && (hedgeSecondary === 'spot_short' || hedgeSecondary === 'spot_long' || hedgeSecondary === 'futures_short' || hedgeSecondary === 'futures_long') && (
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
                    {hedgeSecondary === 'futures_long'
                      ? t('botCreate.hedgeSpotGridShortFuturesLongConfig')
                      : hedgeSecondary === 'futures_short'
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
                <FormLabel>{t('botCreate.profitSpread')}</FormLabel>
                <DecimalNumberInput
                  value={form.profit_spread ?? ''}
                  min={0}
                  step={0.1}
                  precision={4}
                  onChange={(v) => setForm((f) => ({ ...f, profit_spread: v ?? '' }))}
                />
                <Text fontSize="xs" color="gray.500" mt={1}>
                  {t('botCreate.profitSpreadHint')}
                </Text>
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

              {/* 网格风控设置 - 简化版，启用风控 + 止损/止盈放一起，趋势过滤独立 */}
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
            </VStack>
          )}

          {step === 4 && (
            <VStack spacing={4} align="stretch">
              <Text>{t('botCreate.reviewDesc')}</Text>
              <Box bg="gray.50" p={4} borderRadius="md" fontSize="sm">
                {form.name && <Text><strong>{t('botCreate.botName')}:</strong> {form.name}</Text>}
                {strategyType === 'funding_perp' ? (
                  <>
                    <Text><strong>{t('botCreate.perpSpreadLegA')}:</strong> {perpLegA.exchange} / {perpLegA.symbol}</Text>
                    <Text><strong>{t('botCreate.perpSpreadLegB')}:</strong> {perpLegB.exchange} / {perpLegB.symbol}</Text>
                  </>
                ) : (
                  <>
                    <Text><strong>{t('configSetup.exchange')}:</strong> {form.exchange}</Text>
                    <Text><strong>{t('configSetup.symbol')}:</strong> {form.symbol}</Text>
                  </>
                )}
                <Text><strong>{t('botCreate.strategyTypeLabel')}:</strong> {t(`botCreate.strategyType.${strategyType}.title`)}</Text>
                {strategyType === 'funding' && (
                  <Text><strong>{t('botCreate.strategyLabel')}:</strong> {t('strategyNames.funding_carry', 'funding_carry')}</Text>
                )}
                {strategyType === 'funding_perp' && (
                  <Text><strong>{t('botCreate.strategyLabel')}:</strong> {t('strategyNames.funding_perp_spread')}</Text>
                )}
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
                {strategyType === 'hedge' && (hedgeSecondary === 'spot_short' || hedgeSecondary === 'spot_long' || hedgeSecondary === 'futures_short' || hedgeSecondary === 'futures_long') && (
                  <Box mt={2} pl={2} borderLeftWidth={2} borderColor="blue.300">
                    <Text fontWeight="medium" mb={1}>
                      {hedgeSecondary === 'futures_long'
                        ? t('botCreate.hedgeSpotGridShortFuturesLongConfig')
                        : hedgeSecondary === 'futures_short'
                          ? t('botCreate.hedgeSpotGridFuturesConfig')
                          : hedgeSecondary === 'spot_short'
                            ? (form.direction === 'LONG' ? t('botCreate.hedgeSpotShortConfig') : t('botCreate.hedgeSpotShortConfigLongOnly'))
                            : t('botCreate.hedgeSpotLongConfig')}
                    </Text>
                    <Text pl={2}><strong>{t('botCreate.shortNotionalRatio')}:</strong> {hedgeShortNotionalRatio}</Text>
                    <Text pl={2}><strong>{t('botCreate.hedgeTriggerLayers')}:</strong> {hedgeTriggerLayers}</Text>
                    <Text pl={2}><strong>{t('botCreate.hedgeRebalanceInterval')}:</strong> {hedgeRebalanceInterval} s</Text>
                  </Box>
                )}
                {strategyType !== 'funding' && strategyType !== 'funding_perp' && (
                  <>
                    <Text mt={2}><strong>{t('botCreate.direction')}:</strong> {form.direction === 'LONG' ? t('botDetail.strategy.directionLong') : form.direction === 'SHORT' ? t('botDetail.strategy.directionShort') : t('botDetail.strategy.directionBoth')}</Text>
                    <Text><strong>{t('botCreate.priceInterval')}:</strong> {form.price_interval}</Text>
                    {(form.profit_spread != null && form.profit_spread !== '') && (
                      <Text><strong>{t('botCreate.profitSpread')}:</strong> {form.profit_spread}</Text>
                    )}
                    <Text><strong>{t('botCreate.buyWindowSize')}:</strong> {form.buy_window_size}</Text>
                    <Text><strong>{t('botCreate.sellWindowSize')}:</strong> {form.sell_window_size}</Text>
                    {form.rocket_tiered_grid_enabled && (
                      <Text><strong>{t('botCreate.rocketTieredGrid')}:</strong> {t('common.enabled')}</Text>
                    )}
                  </>
                )}
                {(strategyType === 'funding' || strategyType === 'funding_perp') && (
                  <Text><strong>{t('botCreate.fundingAllocatedCapital')}:</strong> {form.order_quantity}</Text>
                )}
                {strategyType !== 'funding' && strategyType !== 'funding_perp' && (
                  <Text><strong>{t('botCreate.orderQuantity')}:</strong> {form.order_quantity}</Text>
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
