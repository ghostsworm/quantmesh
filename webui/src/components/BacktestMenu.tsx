import React, { useEffect, useState, useCallback, useRef } from 'react'
import { useSearchParams } from 'react-router-dom'
import html2canvas from 'html2canvas'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useTranslation } from 'react-i18next'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip as RechartsTooltip,
  ResponsiveContainer,
} from 'recharts'
import {
  Box,
  Button,
  Card,
  CardBody,
  CardHeader,
  FormControl,
  FormLabel,
  Heading,
  HStack,
  Input,
  NumberInput,
  NumberInputField,
  Select,
  SimpleGrid,
  Slider,
  SliderTrack,
  SliderFilledTrack,
  SliderThumb,
  Spinner,
  Tab,
  TabList,
  TabPanel,
  TabPanels,
  Tabs,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Badge,
  Text,
  useToast,
  VStack,
  Flex,
  IconButton,
  Tooltip,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Progress,
  Divider,
  Collapse,
  useDisclosure,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  RadioGroup,
  Radio,
  Stack,
} from '@chakra-ui/react'
import { RepeatIcon, DownloadIcon, DeleteIcon, CheckIcon, StarIcon, ChevronDownIcon, ChevronUpIcon } from '@chakra-ui/icons'
import {
  getBacktestStrategies,
  getBacktestPreset,
  getBacktestExchanges,
  getBacktestSymbols,
  getBacktestConfigParams,
  postCacheGenerate,
  getCacheStatus,
  listCache,
  deleteCache,
  postBacktestTask,
  getBacktestTasks,
  getBacktestTask,
  getBacktestTaskResult,
  getBacktestTaskReport,
  getBacktestTaskKlines,
  deleteBacktestTask,
  getSmartParamsRecommendation,
  getPrecomputedResults,
  triggerPrecompute,
  getAutoSchedulerStatus,
  postOptimTask,
  getOptimTasks,
  getOptimTaskResult,
  deleteOptimTask,
  getOptimSearchSpace,
  type StrategyParamDefinition,
  type SymbolBacktestPreset,
  type BacktestTask,
  type BacktestExchangeInfo,
  type BacktestSymbolInfo,
  type SmartParamsRecommendation,
  type PrecomputedResult,
  type CacheInfo,
  type OptimTask,
  type OptimSearchSpace,
  type OptimResult,
} from '../services/backtest'
import { listKlineFiles, listAvailableKlineFiles, type KlineFileInfo, type AvailableKlineFile } from '../services/klineFiles'
import OptimResultModal from './OptimResultModal'

/** 回测结果数据结构（含风控对比） */
interface BacktestResultData {
  result?: {
    metrics?: Record<string, unknown>
    trades?: Array<{ type?: string; quantity?: number }>
    price_curve?: { end_price?: number }
    final_capital?: number
  }
  comparison?: {
    no_risk_result?: {
      metrics?: Record<string, unknown>
      trades?: Array<{ type?: string; quantity?: number }>
      price_curve?: { end_price?: number }
      final_capital?: number
    }
    with_risk_result?: {
      metrics?: Record<string, unknown>
      trades?: Array<{ type?: string; quantity?: number }>
      price_curve?: { end_price?: number }
      final_capital?: number
      risk_interventions?: Array<{ time_str?: string; reason?: string; risk_type?: string; duration?: number; skipped_buys?: number }>
    }
    comparison?: { risk_intervention_count?: number; skipped_signals?: number }
  }
  multi_result?: {
    total_return_pct?: number
    total_trades?: number
    final_equity?: number
    initial_capital?: number
    total_fees?: number
    total_funding?: number
    stats_by_strategy?: Record<string, unknown>
    risk_metrics?: {
      max_drawdown_pct?: number
      sharpe_ratio?: number
      win_rate?: number
    }
  }
  hedge_result?: {
    total_return_pct?: number
    max_drawdown_pct?: number
    final_equity?: number
    initial_capital?: number
    rebalance_count?: number
    aligned_points?: number
    long_symbol?: string
    short_symbol?: string
  }
}

const formatDate = (s: string) => {
  try {
    const d = new Date(s)
    return isNaN(d.getTime()) ? s : d.toLocaleString()
  } catch {
    return s
  }
}

// 网格策略回测时价格区间从 K 线自动推导，不再需要预设价格上下限

export default function BacktestMenu() {
  const [searchParams] = useSearchParams()
  const [strategies, setStrategies] = useState<StrategyParamDefinition[]>([])
  
  // 三级联动：交易所 → 市场类型 → 交易对
  const [exchanges, setExchanges] = useState<BacktestExchangeInfo[]>([])
  const [selectedExchange, setSelectedExchange] = useState('')
  const [selectedMarketType, setSelectedMarketType] = useState('futures')
  const [availableMarketTypes, setAvailableMarketTypes] = useState<string[]>(['futures', 'spot'])
  const [symbols, setSymbols] = useState<BacktestSymbolInfo[]>([])
  const [symbol, setSymbol] = useState('')
  
  const [preset, setPreset] = useState<SymbolBacktestPreset | null>(null)
  const [interval, setInterval] = useState('')
  const [days, setDays] = useState(7)
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [cacheExists, setCacheExists] = useState(false)
  const [cacheGenerating, setCacheGenerating] = useState(false)
  const [strategyType, setStrategyType] = useState('')
  const [taskMode, setTaskMode] = useState<'single_strategy' | 'bot_strategies' | 'hedge_group'>('single_strategy')
  const [taskBotId, setTaskBotId] = useState('')
  const [taskGroupId, setTaskGroupId] = useState('')
  const [taskStrategies, setTaskStrategies] = useState<Array<{ type: string; weight: number; config?: Record<string, unknown> }>>([])
  const [params, setParams] = useState<Record<string, unknown>>({})
  const [totalCapital, setTotalCapital] = useState(10000)
  const urlParamsApplied = useRef(false)
  
  // 数据来源相关状态
  const [dataSource, setDataSource] = useState<'time_range' | 'kline_file' | 'cache'>('time_range')
  const [selectedKlineFile, setSelectedKlineFile] = useState<string>('')
  const [selectedCacheName, setSelectedCacheName] = useState<string>('')
  const [klineFiles, setKlineFiles] = useState<KlineFileInfo[]>([])
  const [availableKlineFiles, setAvailableKlineFiles] = useState<AvailableKlineFile[]>([])
  const [klineFilesLoading, setKlineFilesLoading] = useState(false)
  const [tasks, setTasks] = useState<BacktestTask[]>([])
  const [tasksLoading, setTasksLoading] = useState(true)
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null)
  const [resultData, setResultData] = useState<unknown>(null)
  const [reportMd, setReportMd] = useState('')
  const [klinesData, setKlinesData] = useState<{ klines: { ts: number; time: string; close: number }[]; symbol: string } | null>(null)
  const [running, setRunning] = useState(false)
  const reportModal = useDisclosure()
  const [configParamsLoading, setConfigParamsLoading] = useState(false)
  const [savingImage, setSavingImage] = useState(false)
  const reportContentRef = useRef<HTMLDivElement>(null)
  const toast = useToast()
  const { t } = useTranslation()

  // 智能推荐相关状态
  const [smartRecommendation, setSmartRecommendation] = useState<SmartParamsRecommendation | null>(null)
  const [smartLoading, setSmartLoading] = useState(false)
  const [precomputedResults, setPrecomputedResults] = useState<PrecomputedResult[]>([])
  const [precomputedLoading, setPrecomputedLoading] = useState(true)
  const { isOpen: showPrecomputed, onToggle: togglePrecomputed } = useDisclosure({ defaultIsOpen: true })

  // K 线缓存列表
  const [cachedKlines, setCachedKlines] = useState<CacheInfo[]>([])
  const [cachedKlinesLoading, setCachedKlinesLoading] = useState(false)

  // 参数优化
  const [optimTasks, setOptimTasks] = useState<OptimTask[]>([])
  const [optimTasksLoading, setOptimTasksLoading] = useState(true)
  const [selectedOptimTaskId, setSelectedOptimTaskId] = useState<string | null>(null)
  const [optimResultData, setOptimResultData] = useState<OptimResult | null>(null)
  const [optimRunning, setOptimRunning] = useState(false)
  const optimResultModal = useDisclosure()

  // 载入预计算结果（需在下方 useEffect 之前定义）
  const loadPrecomputedResults = useCallback(async () => {
    setPrecomputedLoading(true)
    try {
      const r = await getPrecomputedResults({ only_ready: true })
      if (r.success && r.results) {
        setPrecomputedResults(r.results)
      }
    } catch (err) {
      console.error('载入预计算结果失败:', err)
    } finally {
      setPrecomputedLoading(false)
    }
  }, [])

  // 载入已缓存的 K 线列表（需在下方 useEffect 之前定义）
  const loadOptimTasks = useCallback(async () => {
    setOptimTasksLoading(true)
    try {
      const r = await getOptimTasks(50, 0)
      if (r.success && r.tasks) setOptimTasks(r.tasks)
    } catch (err) {
      console.error('载入优化任务失败:', err)
    } finally {
      setOptimTasksLoading(false)
    }
  }, [])

  const handleStartOptim = useCallback(async () => {
    if (!symbol || !interval || !startDate || !endDate || !strategyType) {
      toast({ title: t('backtest.selectSymbolIntervalDateStrategy'), status: 'warning' })
      return
    }
    if (totalCapital <= 0) {
      toast({ title: t('backtest.capitalMustBePositive'), status: 'warning' })
      return
    }
    const start = new Date(startDate)
    const end = new Date(endDate)
    end.setHours(23, 59, 59, 999)
    setOptimRunning(true)
    try {
      const r = await postOptimTask({
        strategy: strategyType,
        symbol,
        interval,
        start_time: start.toISOString(),
        end_time: end.toISOString(),
        total_capital: totalCapital,
        search_space: undefined, // 使用後端默認
      })
      if (r.success) {
        toast({ title: t('backtest.optimTaskCreated'), status: 'success' })
        loadOptimTasks()
        setSelectedOptimTaskId(r.task_id)
      }
    } catch (e) {
      toast({ title: (e as Error)?.message || t('backtest.createFailed'), status: 'error' })
    } finally {
      setOptimRunning(false)
    }
  }, [symbol, interval, startDate, endDate, strategyType, totalCapital, toast, loadOptimTasks])

  const handleViewOptimResult = useCallback(
    async (taskId: string) => {
      setSelectedOptimTaskId(taskId)
      optimResultModal.onOpen()
      setOptimResultData(null)
      try {
        const data = await getOptimTaskResult(taskId)
        setOptimResultData(data as OptimResult)
      } catch (err) {
        console.error('载入优化结果失败:', err)
        toast({ title: t('backtest.loadResultFailed'), status: 'error' })
      }
    },
    [optimResultModal, toast]
  )

  const loadCachedKlines = useCallback(async () => {
    setCachedKlinesLoading(true)
    try {
      const r = await listCache()
      if (r.success && r.caches) {
        setCachedKlines(r.caches)
      }
    } catch (err) {
      console.error('载入 K 线缓存列表失败:', err)
      toast({ title: t('backtest.loadCacheListFailed'), status: 'error' })
    } finally {
      setCachedKlinesLoading(false)
    }
  }, [toast])

  const loadKlineFiles = useCallback(async (showError = false) => {
    setKlineFilesLoading(true)
    try {
      const files = await listKlineFiles()
      setKlineFiles(files)
      // 同时加载可用文件列表（用于回测）
      const availableFiles = await listAvailableKlineFiles()
      setAvailableKlineFiles(availableFiles)
    } catch (err) {
      // 服务不可用时（如 K 线收集器未初始化）静默处理
      // 只有在用户手动刷新时才显示错误
      console.warn('载入 K 线文件列表失败:', err)
      if (showError) {
        toast({ title: t('backtest.loadKlineFilesFailed'), description: t('backtest.klineCollectorNotInitialized'), status: 'warning' })
      }
    } finally {
      setKlineFilesLoading(false)
    }
  }, [toast])

  // 从 URL 解析预填参数（用于从 Bot 详情页跳转）
  const urlExchange = searchParams.get('exchange')
  const urlMarketType = searchParams.get('market_type')
  const urlSymbol = searchParams.get('symbol')
  const urlMode = searchParams.get('mode')
  const urlBotId = searchParams.get('bot_id')
  const urlGroupId = searchParams.get('group_id')
  const urlStrategies = searchParams.get('strategies')
  const urlStrategy = searchParams.get('strategy')
  const urlDays = searchParams.get('days')
  const urlTotalCapital = searchParams.get('total_capital')
  const urlGridSpacing = searchParams.get('grid_spacing')
  const urlOrderQuantity = searchParams.get('order_quantity')
  const urlProfitSpread = searchParams.get('profit_spread')

  // 初始化：载入策略列表、交易所列表、预计算结果和 K 线缓存列表
  useEffect(() => {
    getBacktestStrategies().then((r) => r.success && setStrategies(r.strategies || []))
    getBacktestExchanges().then((r) => {
      if (r.success && r.exchanges) {
        const sorted = [...r.exchanges].sort((a, b) =>
          a.exchange.localeCompare(b.exchange, undefined, { sensitivity: 'base' })
        )
        setExchanges(sorted)
        // 优先使用 URL 参数，否则使用已配置或第一个
        const urlEx = urlExchange && sorted.some(e => e.exchange === urlExchange) ? urlExchange : null
        const configured = sorted.find(e => e.is_configured)
        const fallback = configured || sorted[0]
        if (urlEx) {
          setSelectedExchange(urlEx)
          const ex = sorted.find(e => e.exchange === urlEx)
          setAvailableMarketTypes(ex?.market_types || ['futures', 'spot'])
        } else if (fallback) {
          setSelectedExchange(fallback.exchange)
          setAvailableMarketTypes(fallback.market_types || ['futures', 'spot'])
        }
      }
    })
    loadPrecomputedResults()
    loadCachedKlines()
    loadKlineFiles()
  }, [loadPrecomputedResults, loadCachedKlines, loadKlineFiles])

  // 应用 URL 参数到表单（非 exchange/symbol 的需在初始化后执行一次）
  useEffect(() => {
    if (urlParamsApplied.current) return
    if (urlMode === 'bot_strategies' || urlMode === 'hedge_group' || urlMode === 'single_strategy') {
      setTaskMode(urlMode)
    }
    if (urlBotId) setTaskBotId(urlBotId)
    if (urlGroupId) setTaskGroupId(urlGroupId)
    if (urlStrategies) {
      try {
        const parsed = JSON.parse(urlStrategies) as Array<{ type?: string; weight?: number; config?: Record<string, unknown> }>
        if (Array.isArray(parsed) && parsed.length > 0) {
          const normalized = parsed
            .filter((strategy) => typeof strategy?.type === 'string' && strategy.type.length > 0)
            .map((strategy) => ({
              type: strategy.type as string,
              weight: typeof strategy.weight === 'number' ? strategy.weight : 0,
              config: strategy.config || {},
            }))
          if (normalized.length > 0) {
            setTaskStrategies(normalized)
            setTaskMode('bot_strategies')
            setStrategyType(normalized[0].type)
          }
        }
      } catch (err) {
        console.error('failed to parse url strategies', err)
      }
    }
    if (urlStrategy) setStrategyType(urlStrategy)
    const d = parseInt(urlDays || '', 10)
    if (!isNaN(d) && d > 0) setDays(d)
    const tc = parseFloat(urlTotalCapital || '')
    if (!isNaN(tc) && tc > 0) setTotalCapital(tc)
    const p: Record<string, unknown> = {}
    const gs = parseFloat(urlGridSpacing || '')
    if (!isNaN(gs) && gs > 0) p.grid_spacing = gs
    const oq = parseFloat(urlOrderQuantity || '')
    if (!isNaN(oq) && oq > 0) p.order_quantity = oq
    const ps = parseFloat(urlProfitSpread || '')
    if (!isNaN(ps) && ps >= 0) p.profit_spread = ps
    const hr = parseFloat(searchParams.get('hedge_ratio') || '')
    if (!isNaN(hr) && hr > 0) p.hedge_ratio = hr
    const rt = parseFloat(searchParams.get('rebalance_threshold') || '')
    if (!isNaN(rt) && rt > 0) p.rebalance_threshold = rt
    const ri = parseInt(searchParams.get('rebalance_interval') || '', 10)
    if (!isNaN(ri) && ri > 0) p.rebalance_interval = ri
    const legBSymbol = searchParams.get('leg_b_symbol')
    if (legBSymbol) p.leg_b_symbol = legBSymbol
    const legBKlineFile = searchParams.get('leg_b_kline_file')
    if (legBKlineFile) p.leg_b_kline_file = legBKlineFile
    if (Object.keys(p).length > 0) setParams(prev => ({ ...prev, ...p }))
    urlParamsApplied.current = true
  }, [searchParams, urlMode, urlBotId, urlGroupId, urlStrategies, urlStrategy, urlDays, urlTotalCapital, urlGridSpacing, urlOrderQuantity, urlProfitSpread])

  // 获取智能参数推荐
  const handleGetSmartRecommendation = useCallback(async () => {
    if (!symbol || !strategyType) {
      toast({ title: t('backtest.selectSymbolAndStrategy'), status: 'warning' })
      return
    }

    setSmartLoading(true)
    try {
      const r = await getSmartParamsRecommendation({
        exchange: selectedExchange,
        market_type: selectedMarketType,
        symbol,
        strategy: strategyType,
        total_capital: totalCapital,
      })
      if (r.success && r.recommendation) {
        setSmartRecommendation(r.recommendation)
        toast({
          title: t('backtest.smartRecommendationReceived'),
          description: `${t('backtest.confidence')}: ${r.recommendation.confidence.toFixed(0)}%`,
          status: 'success',
          duration: 3000,
        })
      }
    } catch (err) {
      console.error('获取智能推荐失败:', err)
      let description: string | undefined
      const msg = err instanceof Error ? err.message : String(err)
      const jsonMatch = msg.match(/^HTTP \d+: (.+)$/)
      if (jsonMatch) {
        try {
          const body = JSON.parse(jsonMatch[1]) as { message?: string }
          if (body?.message) description = body.message
        } catch {
          description = msg
        }
      } else {
        description = msg
      }
      toast({ title: t('backtest.smartRecommendationFailed'), status: 'error', description })
    } finally {
      setSmartLoading(false)
    }
  }, [symbol, strategyType, selectedExchange, selectedMarketType, totalCapital, toast])

  // 应用智能推荐参数
  const applySmartRecommendation = useCallback((recommendation: SmartParamsRecommendation) => {
    if (!recommendation.params) return

    const newParams: Record<string, unknown> = {}
    const strategyDef = strategies.find(s => s.strategy_type === recommendation.strategy)
    
    if (strategyDef?.params) {
      for (const p of strategyDef.params) {
        if (recommendation.params[p.name] !== undefined) {
          newParams[p.name] = recommendation.params[p.name]
        }
      }
    }

    setParams(newParams)
    
    // 如果推荐中有 total_capital，也设置
    if (recommendation.params.total_capital && typeof recommendation.params.total_capital === 'number') {
      setTotalCapital(recommendation.params.total_capital)
    }

    toast({
      title: t('backtest.smartParamsApplied'),
      status: 'success',
      duration: 2000,
    })
  }, [strategies, toast])

  // 从预计算结果应用配置
  const applyPrecomputedResult = useCallback((result: PrecomputedResult) => {
    // 设置交易对和策略
    setSelectedExchange(result.exchange)
    setSelectedMarketType(result.market_type)
    setSymbol(result.symbol)
    setStrategyType(result.strategy)

    // 应用参数
    if (result.recommendation?.params) {
      setTimeout(() => {
        applySmartRecommendation(result.recommendation)
      }, 100)
    }

    toast({
      title: t('backtest.precomputedConfigApplied'),
      description: `${result.symbol} - ${result.strategy}`,
      status: 'success',
      duration: 3000,
    })
  }, [toast, applySmartRecommendation])

  // 当交易所改变时，更新可用的市场类型
  useEffect(() => {
    if (!selectedExchange) return
    const ex = exchanges.find(e => e.exchange === selectedExchange)
    if (ex) {
      setAvailableMarketTypes(ex.market_types || ['futures', 'spot'])
      // 若有 URL 的 market_type 且合法，优先使用；否则若当前不在列表中则重置
      const mt = ex.market_types || ['futures', 'spot']
      if (urlMarketType && mt.includes(urlMarketType)) {
        setSelectedMarketType(urlMarketType)
      } else if (!mt.includes(selectedMarketType)) {
        setSelectedMarketType(mt[0] || 'futures')
      }
    }
    // 若无 URL symbol，清空交易对选择
    if (!urlSymbol) setSymbol('')
    setSymbols([])
  }, [selectedExchange])

  // 当交易所或市场类型改变时，载入交易对列表
  useEffect(() => {
    if (!selectedExchange || !selectedMarketType) return
    getBacktestSymbols(selectedExchange, selectedMarketType).then((r) => {
      if (r.success && r.symbols) {
        setSymbols(r.symbols)
        // 优先使用 URL 的 symbol（若在列表中），否则选已配置或清空
        const urlSym = urlSymbol && r.symbols.some(s => s.symbol === urlSymbol) ? urlSymbol : null
        const configured = r.symbols.find(s => s.is_configured)
        if (urlSym) {
          setSymbol(urlSym)
        } else if (configured) {
          setSymbol(configured.symbol)
        } else {
          setSymbol('')
        }
      }
    })
  }, [selectedExchange, selectedMarketType])

  // 当交易对改变时，载入预设配置
  useEffect(() => {
    if (!symbol) {
      setPreset(null)
      return
    }
    getBacktestPreset(symbol).then((r) => {
      if (r.success && r.preset) {
        setPreset(r.preset)
        if (!interval) setInterval(r.preset.recommended_interval)
        if (r.preset.recommended_days?.length) setDays(r.preset.recommended_days[0])
      } else {
        setPreset(null)
      }
    })
  }, [symbol])

  // 当策略类型改变时，尝试从配置中预填参数
  const loadConfigParams = useCallback(async () => {
    if (!symbol || !strategyType) return
    
    setConfigParamsLoading(true)
    try {
      const r = await getBacktestConfigParams({
        exchange: selectedExchange,
        symbol,
        strategy: strategyType,
      })
      if (r.success && r.found && r.params) {
        const newParams: Record<string, unknown> = {}
        const configParams = r.params as Record<string, unknown>
        
        // 根据策略定义过滤并设置参数（不覆盖总投入资金，保留用户已填写的值）
        const strategyDef = strategies.find(s => s.strategy_type === strategyType)
        if (strategyDef?.params) {
          for (const p of strategyDef.params) {
            if (configParams[p.name] !== undefined) {
              newParams[p.name] = configParams[p.name]
            }
          }
        }
        
        if (Object.keys(newParams).length > 0) {
          setParams(newParams)
          toast({
            title: t('backtest.configParamsLoaded'),
            description: t('backtest.configParamsLoadedDesc', { symbol, strategy: strategyType }),
            status: 'info',
            duration: 3000,
          })
        }
      }
    } catch (err) {
      console.error('载入配置参数失败:', err)
    } finally {
      setConfigParamsLoading(false)
    }
  }, [symbol, strategyType, selectedExchange, strategies, toast])

  useEffect(() => {
    if (strategyType && symbol) {
      loadConfigParams()
    }
  }, [strategyType, symbol, loadConfigParams])

  // 网格策略回测时价格区间从 K 线自动推导，不再预填 price_low / price_high

  const updateDateRange = () => {
    const end = new Date()
    const start = new Date()
    start.setDate(start.getDate() - days)
    setStartDate(start.toISOString().slice(0, 10))
    setEndDate(end.toISOString().slice(0, 10))
  }
  useEffect(updateDateRange, [days])

  useEffect(() => {
    if (!symbol || !interval || !startDate || !endDate) {
      setCacheExists(false)
      return
    }
    getCacheStatus({ symbol, interval, start_date: startDate, end_date: endDate }).then((r) => {
      if (r.success) setCacheExists(r.exists)
    })
  }, [symbol, interval, startDate, endDate])

  const loadTasks = () => {
    setTasksLoading(true)
    getBacktestTasks(50, 0)
      .then((r) => (r.success && r.tasks ? setTasks(r.tasks) : setTasks([])))
      .finally(() => setTasksLoading(false))
  }
  useEffect(loadTasks, [])
  useEffect(() => {
    loadOptimTasks()
  }, [loadOptimTasks])
  const pollTasks = () => {
    const hasRunning = tasks.some((t) => t.status === 'running' || t.status === 'pending')
    if (hasRunning) setTimeout(loadTasks, 3000)
  }
  useEffect(pollTasks, [tasks])
  const pollOptimTasks = () => {
    const hasOptimRunning = optimTasks.some((t) => t.status === 'running' || t.status === 'pending')
    if (hasOptimRunning) setTimeout(loadOptimTasks, 3000)
  }
  useEffect(pollOptimTasks, [optimTasks])

  const handleGenerateCache = () => {
    if (!symbol || !interval || !startDate || !endDate) {
      toast({ title: t('backtest.selectSymbolIntervalDate'), status: 'warning' })
      return
    }
    setCacheGenerating(true)
    postCacheGenerate({ symbol, interval, start_date: startDate, end_date: endDate })
      .then((r) => {
        if (r.success) {
          toast({ title: t('backtest.cacheGeneratedInBackground'), status: 'success' })
          setTimeout(() => getCacheStatus({ symbol, interval, start_date: startDate, end_date: endDate }).then((s) => s.success && setCacheExists(s.exists)), 2000)
        }
      })
      .catch((e) => toast({ title: e.message || t('backtest.generateFailed'), status: 'error' }))
      .finally(() => setCacheGenerating(false))
  }

  const handleRunBacktest = () => {
    if (!strategyType) {
      toast({ title: t('backtest.selectStrategyRequired'), status: 'warning' })
      return
    }
    if (totalCapital <= 0) {
      toast({ title: t('backtest.capitalMustBePositive'), status: 'warning' })
      return
    }

    // 按数据来源校验
    if (dataSource === 'kline_file') {
      if (!selectedKlineFile) {
        toast({ title: t('backtest.selectKlineFileRequired'), status: 'warning' })
        return
      }
    } else if (dataSource === 'cache') {
      if (!selectedCacheName) {
        toast({ title: t('backtest.selectCacheRequired'), status: 'warning' })
        return
      }
    } else {
      // 时间范围
      if (!symbol || !interval || !startDate || !endDate) {
        toast({ title: t('backtest.selectSymbolIntervalDate'), status: 'warning' })
        return
      }
    }

    setRunning(true)
    
    const hasMode = taskMode !== 'single_strategy'
    const hasMultiStrategies = taskStrategies.length > 0
    const payload = dataSource === 'kline_file'
      ? { 
          mode: hasMode ? taskMode : undefined,
          bot_id: hasMode ? taskBotId || undefined : undefined,
          group_id: taskMode === 'hedge_group' ? taskGroupId || undefined : undefined,
          strategy: hasMultiStrategies || taskMode === 'hedge_group' ? undefined : strategyType,
          strategies: hasMultiStrategies ? taskStrategies : undefined,
          data_source: 'kline_file' as const, 
          kline_file: selectedKlineFile, 
          total_capital: totalCapital, 
          params: Object.keys(params).length ? params : undefined 
        }
      : dataSource === 'cache'
      ? (() => {
          // 从缓存列表中获取选中缓存的元信息，以便在任务列表中显示币种等信息
          const selectedCache = cachedKlines.find(c => c.name === selectedCacheName)
          return { 
            mode: hasMode ? taskMode : undefined,
            bot_id: hasMode ? taskBotId || undefined : undefined,
            group_id: taskMode === 'hedge_group' ? taskGroupId || undefined : undefined,
            strategy: hasMultiStrategies || taskMode === 'hedge_group' ? undefined : strategyType,
            strategies: hasMultiStrategies ? taskStrategies : undefined,
            data_source: 'cache' as const, 
            cache_name: selectedCacheName,
            symbol: selectedCache?.symbol,
            interval: selectedCache?.interval,
            start_time: selectedCache?.start ? new Date(selectedCache.start).toISOString() : undefined,
            end_time: selectedCache?.end ? new Date(selectedCache.end).toISOString() : undefined,
            total_capital: totalCapital, 
            params: Object.keys(params).length ? params : undefined 
          }
        })()
      : { 
          mode: hasMode ? taskMode : undefined,
          bot_id: hasMode ? taskBotId || undefined : undefined,
          group_id: taskMode === 'hedge_group' ? taskGroupId || undefined : undefined,
          strategy: hasMultiStrategies || taskMode === 'hedge_group' ? undefined : strategyType,
          strategies: hasMultiStrategies ? taskStrategies : undefined,
          symbol, 
          interval, 
          start_time: new Date(startDate).toISOString(), 
          end_time: (() => {
            const end = new Date(endDate)
            end.setHours(23, 59, 59, 999)
            return end.toISOString()
          })(),
          total_capital: totalCapital, 
          params: Object.keys(params).length ? params : undefined 
        }

    postBacktestTask(payload)
      .then((r) => {
        if (r.success) {
          toast({ title: t('backtest.backtestTaskCreated'), status: 'success' })
          loadTasks()
          setSelectedTaskId(r.task_id)
        }
      })
      .catch((e) => {
        const msg = e?.message || ''
        const is503 = msg.includes('503') || msg.includes('回测服务未初始化')
        let description: string | undefined
        if (is503) {
          try {
            const jsonMatch = msg.match(/\{[\s\S]*\}/)
            if (jsonMatch) {
              const body = JSON.parse(jsonMatch[0])
              if (body?.message) description = body.message
            }
          } catch (_) {}
          toast({
            title: t('backtest.backtestServiceUnavailable'),
            description: description || t('backtest.backtestServiceUnavailableDesc'),
            status: 'error',
            duration: 8000,
            isClosable: true,
          })
        } else {
          toast({ title: msg || t('backtest.createFailed'), status: 'error' })
        }
      })
      .finally(() => setRunning(false))
  }

  useEffect(() => {
    if (!selectedTaskId) {
      setResultData(null)
      setReportMd('')
      setKlinesData(null)
      return
    }
    
    // 先检查 tasks 列表中的状态，避免额外的 API 调用
    const taskFromList = tasks.find((t) => t.id === selectedTaskId)
    const isCompleted = taskFromList?.status === 'completed'
    
    const loadResultData = (retryCount = 0) => {
      getBacktestTaskResult(selectedTaskId)
        .then((data) => setResultData(data))
        .catch(() => {
          // 结果文件可能还没生成完成，重试最多 3 次，每次间隔 1 秒
          if (retryCount < 3) {
            setTimeout(() => loadResultData(retryCount + 1), 1000)
          }
        })
      getBacktestTaskReport(selectedTaskId).then(setReportMd).catch(() => setReportMd(''))
      getBacktestTaskKlines(selectedTaskId).then((res) => {
        const kls = res.klines || []
        const data = kls.map((k) => {
          const d = new Date(k.time * 1000)
          return { ts: k.time, time: d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric', hour: kls.length < 200 ? '2-digit' : undefined }), close: k.close }
        })
        setKlinesData({ klines: data, symbol: res.symbol })
      }).catch(() => setKlinesData(null))
    }
    
    if (isCompleted) {
      // 任务已完成，直接加载结果
      loadResultData()
    } else {
      // 任务未完成或不在列表中，查询最新状态
      getBacktestTask(selectedTaskId)
        .then((r) => {
          if (r.success && r.task?.status === 'completed') {
            loadResultData()
          } else {
            setResultData(null)
            setReportMd('')
            setKlinesData(null)
          }
        })
        .catch(() => {
          // API 调用失败时，如果列表中显示已完成，仍尝试加载
          if (isCompleted) {
            loadResultData()
          }
        })
    }
  }, [selectedTaskId, tasks])

  // 保存报告为图片并下载
  const handleSaveAsImage = async () => {
    if (!reportContentRef.current || !selectedTaskId) return
    
    setSavingImage(true)
    try {
      const element = reportContentRef.current
      
      // 克隆元素以避免修改原始 DOM
      const clone = element.cloneNode(true) as HTMLElement
      clone.style.position = 'absolute'
      clone.style.left = '-9999px'
      clone.style.top = '0'
      clone.style.width = `${element.scrollWidth}px`
      clone.style.overflow = 'visible'
      clone.style.maxHeight = 'none'
      clone.style.height = 'auto'
      clone.style.backgroundColor = '#ffffff'
      document.body.appendChild(clone)
      
      // 等待克隆元素渲染完成
      await new Promise(resolve => setTimeout(resolve, 200))
      
      const contentHeight = clone.scrollHeight
      const contentWidth = clone.scrollWidth
      console.log(`html2canvas: content=${contentWidth}x${contentHeight}`)
      
      const canvas = await html2canvas(clone, {
        backgroundColor: '#ffffff',
        scale: 2, // 2倍分辨率，生成高清图片
        useCORS: true,
        logging: false,
        allowTaint: true,
        foreignObjectRendering: false,
        removeContainer: false,
      })
      
      // 移除克隆元素
      document.body.removeChild(clone)
      
      // 检查 canvas 是否有效
      if (canvas.width === 0 || canvas.height === 0) {
        throw new Error(t('backtest.canvasGenerationFailed'))
      }
      
      // 下载图片
      const dataUrl = canvas.toDataURL('image/png')
      if (!dataUrl || dataUrl === 'data:,') {
        throw new Error(t('backtest.imageDataGenerationFailed'))
      }
      
      const link = document.createElement('a')
      link.download = `backtest_${selectedTaskId}.png`
      link.href = dataUrl
      link.click()
      
      toast({
        title: t('backtest.imageSaveSuccess'),
        description: t('backtest.imageSaveSuccessDesc', { id: selectedTaskId }),
        status: 'success',
        duration: 3000,
        isClosable: true,
      })
    } catch (error) {
      console.error('保存图片失败:', error)
      toast({
        title: t('backtest.imageSaveFailed'),
        description: String(error),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setSavingImage(false)
    }
  }

  const currentStrategyDef = strategies.find((s) => s.strategy_type === strategyType)
  const selectedOptimTask = selectedOptimTaskId ? optimTasks.find((t) => t.id === selectedOptimTaskId) : null
  const selectedTask = selectedTaskId ? tasks.find((task) => task.id === selectedTaskId) : null

  // 市场类型显示名称
  const marketTypeLabels: Record<string, string> = {
    futures: t('backtest.futures'),
    spot: t('backtest.spot'),
  }

  return (
    <Box>
      <Heading size="md" mb={4}>{t('backtest.title')}</Heading>

      {/* 预计算推荐区域 */}
      {precomputedResults.length > 0 && (
        <Card mb={4} borderColor="blue.200" borderWidth={1}>
          <CardHeader pb={2}>
            <Flex justify="space-between" align="center">
              <HStack>
                <StarIcon color="yellow.500" />
                <Text fontWeight="600">{t('backtest.smartPrecomputedTitle')}</Text>
                <Badge colorScheme="green">{t('backtest.readyCount', { count: precomputedResults.length })}</Badge>
              </HStack>
              <IconButton
                aria-label={t('backtest.expandCollapse')}
                size="sm"
                variant="ghost"
                icon={showPrecomputed ? <ChevronUpIcon /> : <ChevronDownIcon />}
                onClick={togglePrecomputed}
              />
            </Flex>
          </CardHeader>
          <Collapse in={showPrecomputed}>
            <CardBody pt={0}>
              <Text fontSize="sm" color="gray.600" mb={3}>
                {t('backtest.precomputedDescription')}
              </Text>
              <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={3}>
                {precomputedResults.slice(0, 6).map((result, idx) => (
                  <Box
                    key={`${result.symbol}-${result.strategy}-${idx}`}
                    p={3}
                    borderRadius="md"
                    border="1px solid"
                    borderColor="gray.200"
                    _hover={{ borderColor: 'blue.300', bg: 'blue.50' }}
                    cursor="pointer"
                    onClick={() => applyPrecomputedResult(result)}
                  >
                    <HStack justify="space-between" mb={2}>
                      <Text fontWeight="600">{result.symbol}</Text>
                      <Badge colorScheme={result.result?.metrics?.total_return && result.result.metrics.total_return > 0 ? 'green' : 'red'}>
                        {result.result?.metrics?.total_return?.toFixed(4) ?? '-'}%
                      </Badge>
                    </HStack>
                    <Text fontSize="sm" color="gray.600" mb={1}>
                      {t('backtest.strategyLabel')}: {t(`backtest.strategyNames.${result.strategy}`, { defaultValue: result.strategy })} | {result.market_type === 'spot' ? t('backtest.spot') : t('backtest.futures')}
                    </Text>
                    <HStack spacing={2} fontSize="xs" color="gray.500">
                      <Text>{t('backtest.sharpe')}: {result.result?.metrics?.sharpe_ratio?.toFixed(4) ?? '-'}</Text>
                      <Text>{t('backtest.drawdown')}: {result.result?.metrics?.max_drawdown?.toFixed(4) ?? '-'}%</Text>
                      <Text>{t('backtest.winRate')}: {result.result?.metrics?.win_rate?.toFixed(4) ?? '-'}%</Text>
                    </HStack>
                    {result.recommendation && (
                      <Progress
                        value={result.recommendation.confidence}
                        size="xs"
                        colorScheme={result.recommendation.confidence > 70 ? 'green' : result.recommendation.confidence > 50 ? 'yellow' : 'red'}
                        mt={2}
                      />
                    )}
                    <Text fontSize="xs" color="gray.400" mt={1}>
                      {t('backtest.confidence')}: {result.recommendation?.confidence?.toFixed(0) ?? '-'}%
                    </Text>
                  </Box>
                ))}
              </SimpleGrid>
              {precomputedResults.length > 6 && (
                <Text fontSize="sm" color="gray.500" mt={2} textAlign="center">
                  {t('backtest.moreResults', { count: precomputedResults.length - 6 })}
                </Text>
              )}
            </CardBody>
          </Collapse>
        </Card>
      )}

      {precomputedLoading && (
        <Alert status="info" mb={4}>
          <Spinner size="sm" mr={3} />
          <AlertDescription>{t('backtest.loadingSmartRecommendation')}</AlertDescription>
        </Alert>
      )}

      <Tabs>
        <TabList>
          <Tab>{t('backtest.newBacktest')}</Tab>
          <Tab>{t('backtest.taskList')}</Tab>
          <Tab>{t('backtest.paramOptimization')}</Tab>
          <Tab>{t('backtest.klineCache')}</Tab>
        </TabList>
        <TabPanels>
          <TabPanel>
            <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
              <Card>
                <CardHeader fontWeight="600">{t('backtest.tradingPairAndData')}</CardHeader>
                <CardBody>
                  {/* 数据来源选择 */}
                  <FormControl mb={4}>
                    <FormLabel>{t('backtest.dataSource')}</FormLabel>
                    <RadioGroup value={dataSource} onChange={(value) => setDataSource(value as 'time_range' | 'kline_file' | 'cache')}>
                      <Stack direction="row" spacing={4}>
                        <Radio value="time_range">{t('backtest.dataSourceTimeRange')}</Radio>
                        <Radio value="kline_file">{t('backtest.dataSourceKlineFile')}</Radio>
                        <Radio value="cache">{t('backtest.dataSourceCache')}</Radio>
                      </Stack>
                    </RadioGroup>
                  </FormControl>

                  {dataSource === 'time_range' && (
                    <>
                      {/* 交易所选择 */}
                  <FormControl mb={3}>
                    <FormLabel>{t('backtest.exchange')}</FormLabel>
                    <Select
                      placeholder={t('backtest.selectExchange')}
                      value={selectedExchange}
                      onChange={(e) => setSelectedExchange(e.target.value)}
                    >
                      {exchanges.map((ex) => (
                        <option key={ex.exchange} value={ex.exchange}>
                          {ex.exchange.toUpperCase()}
                          {ex.is_configured ? ` (${t('backtest.configured')})` : ''}
                        </option>
                      ))}
                    </Select>
                  </FormControl>

                  {/* 市场类型选择 */}
                  <FormControl mb={3}>
                    <FormLabel>{t('backtest.marketType')}</FormLabel>
                    <Select
                      value={selectedMarketType}
                      onChange={(e) => setSelectedMarketType(e.target.value)}
                      isDisabled={!selectedExchange}
                    >
                      {availableMarketTypes.map((mt) => (
                        <option key={mt} value={mt}>
                          {marketTypeLabels[mt] || mt}
                        </option>
                      ))}
                    </Select>
                  </FormControl>

                  {/* 交易对选择 */}
                  <FormControl mb={3}>
                    <FormLabel>{t('backtest.tradingPair')}</FormLabel>
                    <Select
                      placeholder={t('backtest.selectTradingPair')}
                      value={symbol}
                      onChange={(e) => setSymbol(e.target.value)}
                      isDisabled={!selectedExchange}
                    >
                      {symbols.map((s) => (
                        <option key={`${s.exchange}-${s.symbol}`} value={s.symbol}>
                          {s.symbol}
                          {s.is_configured ? ` (${t('backtest.configured')})` : ''}
                        </option>
                      ))}
                    </Select>
                  </FormControl>

                  {preset && (
                    <Box mb={3} p={2} bg="gray.50" borderRadius="md" fontSize="sm">
                      <Text>{t('backtest.presetRecommendation', { interval: preset.recommended_interval, days: preset.recommended_days?.join('/'), gridGap: preset.grid_gap_range })}</Text>
                    </Box>
                  )}
                  <FormControl mb={3}>
                    <FormLabel>{t('backtest.backtestDays')}</FormLabel>
                    <HStack mb={2} flexWrap="wrap" gap={2}>
                      {[3, 7, 14, 30, 90, 180, 365].map((d) => (
                        <Button
                          key={d}
                          size="sm"
                          variant={days === d ? 'solid' : 'outline'}
                          colorScheme={days === d ? 'blue' : 'gray'}
                          onClick={() => setDays(d)}
                        >
                          {t('backtest.nDays', { n: d })}
                        </Button>
                      ))}
                    </HStack>
                    <Box px={1}>
                      <Slider
                        aria-label={t('backtest.backtestDays')}
                        value={days}
                        min={1}
                        max={365}
                        step={1}
                        onChange={(v) => setDays(v)}
                      >
                        <SliderTrack>
                          <SliderFilledTrack />
                        </SliderTrack>
                        <SliderThumb boxSize={4} />
                      </Slider>
                      <HStack justify="space-between" mt={1} px={0} fontSize="xs" color="gray.500">
                        <Text>1</Text>
                        <Text fontWeight="600" color="blue.600">{t('backtest.nDays', { n: days })}</Text>
                        <Text>365</Text>
                      </HStack>
                    </Box>
                  </FormControl>
                  <FormControl mb={3}>
                    <FormLabel>{t('backtest.klineInterval')}</FormLabel>
                    <Select value={interval} onChange={(e) => setInterval(e.target.value)}>
                      {['1m', '3m', '5m', '15m', '30m', '1h', '4h', '1d'].map((i) => (
                        <option key={i} value={i}>{i}</option>
                      ))}
                    </Select>
                  </FormControl>
                  <HStack mb={3}>
                    <FormControl flex={1}>
                      <FormLabel>{t('backtest.startDate')}</FormLabel>
                      <Input type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
                    </FormControl>
                    <FormControl flex={1}>
                      <FormLabel>{t('backtest.endDate')}</FormLabel>
                      <Input type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} />
                    </FormControl>
                  </HStack>
                  <HStack>
                    <Button
                      size="sm"
                      leftIcon={cacheGenerating ? <Spinner size="sm" /> : <RepeatIcon />}
                      onClick={handleGenerateCache}
                      isDisabled={cacheGenerating || !symbol}
                    >
                      {cacheExists ? t('backtest.cached') : t('backtest.generateKlineCache')}
                    </Button>
                    {cacheExists && <Badge colorScheme="green"><CheckIcon mr={1} /> {t('backtest.cacheReady')}</Badge>}
                  </HStack>
                  <Text fontSize="xs" color="gray.500" mt={2}>
                    {t('backtest.cacheOptionalHint')}
                  </Text>
                    </>
                  )}

                  {dataSource === 'kline_file' && (
                    <>
                      <FormControl mb={3}>
                        <FormLabel>{t('backtest.dataSourceKlineFile')}</FormLabel>
                        <Button
                          size="sm"
                          leftIcon={<RepeatIcon />}
                          onClick={() => loadKlineFiles(true)}
                          isLoading={klineFilesLoading}
                          mb={2}
                        >
                          {t('backtest.refreshFileList')}
                        </Button>
                        {klineFilesLoading ? (
                          <Flex justify="center" py={4}><Spinner /></Flex>
                        ) : availableKlineFiles.length === 0 ? (
                          <Text color="gray.500" fontSize="sm">{t('backtest.noKlineFilesAvailable')}</Text>
                        ) : (
                          <Box maxH="350px" overflowY="auto" border="1px" borderColor="gray.200" borderRadius="md">
                            <Table size="sm">
                              <Thead>
                                <Tr>
                                  <Th>{t('backtest.select')}</Th>
                                  <Th>{t('backtest.tradingPair')}</Th>
                                  <Th>{t('backtest.interval')}</Th>
                                  <Th>{t('backtest.timeRange')}</Th>
                                  <Th>{t('backtest.depth')}</Th>
                                  <Th>{t('backtest.klineCount')}</Th>
                                  <Th>{t('backtest.source')}</Th>
                                </Tr>
                              </Thead>
                              <Tbody>
                                {availableKlineFiles.map((file) => (
                                  <Tr key={file.filename}>
                                    <Td>
                                      <Radio
                                        isChecked={selectedKlineFile === file.filename}
                                        onChange={() => setSelectedKlineFile(file.filename)}
                                      />
                                    </Td>
                                    <Td>{file.symbol}</Td>
                                    <Td>{file.interval}</Td>
                                    <Td>
                                      <Tooltip label={file.filename}>
                                        <Text fontSize="xs" noOfLines={2} maxW="120px">{file.time_range}</Text>
                                      </Tooltip>
                                    </Td>
                                    <Td>
                                      {file.has_depth ? (
                                        <Badge colorScheme="green">{t('backtest.hasDepthYes')}</Badge>
                                      ) : (
                                        <Badge variant="outline">{t('backtest.hasDepthNo')}</Badge>
                                      )}
                                    </Td>
                                    <Td>{file.candle_count.toLocaleString()}</Td>
                                    <Td>
                                      <Badge variant="outline" fontSize="xs">
                                        {file.source === 'collector' ? t('backtest.sourceCollector') : 
                                         file.source === 'backtest_cache' ? t('backtest.sourceCache') : t('backtest.sourceManual')}
                                      </Badge>
                                    </Td>
                                  </Tr>
                                ))}
                              </Tbody>
                            </Table>
                          </Box>
                        )}
                      </FormControl>
                    </>
                  )}

                  {dataSource === 'cache' && (
                    <>
                      <FormControl mb={3}>
                        <FormLabel>{t('backtest.dataSourceCache')}</FormLabel>
                        <Button
                          size="sm"
                          leftIcon={<RepeatIcon />}
                          onClick={loadCachedKlines}
                          isLoading={cachedKlinesLoading}
                          mb={2}
                        >
                          {t('backtest.refreshCacheList')}
                        </Button>
                        {cachedKlinesLoading ? (
                          <Flex justify="center" py={4}><Spinner /></Flex>
                        ) : cachedKlines.length === 0 ? (
                          <Text color="gray.500" fontSize="sm">{t('backtest.noBacktestCache')}</Text>
                        ) : (
                          <Box maxH="300px" overflowY="auto" border="1px" borderColor="gray.200" borderRadius="md">
                            <Table size="sm">
                              <Thead>
                                <Tr>
                                  <Th>{t('backtest.select')}</Th>
                                  <Th>{t('backtest.cacheName')}</Th>
                                  <Th>{t('backtest.tradingPair')}</Th>
                                  <Th>{t('backtest.interval')}</Th>
                                  <Th>{t('backtest.klineCount')}</Th>
                                </Tr>
                              </Thead>
                              <Tbody>
                                {cachedKlines.map((cache) => (
                                  <Tr key={cache.name}>
                                    <Td>
                                      <Radio
                                        isChecked={selectedCacheName === cache.name}
                                        onChange={() => setSelectedCacheName(cache.name)}
                                      />
                                    </Td>
                                    <Td>
                                      <Tooltip label={cache.name}>
                                        <Text fontSize="xs" noOfLines={1} maxW="120px">{cache.name}</Text>
                                      </Tooltip>
                                    </Td>
                                    <Td>{cache.symbol || '-'}</Td>
                                    <Td>{cache.interval || '-'}</Td>
                                    <Td>{cache.candles.toLocaleString()}</Td>
                                  </Tr>
                                ))}
                              </Tbody>
                            </Table>
                          </Box>
                        )}
                      </FormControl>
                    </>
                  )}
                </CardBody>
              </Card>
              <Card>
                <CardHeader fontWeight="600">{t('backtest.strategyAndParams')}</CardHeader>
                <CardBody>
                  <FormControl mb={4}>
                    <FormLabel>{t('backtest.totalCapital')}</FormLabel>
                    <NumberInput value={totalCapital} min={1} onChange={(_: string, v: number) => setTotalCapital(v)}>
                      <NumberInputField />
                    </NumberInput>
                    <Text fontSize="xs" color="gray.500" mt={1}>{t('backtest.totalCapitalHint')}</Text>
                  </FormControl>
                  <FormControl mb={3}>
                    <FormLabel>{t('backtest.strategy')}</FormLabel>
                    <Select
                      placeholder={t('backtest.selectStrategyPlaceholder')}
                      value={strategyType}
                      onChange={(e) => {
                        setStrategyType(e.target.value)
                        setParams({}) // 清空参数，loadConfigParams 会自动触发
                        setSmartRecommendation(null) // 清空智能推荐
                      }}
                    >
                      {strategies.map((s) => (
                        <option key={s.strategy_type} value={s.strategy_type}>{t(`backtest.strategyNames.${s.strategy_type}`, { defaultValue: s.name })}</option>
                      ))}
                    </Select>
                  </FormControl>

                  {/* 智能推荐按钮 */}
                  {symbol && strategyType && (
                    <Box mb={3}>
                      <Button
                        size="sm"
                        colorScheme="purple"
                        leftIcon={smartLoading ? <Spinner size="sm" /> : <StarIcon />}
                        onClick={handleGetSmartRecommendation}
                        isDisabled={smartLoading}
                        mr={2}
                      >
                        {t('backtest.getSmartRecommendation')}
                      </Button>
                      {smartRecommendation && (
                        <Button
                          size="sm"
                          colorScheme="green"
                          onClick={() => applySmartRecommendation(smartRecommendation)}
                        >
                          {t('backtest.applyRecommendation')}
                        </Button>
                      )}
                    </Box>
                  )}

                  {/* 智能推荐结果展示 */}
                  {smartRecommendation && (
                    <Alert status="info" mb={3} borderRadius="md">
                      <Box flex="1">
                        <AlertTitle fontSize="sm">
                          {t('backtest.smartRecommendationTitle', { confidence: smartRecommendation.confidence.toFixed(0) })}
                        </AlertTitle>
                        <AlertDescription fontSize="xs">
                          <Text mb={1}>{t('backtest.currentPrice')}: ${smartRecommendation.current_price.toFixed(2)}</Text>
                          {smartRecommendation.volatility && (
                            <Text mb={1}>
                              {t('backtest.volatility7d')}: {smartRecommendation.volatility.volatility_7d?.toFixed(1)}% |
                              {t('backtest.trend')}: {smartRecommendation.volatility.trend_direction === 'up' ? t('backtest.trendUp') : smartRecommendation.volatility.trend_direction === 'down' ? t('backtest.trendDown') : t('backtest.trendSideways')}
                            </Text>
                          )}
                          <Text>{smartRecommendation.reasoning}</Text>
                        </AlertDescription>
                      </Box>
                    </Alert>
                  )}

                  {configParamsLoading && (
                    <HStack mb={2}>
                      <Spinner size="sm" />
                      <Text fontSize="sm" color="gray.500">{t('backtest.loadingConfigParams')}</Text>
                    </HStack>
                  )}
                  {currentStrategyDef?.params
                    ?.filter((p) => p.name !== 'total_capital')
                    ?.map((p) => (
                    <FormControl key={p.name} mb={2}>
                      <FormLabel fontSize="sm">{t(`backtest.paramLabels.${p.name}`, { defaultValue: p.label })}{p.required ? ' *' : ''}</FormLabel>
                      {p.type === 'number' && (
                        <NumberInput
                          value={(params[p.name] as number) ?? (p.default as number)}
                          min={p.min}
                          max={p.max}
                          step={p.step ?? 0.1}
                          precision={p.step ? Math.max(0, Math.round(-Math.log10(p.step))) : 1}
                          onChange={(_: string, v: number) => setParams((prev) => ({ ...prev, [p.name]: v }))}
                        >
                          <NumberInputField />
                        </NumberInput>
                      )}
                      {p.hint && (
                        <Text fontSize="xs" color="gray.500" mt={1}>
                          {t(`backtest.paramHints.${p.name}`, { defaultValue: p.hint })}
                        </Text>
                      )}
                      {p.unit && <Text as="span" fontSize="xs" color="gray.500" ml={2}>{p.unit}</Text>}
                    </FormControl>
                  ))}
                  <Button
                    colorScheme="blue"
                    onClick={handleRunBacktest}
                    isLoading={running}
                    isDisabled={!strategyType || totalCapital <= 0}
                  >
                    {t('backtest.startBacktest')}
                  </Button>
                </CardBody>
              </Card>
            </SimpleGrid>
          </TabPanel>
          <TabPanel>
            {tasksLoading ? (
              <Flex justify="center" py={8}><Spinner /></Flex>
            ) : (
              <Box overflowX="auto">
                <Table size="sm">
                  <Thead>
                    <Tr>
                      <Th>{t('backtest.status')}</Th>
                      <Th>{t('backtest.strategy')}</Th>
                      <Th>{t('backtest.tradingPair')}</Th>
                      <Th>{t('backtest.interval')}</Th>
                      <Th>{t('backtest.timeRange')}</Th>
                      <Th>{t('backtest.createdAt')}</Th>
                      <Th>{t('backtest.actions')}</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {tasks.map((task) => (
                      <Tr
                        key={task.id}
                        bg={task.id === selectedTaskId ? 'blue.50' : undefined}
                        _dark={{ bg: task.id === selectedTaskId ? 'blue.900' : undefined }}
                      >
                        <Td>
                          <Tooltip
                            label={task.status === 'failed' && task.error ? task.error : undefined}
                            isDisabled={task.status !== 'failed' || !task.error}
                            placement="top"
                            maxW="320px"
                          >
                            <Badge
                              colorScheme={
                                task.status === 'completed' ? 'green' : task.status === 'failed' ? 'red' : task.status === 'running' ? 'blue' : 'gray'
                              }
                            >
                              {task.status}
                            </Badge>
                          </Tooltip>
                        </Td>
                        <Td>{task.mode === 'hedge_group' ? t('backtest.hedgeMode') : t(`backtest.strategyNames.${task.strategy}`, { defaultValue: task.strategy })}</Td>
                        <Td>{task.symbol}</Td>
                        <Td>{task.interval}</Td>
                        <Td>{formatDate(task.start_time)} ~ {formatDate(task.end_time)}</Td>
                        <Td>{formatDate(task.created_at)}</Td>
                        <Td>
                          <HStack>
                            <Button
                              size="xs"
                              onClick={() => {
                                setSelectedTaskId(task.id)
                                reportModal.onOpen()
                              }}
                            >
                              {t('backtest.view')}
                            </Button>
                            {task.status === 'completed' && (
                              <Tooltip label={t('backtest.downloadReport')}>
                                <IconButton
                                  aria-label={t('backtest.downloadReport')}
                                  size="xs"
                                  icon={<DownloadIcon />}
                                  onClick={() => {
                                    getBacktestTaskReport(task.id, true).then((md) => {
                                      const blob = new Blob([md], { type: 'text/markdown' })
                                      const a = document.createElement('a')
                                      a.href = URL.createObjectURL(blob)
                                      a.download = `backtest_${task.id}.md`
                                      a.click()
                                    })
                                  }}
                                />
                              </Tooltip>
                            )}
                            <IconButton
                              aria-label={t('backtest.delete')}
                              size="xs"
                              icon={<DeleteIcon />}
                              onClick={() => {
                                deleteBacktestTask(task.id).then(() => loadTasks())
                                if (selectedTaskId === task.id) {
                                  setSelectedTaskId(null)
                                  reportModal.onClose()
                                }
                              }}
                            />
                          </HStack>
                        </Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </Box>
            )}
            <Modal
              isOpen={reportModal.isOpen}
              onClose={() => {
                reportModal.onClose()
                setSelectedTaskId(null)
              }}
              size="4xl"
              scrollBehavior="inside"
            >
              <ModalOverlay />
              <ModalContent maxH="90vh">
                <ModalHeader>{t('backtest.backtestResult', { id: selectedTaskId ?? '-' })}</ModalHeader>
                <ModalCloseButton />
                <ModalBody pb={6} overflowY="auto">
                  <Box ref={reportContentRef} bg="white" _dark={{ bg: 'gray.900' }}>
                  {selectedTaskId && (() => {
                    const sel = tasks.find((t) => t.id === selectedTaskId)
                    if (sel?.status === 'failed' && sel?.error) {
                      return (
                        <Alert status="error" borderRadius="md" mb={4}>
                          <AlertIcon />
                          <Box>
                            <AlertTitle>{t('backtest.taskFailed')}</AlertTitle>
                            <AlertDescription>{sel.error}</AlertDescription>
                          </Box>
                        </Alert>
                      )
                    }
                    return null
                  })()}
                  {resultData && typeof resultData === 'object' && ('result' in resultData || 'comparison' in resultData || 'multi_result' in resultData || 'hedge_result' in resultData) && (
                    <Box mb={4}>
                      {(() => {
                        const data = resultData as BacktestResultData
                        const multi = data.multi_result
                        const hedge = data.hedge_result
                        const comp = data.comparison
                        const hasComparison = comp != null

                        const metricBox = (
                          m: Record<string, unknown> | undefined,
                          trades: Array<{ type?: string; quantity?: number }>,
                          endPrice: number,
                          finalCapital: number,
                          label?: string
                        ) => {
                          if (!m) return null
                          let endPosQty = 0
                          for (const t of trades) {
                            if (t.type === 'buy') endPosQty += t.quantity ?? 0
                            else if (t.type === 'sell') endPosQty -= t.quantity ?? 0
                          }
                          if (endPosQty < 0) endPosQty = 0
                          const endPosValue = endPosQty * endPrice
                          const endCashUSDT = Math.max(0, finalCapital - endPosValue)
                          return (
                            <Box key={label || 'single'}>
                              {label && <Text fontSize="xs" fontWeight="600" mb={2} color="gray.600" _dark={{ color: 'gray.400' }}>{label}</Text>}
                              <SimpleGrid columns={[2, 3, 5]} spacing={2}>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.totalReturn')}</Text><Text fontWeight="600">{String(m.total_return ?? '-')}%</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.maxDrawdown')}</Text><Text fontWeight="600">{String(m.max_drawdown ?? '-')}%</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.sharpeRatio')}</Text><Text fontWeight="600">{String(m.sharpe_ratio ?? '-')}</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.totalTrades')}</Text><Text fontWeight="600">{String(m.total_trades ?? '-')}</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.buySell')}</Text><Text fontWeight="600">{String(m.buy_count ?? '-')} / {String(m.sell_count ?? '-')}</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.endPosition')}</Text><Text fontWeight="600">{endPosQty.toFixed(6)}</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.endPositionValue')}</Text><Text fontWeight="600">{endPosValue.toFixed(4)} USDT</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.endUsdt')}</Text><Text fontWeight="600">{endCashUSDT.toFixed(4)}</Text></Box>
                              </SimpleGrid>
                            </Box>
                          )
                        }

                        if (hasComparison && comp?.no_risk_result && comp?.with_risk_result) {
                          const noRisk = comp.no_risk_result
                          const withRisk = comp.with_risk_result
                          const cm = comp.comparison
                          const interventions = comp.with_risk_result?.risk_interventions ?? []
                          
                          // 计算期末持仓相关数据
                          const calcEndPos = (trades: typeof noRisk.trades, endPrice: number, finalCapital: number) => {
                            let endPosQty = 0
                            for (const t of (trades ?? [])) {
                              if (t.type === 'buy') endPosQty += t.quantity ?? 0
                              else if (t.type === 'sell') endPosQty -= t.quantity ?? 0
                            }
                            if (endPosQty < 0) endPosQty = 0
                            const endPosValue = endPosQty * endPrice
                            const endCashUSDT = Math.max(0, finalCapital - endPosValue)
                            return { endPosQty, endPosValue, endCashUSDT }
                          }
                          const noRiskPos = calcEndPos(noRisk.trades, noRisk.price_curve?.end_price ?? 0, noRisk.final_capital ?? 0)
                          const withRiskPos = calcEndPos(withRisk.trades, withRisk.price_curve?.end_price ?? 0, withRisk.final_capital ?? 0)
                          const noRiskM = noRisk.metrics
                          const withRiskM = withRisk.metrics
                          
                          // 对比行组件
                          const compareRow = (label: string, noVal: string | number, withVal: string | number, unit?: string) => (
                            <Tr>
                              <Td fontWeight="500">{label}</Td>
                              <Td isNumeric>{noVal}{unit}</Td>
                              <Td isNumeric>{withVal}{unit}</Td>
                            </Tr>
                          )
                          
                          return (
                            <>
                              <Text fontSize="sm" fontWeight="600" mb={3}>{t('backtest.riskComparison')}</Text>
                              <Box overflowX="auto" mb={4}>
                                <Table size="sm" variant="simple">
                                  <Thead>
                                    <Tr>
                                      <Th>{t('backtest.metric')}</Th>
                                      <Th isNumeric>{t('backtest.noRiskControl')}</Th>
                                      <Th isNumeric>{t('backtest.withRiskControl')}</Th>
                                    </Tr>
                                  </Thead>
                                  <Tbody>
                                    {compareRow(t('backtest.totalReturn'), String((noRiskM as Record<string, unknown>)?.total_return ?? '-'), String((withRiskM as Record<string, unknown>)?.total_return ?? '-'), '%')}
                                    {compareRow(t('backtest.maxDrawdown'), String((noRiskM as Record<string, unknown>)?.max_drawdown ?? '-'), String((withRiskM as Record<string, unknown>)?.max_drawdown ?? '-'), '%')}
                                    {compareRow(t('backtest.sharpeRatio'), String((noRiskM as Record<string, unknown>)?.sharpe_ratio ?? '-'), String((withRiskM as Record<string, unknown>)?.sharpe_ratio ?? '-'))}
                                    {compareRow(t('backtest.totalTrades'), String((noRiskM as Record<string, unknown>)?.total_trades ?? '-'), String((withRiskM as Record<string, unknown>)?.total_trades ?? '-'))}
                                    {compareRow(t('backtest.buySell'), `${(noRiskM as Record<string, unknown>)?.buy_count ?? '-'} / ${(noRiskM as Record<string, unknown>)?.sell_count ?? '-'}`, `${(withRiskM as Record<string, unknown>)?.buy_count ?? '-'} / ${(withRiskM as Record<string, unknown>)?.sell_count ?? '-'}`)}
                                    {compareRow(t('backtest.endPosition'), noRiskPos.endPosQty.toFixed(6), withRiskPos.endPosQty.toFixed(6))}
                                    {compareRow(t('backtest.endPositionValue'), noRiskPos.endPosValue.toFixed(4), withRiskPos.endPosValue.toFixed(4), ' USDT')}
                                    {compareRow(t('backtest.endUsdt'), noRiskPos.endCashUSDT.toFixed(4), withRiskPos.endCashUSDT.toFixed(4))}
                                  </Tbody>
                                </Table>
                              </Box>
                              {(cm?.risk_intervention_count ?? 0) > 0 && (() => {
                                const maxDisplay = 50
                                const displayList = interventions.slice(0, maxDisplay)
                                const hasMore = interventions.length > maxDisplay
                                return (
                                  <Box mb={4}>
                                    <Text fontSize="sm" fontWeight="600" mb={2}>
                                      {t('backtest.riskInterventionRecord', { count: cm?.risk_intervention_count ?? 0, skipped: cm?.skipped_signals ?? 0 })}
                                      {hasMore && <Text as="span" color="gray.500" fontWeight="normal">{t('backtest.showingFirst', { count: maxDisplay })}</Text>}
                                    </Text>
                                    <Box overflowX="auto" maxH="200px" overflowY="auto">
                                      <Table size="sm">
                                        <Thead>
                                          <Tr>
                                            <Th>{t('backtest.time')}</Th>
                                            <Th>{t('backtest.reason')}</Th>
                                            <Th>{t('backtest.type')}</Th>
                                            <Th>{t('backtest.durationKlines')}</Th>
                                            <Th>{t('backtest.skippedBuys')}</Th>
                                          </Tr>
                                        </Thead>
                                        <Tbody>
                                          {displayList.map((inv, idx) => (
                                            <Tr key={idx}>
                                              <Td fontSize="xs">{inv.time_str ?? '-'}</Td>
                                              <Td fontSize="xs">{inv.reason ?? '-'}</Td>
                                              <Td fontSize="xs">{inv.risk_type ?? '-'}</Td>
                                              <Td fontSize="xs">{inv.duration ?? '-'}</Td>
                                              <Td fontSize="xs">{inv.skipped_buys ?? '-'}</Td>
                                            </Tr>
                                          ))}
                                        </Tbody>
                                      </Table>
                                    </Box>
                                  </Box>
                                )
                              })()}
                            </>
                          )
                        }

                        if (hedge) {
                          return (
                            <SimpleGrid columns={[2, 3, 5]} spacing={2}>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.hedgeMode')}</Text><Text fontWeight="600">{selectedTask?.group_id || taskGroupId || t('backtest.truePair')}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.totalReturn')}</Text><Text fontWeight="600">{hedge.total_return_pct?.toFixed(4) ?? '-'}%</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.maxDrawdown')}</Text><Text fontWeight="600">{hedge.max_drawdown_pct?.toFixed(4) ?? '-'}%</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.endUsdt')}</Text><Text fontWeight="600">{hedge.final_equity?.toFixed(4) ?? '-'}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.totalCapital')}</Text><Text fontWeight="600">{hedge.initial_capital?.toFixed(4) ?? '-'}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.rebalanceCount')}</Text><Text fontWeight="600">{hedge.rebalance_count ?? '-'}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.alignedPoints')}</Text><Text fontWeight="600">{hedge.aligned_points ?? '-'}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.longSymbol')}</Text><Text fontWeight="600">{hedge.long_symbol ?? '-'}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.shortSymbol')}</Text><Text fontWeight="600">{hedge.short_symbol ?? '-'}</Text></Box>
                            </SimpleGrid>
                          )
                        }

                        if (multi) {
                          const strategyCount = Object.keys(multi.stats_by_strategy || {}).length
                          return (
                            <SimpleGrid columns={[2, 3, 5]} spacing={2}>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.totalReturn')}</Text><Text fontWeight="600">{multi.total_return_pct?.toFixed(4) ?? '-'}%</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.maxDrawdown')}</Text><Text fontWeight="600">{multi.risk_metrics?.max_drawdown_pct?.toFixed(4) ?? '-'}%</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.sharpeRatio')}</Text><Text fontWeight="600">{multi.risk_metrics?.sharpe_ratio?.toFixed(4) ?? '-'}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.totalTrades')}</Text><Text fontWeight="600">{multi.total_trades ?? '-'}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.strategyLabel')}</Text><Text fontWeight="600">{strategyCount}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.endUsdt')}</Text><Text fontWeight="600">{multi.final_equity?.toFixed(4) ?? '-'}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.totalCapital')}</Text><Text fontWeight="600">{multi.initial_capital?.toFixed(4) ?? '-'}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.feeRate')}</Text><Text fontWeight="600">{multi.total_fees?.toFixed(4) ?? '-'}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.fundingRate')}</Text><Text fontWeight="600">{multi.total_funding?.toFixed(4) ?? '-'}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">{t('backtest.winRate')}</Text><Text fontWeight="600">{multi.risk_metrics?.win_rate?.toFixed(4) ?? '-'}%</Text></Box>
                            </SimpleGrid>
                          )
                        }

                        const res = data.result
                        const m = res?.metrics
                        const trades = res?.trades ?? []
                        const endPrice = res?.price_curve?.end_price ?? 0
                        const finalCapital = res?.final_capital ?? 0
                        return metricBox(m, trades, endPrice, finalCapital)
                      })()}
                    </Box>
                  )}
                  {klinesData && klinesData.klines.length > 0 && (
                    <Box mb={4}>
                      <Text fontSize="sm" fontWeight="600" mb={2}>{t('backtest.klineTrend')}</Text>
                      <SimpleGrid columns={2} spacing={3}>
                        {(() => {
                          const kls = klinesData.klines
                          const n = kls.length
                          const seg = Math.max(1, Math.ceil(n / 4))
                          return [0, 1, 2, 3].map((i) => {
                            const start = i * seg
                            const end = Math.min((i + 1) * seg, n)
                            const segData = kls.slice(start, end)
                            if (segData.length === 0) return null
                            return (
                              <Box key={i} h="160px" bg="gray.50" borderRadius="md" p={2} _dark={{ bg: 'gray.800' }}>
                                <Text fontSize="xs" color="gray.500" mb={1}>{t('backtest.segment', { n: i + 1 })}</Text>
                                <ResponsiveContainer width="100%" height={130}>
                                  <LineChart data={segData} margin={{ top: 5, right: 5, left: -20, bottom: 0 }}>
                                    <CartesianGrid strokeDasharray="2 2" stroke="rgba(0,0,0,0.05)" />
                                    <XAxis dataKey="time" tick={{ fontSize: 9 }} interval="preserveStartEnd" />
                                    <YAxis domain={['auto', 'auto']} tick={{ fontSize: 9 }} width={45} />
                                    <RechartsTooltip formatter={(v: number) => [v.toFixed(2), t('backtest.close')]} />
                                    <Line type="monotone" dataKey="close" stroke="#3182ce" dot={false} strokeWidth={1.5} />
                                  </LineChart>
                                </ResponsiveContainer>
                              </Box>
                            )
                          })
                        })()}
                      </SimpleGrid>
                    </Box>
                  )}
                  {reportMd && (
                    <Box
                      p={4}
                      bg="gray.50"
                      borderRadius="md"
                      overflowX="auto"
                      fontSize="sm"
                      _dark={{ bg: 'gray.800' }}
                      sx={{
                        '& h1': { fontSize: 'xl', fontWeight: 'bold', mt: 2, mb: 2 },
                        '& h2': { fontSize: 'lg', fontWeight: 'semibold', mt: 3, mb: 2 },
                        '& h3': { fontSize: 'md', fontWeight: 'semibold', mt: 2, mb: 1 },
                        '& p': { mb: 2 },
                        '& ul, & ol': { pl: 6, mb: 2 },
                        '& table': { borderCollapse: 'collapse', width: '100%', mb: 3 },
                        '& th, & td': { border: '1px solid', borderColor: 'gray.300', px: 3, py: 2, textAlign: 'left' },
                        '& th': { bg: 'gray.200', fontWeight: 'semibold' },
                        '& code': { bg: 'gray.200', px: 1, py: 0.5, borderRadius: 'sm' },
                        '& pre': { bg: 'gray.200', p: 3, borderRadius: 'md', overflowX: 'auto', whiteSpace: 'pre-wrap' },
                      }}
                    >
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{reportMd}</ReactMarkdown>
                    </Box>
                  )}
                  {selectedTaskId && !resultData && !reportMd && !tasks.find((t) => t.id === selectedTaskId)?.error && (
                    <Flex justify="center" py={8}>
                      <Spinner />
                      <Text ml={3} color="gray.500">{t('common.loading')}</Text>
                    </Flex>
                  )}
                  </Box>
                </ModalBody>
                {selectedTaskId && (
                  <ModalFooter gap={2}>
                    {tasks.find((t) => t.id === selectedTaskId)?.status === 'completed' && (
                      <>
                        <Button
                          size="sm"
                          leftIcon={<DownloadIcon />}
                          variant="outline"
                          onClick={handleSaveAsImage}
                          isLoading={savingImage}
                          loadingText={t('backtest.generating')}
                        >
                          {t('backtest.saveAsImage')}
                        </Button>
                        <Button
                          size="sm"
                          leftIcon={<DownloadIcon />}
                          variant="outline"
                          onClick={() => {
                            getBacktestTaskReport(selectedTaskId, true).then((md) => {
                              const blob = new Blob([md], { type: 'text/markdown' })
                              const a = document.createElement('a')
                              a.href = URL.createObjectURL(blob)
                              a.download = `backtest_${selectedTaskId}.md`
                              a.click()
                            })
                          }}
                        >
                          {t('backtest.downloadReport')}
                        </Button>
                      </>
                    )}
                  </ModalFooter>
                )}
              </ModalContent>
            </Modal>
          </TabPanel>
          <TabPanel>
            <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
              <Card>
                <CardHeader fontWeight="600">{t('backtest.baseConfig')}</CardHeader>
                <CardBody>
                  <Text fontSize="sm" color="gray.600" mb={3}>
                    {t('backtest.optimDescription')}
                  </Text>
                  <FormControl mb={2}>
                    <FormLabel fontSize="sm">{t('backtest.tradingPair')}</FormLabel>
                    <Select placeholder={t('backtest.selectTradingPair')} value={symbol} onChange={(e) => setSymbol(e.target.value)} isDisabled={!selectedExchange}>
                      {symbols.map((s) => (
                        <option key={`${s.exchange}-${s.symbol}`} value={s.symbol}>{s.symbol}</option>
                      ))}
                    </Select>
                  </FormControl>
                  <FormControl mb={2}>
                    <FormLabel fontSize="sm">{t('backtest.strategy')}</FormLabel>
                    <Select placeholder={t('backtest.selectStrategyPlaceholder')} value={strategyType} onChange={(e) => setStrategyType(e.target.value)}>
                      {strategies.filter((s) => ['grid', 'momentum', 'mean_reversion', 'trend_following', 'dca', 'martingale'].includes(s.strategy_type)).map((s) => (
                        <option key={s.strategy_type} value={s.strategy_type}>{t(`backtest.strategyNames.${s.strategy_type}`, { defaultValue: s.name })}</option>
                      ))}
                    </Select>
                  </FormControl>
                  <FormControl mb={2}>
                    <FormLabel fontSize="sm">{t('backtest.klineInterval')}</FormLabel>
                    <Select value={interval} onChange={(e) => setInterval(e.target.value)}>
                      {['1m', '3m', '5m', '15m', '30m', '1h', '4h', '1d'].map((i) => (
                        <option key={i} value={i}>{i}</option>
                      ))}
                    </Select>
                  </FormControl>
                  <HStack mb={2}>
                    <FormControl flex={1}>
                      <FormLabel fontSize="sm">{t('backtest.startDate')}</FormLabel>
                      <Input type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
                    </FormControl>
                    <FormControl flex={1}>
                      <FormLabel fontSize="sm">{t('backtest.endDate')}</FormLabel>
                      <Input type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} />
                    </FormControl>
                  </HStack>
                  <FormControl mb={3}>
                    <FormLabel fontSize="sm">{t('backtest.initialCapital')}</FormLabel>
                    <NumberInput value={totalCapital} min={1} onChange={(_: string, v: number) => setTotalCapital(v)}>
                      <NumberInputField />
                    </NumberInput>
                  </FormControl>
                  <Button
                    colorScheme="purple"
                    onClick={handleStartOptim}
                    isLoading={optimRunning}
                    isDisabled={!symbol || !strategyType || !interval || !startDate || !endDate || totalCapital <= 0}
                  >
                    {t('backtest.startParamOptimization')}
                  </Button>
                  <Text fontSize="xs" color="gray.500" mt={2}>
                    {t('backtest.optimHint')}
                  </Text>
                </CardBody>
              </Card>
              <Card>
                <CardHeader fontWeight="600">{t('backtest.optimTaskList')}</CardHeader>
                <CardBody>
                  {optimTasksLoading ? (
                    <Flex justify="center" py={6}><Spinner /></Flex>
                  ) : (
                    <Box overflowX="auto">
                      <Table size="sm">
                        <Thead>
                          <Tr>
                            <Th>{t('backtest.status')}</Th>
                            <Th>{t('backtest.strategy')}</Th>
                            <Th>{t('backtest.tradingPair')}</Th>
                            <Th>{t('backtest.combos')}</Th>
                            <Th>{t('backtest.progress')}</Th>
                            <Th>{t('backtest.actions')}</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {optimTasks.map((task) => (
                            <Tr key={task.id}>
                              <Td>
                                <Badge
                                  colorScheme={
                                    task.status === 'completed' ? 'green' : task.status === 'failed' ? 'red' : task.status === 'running' ? 'blue' : 'gray'
                                  }
                                >
                                  {task.status}
                                </Badge>
                              </Td>
                              <Td>{t(`backtest.strategyNames.${task.strategy}`, { defaultValue: task.strategy })}</Td>
                              <Td>{task.symbol}</Td>
                              <Td>{task.total_combos}</Td>
                              <Td>{task.progress}%</Td>
                              <Td>
                                <HStack>
                                  <Button
                                    size="xs"
                                    onClick={() => handleViewOptimResult(task.id)}
                                    isDisabled={task.status !== 'completed'}
                                  >
                                    {t('backtest.view')}
                                  </Button>
                                  <IconButton
                                    aria-label={t('backtest.delete')}
                                    size="xs"
                                    icon={<DeleteIcon />}
                                    onClick={() => deleteOptimTask(task.id).then(() => loadOptimTasks())}
                                  />
                                </HStack>
                              </Td>
                            </Tr>
                          ))}
                        </Tbody>
                      </Table>
                    </Box>
                  )}
                  {!optimTasksLoading && optimTasks.length === 0 && (
                    <Text color="gray.500" py={4}>{t('backtest.noOptimTasks')}</Text>
                  )}
                </CardBody>
              </Card>
            </SimpleGrid>
            <OptimResultModal
              isOpen={optimResultModal.isOpen}
              onClose={() => {
                optimResultModal.onClose()
                setSelectedOptimTaskId(null)
              }}
              result={optimResultData}
              task={selectedOptimTask}
              isLoading={optimResultModal.isOpen && selectedOptimTaskId && !optimResultData}
            />
          </TabPanel>
          <TabPanel>
            <Card>
              <CardHeader>
                <Flex justify="space-between" align="center">
                  <Text fontWeight="600">{t('backtest.cachedKlines')}</Text>
                  <Button
                    size="sm"
                    leftIcon={<RepeatIcon />}
                    onClick={loadCachedKlines}
                    isLoading={cachedKlinesLoading}
                  >
                    {t('backtest.refresh')}
                  </Button>
                </Flex>
              </CardHeader>
              <CardBody>
                {cachedKlinesLoading && cachedKlines.length === 0 ? (
                  <Flex justify="center" py={6}><Spinner /></Flex>
                ) : cachedKlines.length === 0 ? (
                  <Text color="gray.500">{t('backtest.noKlineCache')}</Text>
                ) : (
                  <Box overflowX="auto">
                    <Table size="sm">
                        <Thead>
                          <Tr>
                            <Th>{t('backtest.cacheName')}</Th>
                            <Th>{t('backtest.tradingPair')}</Th>
                            <Th>{t('backtest.interval')}</Th>
                            <Th>{t('backtest.klineCount')}</Th>
                            <Th>{t('backtest.sizeMb')}</Th>
                            <Th>{t('backtest.createdAt')}</Th>
                            <Th>{t('backtest.actions')}</Th>
                          </Tr>
                        </Thead>
                      <Tbody>
                        {cachedKlines.map((c) => (
                          <Tr key={c.name}>
                            <Td>
                              <Tooltip label={c.name}>
                                <Text fontSize="sm" noOfLines={1} maxW="200px">{c.name}</Text>
                              </Tooltip>
                            </Td>
                            <Td>{c.symbol || '-'}</Td>
                            <Td>{c.interval || '-'}</Td>
                            <Td>{c.candles.toLocaleString()}</Td>
                            <Td>{(c.size_mb ?? 0).toFixed(2)}</Td>
                            <Td>{formatDate(c.created)}</Td>
                            <Td>
                              <Tooltip label={t('backtest.deleteCache')}>
                                <IconButton
                                  aria-label={t('backtest.deleteCacheAria')}
                                  size="xs"
                                  icon={<DeleteIcon />}
                                  colorScheme="red"
                                  variant="ghost"
                                  onClick={() => {
                                    deleteCache(c.name).then((r) => {
                                      if (r.success) {
                                        toast({ title: t('backtest.cacheDeleted'), status: 'success' })
                                        loadCachedKlines()
                                      }
                                    }).catch(() => toast({ title: t('backtest.deleteFailed'), status: 'error' }))
                                  }}
                                />
                              </Tooltip>
                            </Td>
                          </Tr>
                        ))}
                      </Tbody>
                    </Table>
                  </Box>
                )}
              </CardBody>
            </Card>
          </TabPanel>
        </TabPanels>
      </Tabs>
    </Box>
  )
}
