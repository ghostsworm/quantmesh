import React, { useEffect, useState, useCallback, useRef } from 'react'
import html2canvas from 'html2canvas'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
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
  const [params, setParams] = useState<Record<string, unknown>>({})
  const [totalCapital, setTotalCapital] = useState(10000)
  
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
      toast({ title: '请选择交易对、周期、日期范围和策略', status: 'warning' })
      return
    }
    if (totalCapital <= 0) {
      toast({ title: '总资金需大于 0', status: 'warning' })
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
        toast({ title: '优化任务已创建', status: 'success' })
        loadOptimTasks()
        setSelectedOptimTaskId(r.task_id)
      }
    } catch (e) {
      toast({ title: (e as Error)?.message || '创建失败', status: 'error' })
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
        toast({ title: '载入结果失败', status: 'error' })
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
      toast({ title: '载入缓存列表失败', status: 'error' })
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
        toast({ title: '载入 K 线文件列表失败', description: 'K 线收集器可能未初始化', status: 'warning' })
      }
    } finally {
      setKlineFilesLoading(false)
    }
  }, [toast])

  // 初始化：载入策略列表、交易所列表、预计算结果和 K 线缓存列表
  useEffect(() => {
    getBacktestStrategies().then((r) => r.success && setStrategies(r.strategies || []))
    getBacktestExchanges().then((r) => {
      if (r.success && r.exchanges) {
        const sorted = [...r.exchanges].sort((a, b) =>
          a.exchange.localeCompare(b.exchange, undefined, { sensitivity: 'base' })
        )
        setExchanges(sorted)
        // 自动选择已配置的交易所，或预设第一个
        const configured = sorted.find(e => e.is_configured)
        if (configured) {
          setSelectedExchange(configured.exchange)
          setAvailableMarketTypes(configured.market_types || ['futures', 'spot'])
        } else if (sorted.length > 0) {
          setSelectedExchange(sorted[0].exchange)
          setAvailableMarketTypes(sorted[0].market_types || ['futures', 'spot'])
        }
      }
    })
    loadPrecomputedResults()
    loadCachedKlines()
    loadKlineFiles()
  }, [loadPrecomputedResults, loadCachedKlines, loadKlineFiles])

  // 获取智能参数推荐
  const handleGetSmartRecommendation = useCallback(async () => {
    if (!symbol || !strategyType) {
      toast({ title: '请先选择交易对和策略', status: 'warning' })
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
          title: '已获取智能推荐',
          description: `置信度: ${r.recommendation.confidence.toFixed(0)}%`,
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
      toast({ title: '获取智能推荐失败', status: 'error', description })
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
      title: '已应用智能推荐参数',
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
      title: '已应用预计算配置',
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
      // 如果当前选择的市场类型不在可用列表中，重置
      if (!ex.market_types?.includes(selectedMarketType)) {
        setSelectedMarketType(ex.market_types?.[0] || 'futures')
      }
    }
    // 清空交易对选择
    setSymbol('')
    setSymbols([])
  }, [selectedExchange])

  // 当交易所或市场类型改变时，载入交易对列表
  useEffect(() => {
    if (!selectedExchange || !selectedMarketType) return
    getBacktestSymbols(selectedExchange, selectedMarketType).then((r) => {
      if (r.success && r.symbols) {
        setSymbols(r.symbols)
        // 自动选择已配置的交易对，或清空
        const configured = r.symbols.find(s => s.is_configured)
        if (configured) {
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
            title: '已从配置中载入参数',
            description: `为 ${symbol} 的 ${strategyType} 策略预填了参数`,
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
      toast({ title: '请先选择交易对、周期和日期范围', status: 'warning' })
      return
    }
    setCacheGenerating(true)
    postCacheGenerate({ symbol, interval, start_date: startDate, end_date: endDate })
      .then((r) => {
        if (r.success) {
          toast({ title: '已在后台生成 K 线缓存', status: 'success' })
          setTimeout(() => getCacheStatus({ symbol, interval, start_date: startDate, end_date: endDate }).then((s) => s.success && setCacheExists(s.exists)), 2000)
        }
      })
      .catch((e) => toast({ title: e.message || '生成失败', status: 'error' }))
      .finally(() => setCacheGenerating(false))
  }

  const handleRunBacktest = () => {
    if (!strategyType) {
      toast({ title: '请选择策略', status: 'warning' })
      return
    }
    if (totalCapital <= 0) {
      toast({ title: '总资金需大于 0', status: 'warning' })
      return
    }

    // 按数据来源校验
    if (dataSource === 'kline_file') {
      if (!selectedKlineFile) {
        toast({ title: '请选择 K 线文件', status: 'warning' })
        return
      }
    } else if (dataSource === 'cache') {
      if (!selectedCacheName) {
        toast({ title: '请选择回测缓存', status: 'warning' })
        return
      }
    } else {
      // 时间范围
      if (!symbol || !interval || !startDate || !endDate) {
        toast({ title: '请选择交易对、周期和日期范围', status: 'warning' })
        return
      }
    }

    setRunning(true)
    
    const payload = dataSource === 'kline_file'
      ? { 
          strategy: strategyType, 
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
            strategy: strategyType, 
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
          strategy: strategyType, 
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
          toast({ title: '回测任务已创建', status: 'success' })
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
            title: '回测服务不可用',
            description: description || '请确保已启用存储（SQLite），并在「设置」中保存过配置后重启服务。',
            status: 'error',
            duration: 8000,
            isClosable: true,
          })
        } else {
          toast({ title: msg || '创建失败', status: 'error' })
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
        throw new Error('Canvas 生成失败，尺寸为 0')
      }
      
      // 下载图片
      const dataUrl = canvas.toDataURL('image/png')
      if (!dataUrl || dataUrl === 'data:,') {
        throw new Error('图片数据生成失败，可能是内容太大')
      }
      
      const link = document.createElement('a')
      link.download = `backtest_${selectedTaskId}.png`
      link.href = dataUrl
      link.click()
      
      toast({
        title: '图片保存成功',
        description: `已保存为 backtest_${selectedTaskId}.png`,
        status: 'success',
        duration: 3000,
        isClosable: true,
      })
    } catch (error) {
      console.error('保存图片失败:', error)
      toast({
        title: '图片保存失败',
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

  // 市场类型显示名称
  const marketTypeLabels: Record<string, string> = {
    futures: '合约',
    spot: '现货',
  }

  return (
    <Box>
      <Heading size="md" mb={4}>回测</Heading>

      {/* 预计算推荐区域 */}
      {precomputedResults.length > 0 && (
        <Card mb={4} borderColor="blue.200" borderWidth={1}>
          <CardHeader pb={2}>
            <Flex justify="space-between" align="center">
              <HStack>
                <StarIcon color="yellow.500" />
                <Text fontWeight="600">智能推荐 - 预计算回测结果</Text>
                <Badge colorScheme="green">{precomputedResults.length} 个就绪</Badge>
              </HStack>
              <IconButton
                aria-label="展开/收起"
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
                系统已根据当前市场情况自动运行回测，您可以直接选用表现良好的配置。
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
                      策略: {result.strategy} | {result.market_type === 'spot' ? '现货' : '合约'}
                    </Text>
                    <HStack spacing={2} fontSize="xs" color="gray.500">
                      <Text>夏普: {result.result?.metrics?.sharpe_ratio?.toFixed(4) ?? '-'}</Text>
                      <Text>回撤: {result.result?.metrics?.max_drawdown?.toFixed(4) ?? '-'}%</Text>
                      <Text>胜率: {result.result?.metrics?.win_rate?.toFixed(4) ?? '-'}%</Text>
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
                      置信度: {result.recommendation?.confidence?.toFixed(0) ?? '-'}%
                    </Text>
                  </Box>
                ))}
              </SimpleGrid>
              {precomputedResults.length > 6 && (
                <Text fontSize="sm" color="gray.500" mt={2} textAlign="center">
                  还有 {precomputedResults.length - 6} 个推荐结果...
                </Text>
              )}
            </CardBody>
          </Collapse>
        </Card>
      )}

      {precomputedLoading && (
        <Alert status="info" mb={4}>
          <Spinner size="sm" mr={3} />
          <AlertDescription>正在载入智能推荐...</AlertDescription>
        </Alert>
      )}

      <Tabs>
        <TabList>
          <Tab>新建回测</Tab>
          <Tab>任务列表</Tab>
          <Tab>参数优化</Tab>
          <Tab>K 线缓存</Tab>
        </TabList>
        <TabPanels>
          <TabPanel>
            <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
              <Card>
                <CardHeader fontWeight="600">1. 交易对与数据</CardHeader>
                <CardBody>
                  {/* 数据来源选择 */}
                  <FormControl mb={4}>
                    <FormLabel>数据来源</FormLabel>
                    <RadioGroup value={dataSource} onChange={(value) => setDataSource(value as 'time_range' | 'kline_file' | 'cache')}>
                      <Stack direction="row" spacing={4}>
                        <Radio value="time_range">时间范围</Radio>
                        <Radio value="kline_file">K线文件</Radio>
                        <Radio value="cache">回测缓存</Radio>
                      </Stack>
                    </RadioGroup>
                  </FormControl>

                  {dataSource === 'time_range' && (
                    <>
                      {/* 交易所选择 */}
                  <FormControl mb={3}>
                    <FormLabel>交易所</FormLabel>
                    <Select
                      placeholder="选择交易所"
                      value={selectedExchange}
                      onChange={(e) => setSelectedExchange(e.target.value)}
                    >
                      {exchanges.map((ex) => (
                        <option key={ex.exchange} value={ex.exchange}>
                          {ex.exchange.toUpperCase()}
                          {ex.is_configured ? ' (已配置)' : ''}
                        </option>
                      ))}
                    </Select>
                  </FormControl>

                  {/* 市场类型选择 */}
                  <FormControl mb={3}>
                    <FormLabel>市场类型</FormLabel>
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
                    <FormLabel>交易对</FormLabel>
                    <Select
                      placeholder="选择交易对"
                      value={symbol}
                      onChange={(e) => setSymbol(e.target.value)}
                      isDisabled={!selectedExchange}
                    >
                      {symbols.map((s) => (
                        <option key={`${s.exchange}-${s.symbol}`} value={s.symbol}>
                          {s.symbol}
                          {s.is_configured ? ' (已配置)' : ''}
                        </option>
                      ))}
                    </Select>
                  </FormControl>

                  {preset && (
                    <Box mb={3} p={2} bg="gray.50" borderRadius="md" fontSize="sm">
                      <Text>推荐: {preset.recommended_interval} K线, {preset.recommended_days?.join('/')} 天, 网格间距 {preset.grid_gap_range}</Text>
                    </Box>
                  )}
                  <FormControl mb={3}>
                    <FormLabel>回测天数</FormLabel>
                    <HStack mb={2} flexWrap="wrap" gap={2}>
                      {[3, 7, 14, 30, 90, 180, 365].map((d) => (
                        <Button
                          key={d}
                          size="sm"
                          variant={days === d ? 'solid' : 'outline'}
                          colorScheme={days === d ? 'blue' : 'gray'}
                          onClick={() => setDays(d)}
                        >
                          {d} 天
                        </Button>
                      ))}
                    </HStack>
                    <Box px={1}>
                      <Slider
                        aria-label="回测天数"
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
                        <Text fontWeight="600" color="blue.600">{days} 天</Text>
                        <Text>365</Text>
                      </HStack>
                    </Box>
                  </FormControl>
                  <FormControl mb={3}>
                    <FormLabel>K 线周期</FormLabel>
                    <Select value={interval} onChange={(e) => setInterval(e.target.value)}>
                      {['1m', '3m', '5m', '15m', '30m', '1h', '4h', '1d'].map((i) => (
                        <option key={i} value={i}>{i}</option>
                      ))}
                    </Select>
                  </FormControl>
                  <HStack mb={3}>
                    <FormControl flex={1}>
                      <FormLabel>开始日期</FormLabel>
                      <Input type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
                    </FormControl>
                    <FormControl flex={1}>
                      <FormLabel>结束日期</FormLabel>
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
                      {cacheExists ? '已缓存' : '生成 K 线缓存'}
                    </Button>
                    {cacheExists && <Badge colorScheme="green"><CheckIcon mr={1} /> 缓存就绪</Badge>}
                  </HStack>
                  <Text fontSize="xs" color="gray.500" mt={2}>
                    可直接运行回测，无缓存时将自动从交易所拉取并缓存数据；预先生成缓存可缩短首次回测等待时间。
                  </Text>
                    </>
                  )}

                  {dataSource === 'kline_file' && (
                    <>
                      <FormControl mb={3}>
                        <FormLabel>K线文件</FormLabel>
                        <Button
                          size="sm"
                          leftIcon={<RepeatIcon />}
                          onClick={() => loadKlineFiles(true)}
                          isLoading={klineFilesLoading}
                          mb={2}
                        >
                          刷新文件列表
                        </Button>
                        {klineFilesLoading ? (
                          <Flex justify="center" py={4}><Spinner /></Flex>
                        ) : availableKlineFiles.length === 0 ? (
                          <Text color="gray.500" fontSize="sm">暂无可用的 K 线文件（已完成采集且状态正常）</Text>
                        ) : (
                          <Box maxH="350px" overflowY="auto" border="1px" borderColor="gray.200" borderRadius="md">
                            <Table size="sm">
                              <Thead>
                                <Tr>
                                  <Th>选择</Th>
                                  <Th>交易对</Th>
                                  <Th>周期</Th>
                                  <Th>时间范围</Th>
                                  <Th>深度</Th>
                                  <Th>K线数</Th>
                                  <Th>来源</Th>
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
                                        <Badge colorScheme="green">有</Badge>
                                      ) : (
                                        <Badge variant="outline">无</Badge>
                                      )}
                                    </Td>
                                    <Td>{file.candle_count.toLocaleString()}</Td>
                                    <Td>
                                      <Badge variant="outline" fontSize="xs">
                                        {file.source === 'collector' ? '采集' : 
                                         file.source === 'backtest_cache' ? '缓存' : '手动'}
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
                        <FormLabel>回测缓存</FormLabel>
                        <Button
                          size="sm"
                          leftIcon={<RepeatIcon />}
                          onClick={loadCachedKlines}
                          isLoading={cachedKlinesLoading}
                          mb={2}
                        >
                          刷新缓存列表
                        </Button>
                        {cachedKlinesLoading ? (
                          <Flex justify="center" py={4}><Spinner /></Flex>
                        ) : cachedKlines.length === 0 ? (
                          <Text color="gray.500" fontSize="sm">暂无回测缓存</Text>
                        ) : (
                          <Box maxH="300px" overflowY="auto" border="1px" borderColor="gray.200" borderRadius="md">
                            <Table size="sm">
                              <Thead>
                                <Tr>
                                  <Th>选择</Th>
                                  <Th>缓存名称</Th>
                                  <Th>交易对</Th>
                                  <Th>周期</Th>
                                  <Th>K线数</Th>
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
                <CardHeader fontWeight="600">2. 策略与参数</CardHeader>
                <CardBody>
                  <FormControl mb={4}>
                    <FormLabel>总投入资金 (USDT) *</FormLabel>
                    <NumberInput value={totalCapital} min={1} onChange={(_: string, v: number) => setTotalCapital(v)}>
                      <NumberInputField />
                    </NumberInput>
                    <Text fontSize="xs" color="gray.500" mt={1}>默认 10000，选策略后不会被覆盖</Text>
                  </FormControl>
                  <FormControl mb={3}>
                    <FormLabel>策略</FormLabel>
                    <Select
                      placeholder="选择策略"
                      value={strategyType}
                      onChange={(e) => {
                        setStrategyType(e.target.value)
                        setParams({}) // 清空参数，loadConfigParams 会自动触发
                        setSmartRecommendation(null) // 清空智能推荐
                      }}
                    >
                      {strategies.map((s) => (
                        <option key={s.strategy_type} value={s.strategy_type}>{s.name}</option>
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
                        获取智能推荐
                      </Button>
                      {smartRecommendation && (
                        <Button
                          size="sm"
                          colorScheme="green"
                          onClick={() => applySmartRecommendation(smartRecommendation)}
                        >
                          应用推荐
                        </Button>
                      )}
                    </Box>
                  )}

                  {/* 智能推荐结果展示 */}
                  {smartRecommendation && (
                    <Alert status="info" mb={3} borderRadius="md">
                      <Box flex="1">
                        <AlertTitle fontSize="sm">
                          智能推荐 (置信度: {smartRecommendation.confidence.toFixed(0)}%)
                        </AlertTitle>
                        <AlertDescription fontSize="xs">
                          <Text mb={1}>当前价格: ${smartRecommendation.current_price.toFixed(2)}</Text>
                          {smartRecommendation.volatility && (
                            <Text mb={1}>
                              7日波动率: {smartRecommendation.volatility.volatility_7d?.toFixed(1)}% |
                              趋势: {smartRecommendation.volatility.trend_direction === 'up' ? '上涨' : smartRecommendation.volatility.trend_direction === 'down' ? '下跌' : '震荡'}
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
                      <Text fontSize="sm" color="gray.500">正在载入配置参数...</Text>
                    </HStack>
                  )}
                  {currentStrategyDef?.params
                    ?.filter((p) => p.name !== 'total_capital')
                    ?.map((p) => (
                    <FormControl key={p.name} mb={2}>
                      <FormLabel fontSize="sm">{p.label}{p.required ? ' *' : ''}</FormLabel>
                      {p.type === 'number' && (
                        <NumberInput
                          value={(params[p.name] as number) ?? (p.default as number)}
                          min={p.min}
                          max={p.max}
                          step={p.step ?? 0.1}
                          precision={p.step ? Math.max(0, -Math.floor(Math.log10(p.step))) : 1}
                          onChange={(_: string, v: number) => setParams((prev) => ({ ...prev, [p.name]: v }))}
                        >
                          <NumberInputField />
                        </NumberInput>
                      )}
                      {p.hint && (
                        <Text fontSize="xs" color="gray.500" mt={1}>
                          {p.hint}
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
                    开始回测
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
                      <Th>状态</Th>
                      <Th>策略</Th>
                      <Th>交易对</Th>
                      <Th>周期</Th>
                      <Th>时间范围</Th>
                      <Th>创建时间</Th>
                      <Th>操作</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {tasks.map((t) => (
                      <Tr
                        key={t.id}
                        bg={t.id === selectedTaskId ? 'blue.50' : undefined}
                        _dark={{ bg: t.id === selectedTaskId ? 'blue.900' : undefined }}
                      >
                        <Td>
                          <Tooltip
                            label={t.status === 'failed' && t.error ? t.error : undefined}
                            isDisabled={t.status !== 'failed' || !t.error}
                            placement="top"
                            maxW="320px"
                          >
                            <Badge
                              colorScheme={
                                t.status === 'completed' ? 'green' : t.status === 'failed' ? 'red' : t.status === 'running' ? 'blue' : 'gray'
                              }
                            >
                              {t.status}
                            </Badge>
                          </Tooltip>
                        </Td>
                        <Td>{t.strategy}</Td>
                        <Td>{t.symbol}</Td>
                        <Td>{t.interval}</Td>
                        <Td>{formatDate(t.start_time)} ~ {formatDate(t.end_time)}</Td>
                        <Td>{formatDate(t.created_at)}</Td>
                        <Td>
                          <HStack>
                            <Button
                              size="xs"
                              onClick={() => {
                                setSelectedTaskId(t.id)
                                reportModal.onOpen()
                              }}
                            >
                              查看
                            </Button>
                            {t.status === 'completed' && (
                              <Tooltip label="下载报告">
                                <IconButton
                                  aria-label="下载报告"
                                  size="xs"
                                  icon={<DownloadIcon />}
                                  onClick={() => {
                                    getBacktestTaskReport(t.id, true).then((md) => {
                                      const blob = new Blob([md], { type: 'text/markdown' })
                                      const a = document.createElement('a')
                                      a.href = URL.createObjectURL(blob)
                                      a.download = `backtest_${t.id}.md`
                                      a.click()
                                    })
                                  }}
                                />
                              </Tooltip>
                            )}
                            <IconButton
                              aria-label="删除"
                              size="xs"
                              icon={<DeleteIcon />}
                              onClick={() => {
                                deleteBacktestTask(t.id).then(() => loadTasks())
                                if (selectedTaskId === t.id) {
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
                <ModalHeader>回测结果: {selectedTaskId ?? '-'}</ModalHeader>
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
                            <AlertTitle>任务执行失败</AlertTitle>
                            <AlertDescription>{sel.error}</AlertDescription>
                          </Box>
                        </Alert>
                      )
                    }
                    return null
                  })()}
                  {resultData && typeof resultData === 'object' && ('result' in resultData || 'comparison' in resultData) && (
                    <Box mb={4}>
                      {(() => {
                        const data = resultData as BacktestResultData
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
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">总收益率</Text><Text fontWeight="600">{String(m.total_return ?? '-')}%</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">最大回撤</Text><Text fontWeight="600">{String(m.max_drawdown ?? '-')}%</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">夏普比率</Text><Text fontWeight="600">{String(m.sharpe_ratio ?? '-')}</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">交易次数</Text><Text fontWeight="600">{String(m.total_trades ?? '-')}</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">买/卖</Text><Text fontWeight="600">{String(m.buy_count ?? '-')} / {String(m.sell_count ?? '-')}</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">期末持仓</Text><Text fontWeight="600">{endPosQty.toFixed(6)}</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">期末持仓市值</Text><Text fontWeight="600">{endPosValue.toFixed(4)} USDT</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">期末 USDT</Text><Text fontWeight="600">{endCashUSDT.toFixed(4)}</Text></Box>
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
                              <Text fontSize="sm" fontWeight="600" mb={3}>风控对比</Text>
                              <Box overflowX="auto" mb={4}>
                                <Table size="sm" variant="simple">
                                  <Thead>
                                    <Tr>
                                      <Th>指标</Th>
                                      <Th isNumeric>无风控</Th>
                                      <Th isNumeric>有风控</Th>
                                    </Tr>
                                  </Thead>
                                  <Tbody>
                                    {compareRow('总收益率', String((noRiskM as Record<string, unknown>)?.total_return ?? '-'), String((withRiskM as Record<string, unknown>)?.total_return ?? '-'), '%')}
                                    {compareRow('最大回撤', String((noRiskM as Record<string, unknown>)?.max_drawdown ?? '-'), String((withRiskM as Record<string, unknown>)?.max_drawdown ?? '-'), '%')}
                                    {compareRow('夏普比率', String((noRiskM as Record<string, unknown>)?.sharpe_ratio ?? '-'), String((withRiskM as Record<string, unknown>)?.sharpe_ratio ?? '-'))}
                                    {compareRow('交易次数', String((noRiskM as Record<string, unknown>)?.total_trades ?? '-'), String((withRiskM as Record<string, unknown>)?.total_trades ?? '-'))}
                                    {compareRow('买/卖', `${(noRiskM as Record<string, unknown>)?.buy_count ?? '-'} / ${(noRiskM as Record<string, unknown>)?.sell_count ?? '-'}`, `${(withRiskM as Record<string, unknown>)?.buy_count ?? '-'} / ${(withRiskM as Record<string, unknown>)?.sell_count ?? '-'}`)}
                                    {compareRow('期末持仓', noRiskPos.endPosQty.toFixed(6), withRiskPos.endPosQty.toFixed(6))}
                                    {compareRow('期末持仓市值', noRiskPos.endPosValue.toFixed(4), withRiskPos.endPosValue.toFixed(4), ' USDT')}
                                    {compareRow('期末 USDT', noRiskPos.endCashUSDT.toFixed(4), withRiskPos.endCashUSDT.toFixed(4))}
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
                                      风控介入记录（共 {cm?.risk_intervention_count ?? 0} 次，跳过 {cm?.skipped_signals ?? 0} 个买入信号）
                                      {hasMore && <Text as="span" color="gray.500" fontWeight="normal">（仅显示前 {maxDisplay} 条）</Text>}
                                    </Text>
                                    <Box overflowX="auto" maxH="200px" overflowY="auto">
                                      <Table size="sm">
                                        <Thead>
                                          <Tr>
                                            <Th>时间</Th>
                                            <Th>原因</Th>
                                            <Th>类型</Th>
                                            <Th>持续K线</Th>
                                            <Th>跳过买入</Th>
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
                      <Text fontSize="sm" fontWeight="600" mb={2}>期间 K 线走势（收盘价，拆为 4 段）</Text>
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
                                <Text fontSize="xs" color="gray.500" mb={1}>第{i + 1}段</Text>
                                <ResponsiveContainer width="100%" height={130}>
                                  <LineChart data={segData} margin={{ top: 5, right: 5, left: -20, bottom: 0 }}>
                                    <CartesianGrid strokeDasharray="2 2" stroke="rgba(0,0,0,0.05)" />
                                    <XAxis dataKey="time" tick={{ fontSize: 9 }} interval="preserveStartEnd" />
                                    <YAxis domain={['auto', 'auto']} tick={{ fontSize: 9 }} width={45} />
                                    <RechartsTooltip formatter={(v: number) => [v.toFixed(2), '收盘']} />
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
                      <Text ml={3} color="gray.500">载入中...</Text>
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
                          loadingText="生成中..."
                        >
                          保存为图片
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
                          下载报告
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
                <CardHeader fontWeight="600">1. 基础配置</CardHeader>
                <CardBody>
                  <Text fontSize="sm" color="gray.600" mb={3}>
                    选择交易对、策略、日期范围和资金，程序将自动遍历参数组合寻找最优解。
                  </Text>
                  <FormControl mb={2}>
                    <FormLabel fontSize="sm">交易对</FormLabel>
                    <Select placeholder="选择交易对" value={symbol} onChange={(e) => setSymbol(e.target.value)} isDisabled={!selectedExchange}>
                      {symbols.map((s) => (
                        <option key={`${s.exchange}-${s.symbol}`} value={s.symbol}>{s.symbol}</option>
                      ))}
                    </Select>
                  </FormControl>
                  <FormControl mb={2}>
                    <FormLabel fontSize="sm">策略</FormLabel>
                    <Select placeholder="选择策略" value={strategyType} onChange={(e) => setStrategyType(e.target.value)}>
                      {strategies.filter((s) => ['grid', 'momentum', 'mean_reversion', 'trend_following', 'dca', 'martingale'].includes(s.strategy_type)).map((s) => (
                        <option key={s.strategy_type} value={s.strategy_type}>{s.name}</option>
                      ))}
                    </Select>
                  </FormControl>
                  <FormControl mb={2}>
                    <FormLabel fontSize="sm">K 线周期</FormLabel>
                    <Select value={interval} onChange={(e) => setInterval(e.target.value)}>
                      {['1m', '3m', '5m', '15m', '30m', '1h', '4h', '1d'].map((i) => (
                        <option key={i} value={i}>{i}</option>
                      ))}
                    </Select>
                  </FormControl>
                  <HStack mb={2}>
                    <FormControl flex={1}>
                      <FormLabel fontSize="sm">开始日期</FormLabel>
                      <Input type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
                    </FormControl>
                    <FormControl flex={1}>
                      <FormLabel fontSize="sm">结束日期</FormLabel>
                      <Input type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} />
                    </FormControl>
                  </HStack>
                  <FormControl mb={3}>
                    <FormLabel fontSize="sm">初始资金 (USDT)</FormLabel>
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
                    开始参数优化
                  </Button>
                  <Text fontSize="xs" color="gray.500" mt={2}>
                    使用策略默认参数范围，遍历组合并回测，完成后可筛选（如最大回撤≤4%）并按收益率排序。
                  </Text>
                </CardBody>
              </Card>
              <Card>
                <CardHeader fontWeight="600">2. 优化任务列表</CardHeader>
                <CardBody>
                  {optimTasksLoading ? (
                    <Flex justify="center" py={6}><Spinner /></Flex>
                  ) : (
                    <Box overflowX="auto">
                      <Table size="sm">
                        <Thead>
                          <Tr>
                            <Th>状态</Th>
                            <Th>策略</Th>
                            <Th>交易对</Th>
                            <Th>组合数</Th>
                            <Th>进度</Th>
                            <Th>操作</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {optimTasks.map((t) => (
                            <Tr key={t.id}>
                              <Td>
                                <Badge
                                  colorScheme={
                                    t.status === 'completed' ? 'green' : t.status === 'failed' ? 'red' : t.status === 'running' ? 'blue' : 'gray'
                                  }
                                >
                                  {t.status}
                                </Badge>
                              </Td>
                              <Td>{t.strategy}</Td>
                              <Td>{t.symbol}</Td>
                              <Td>{t.total_combos}</Td>
                              <Td>{t.progress}%</Td>
                              <Td>
                                <HStack>
                                  <Button
                                    size="xs"
                                    onClick={() => handleViewOptimResult(t.id)}
                                    isDisabled={t.status !== 'completed'}
                                  >
                                    查看
                                  </Button>
                                  <IconButton
                                    aria-label="删除"
                                    size="xs"
                                    icon={<DeleteIcon />}
                                    onClick={() => deleteOptimTask(t.id).then(() => loadOptimTasks())}
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
                    <Text color="gray.500" py={4}>暂无优化任务</Text>
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
              isLoading={optimResultModal.isOpen && selectedOptimTaskId && !optimResultData}
            />
          </TabPanel>
          <TabPanel>
            <Card>
              <CardHeader>
                <Flex justify="space-between" align="center">
                  <Text fontWeight="600">已缓存的 K 线</Text>
                  <Button
                    size="sm"
                    leftIcon={<RepeatIcon />}
                    onClick={loadCachedKlines}
                    isLoading={cachedKlinesLoading}
                  >
                    刷新
                  </Button>
                </Flex>
              </CardHeader>
              <CardBody>
                {cachedKlinesLoading && cachedKlines.length === 0 ? (
                  <Flex justify="center" py={6}><Spinner /></Flex>
                ) : cachedKlines.length === 0 ? (
                  <Text color="gray.500">暂无 K 线缓存。在「新建回测」中选择交易对、周期与日期后点击「生成 K 线缓存」即可生成。</Text>
                ) : (
                  <Box overflowX="auto">
                    <Table size="sm">
                        <Thead>
                          <Tr>
                            <Th>缓存名称</Th>
                            <Th>交易对</Th>
                            <Th>周期</Th>
                            <Th>K 线数</Th>
                            <Th>大小 (MB)</Th>
                            <Th>创建时间</Th>
                            <Th>操作</Th>
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
                              <Tooltip label="删除此缓存">
                                <IconButton
                                  aria-label="删除缓存"
                                  size="xs"
                                  icon={<DeleteIcon />}
                                  colorScheme="red"
                                  variant="ghost"
                                  onClick={() => {
                                    deleteCache(c.name).then((r) => {
                                      if (r.success) {
                                        toast({ title: '缓存已删除', status: 'success' })
                                        loadCachedKlines()
                                      }
                                    }).catch(() => toast({ title: '删除失败', status: 'error' }))
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
