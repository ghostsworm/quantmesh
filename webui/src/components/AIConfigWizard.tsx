import React, { useState, useEffect } from 'react'
import {
  Box,
  Button,
  FormControl,
  FormLabel,
  Input,
  InputGroup,
  InputRightElement,
  IconButton,
  NumberInput,
  NumberInputField,
  Select,
  VStack,
  HStack,
  Text,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Spinner,
  Center,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  useToast,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
  Badge,
  Divider,
  useColorModeValue,
  RadioGroup,
  Radio,
  Stack,
  Wrap,
  WrapItem,
  Progress,
  Heading,
  Switch,
  Tooltip,
} from '@chakra-ui/react'
import { ViewIcon, ViewOffIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { generateAIConfig, applyAIConfig, createAIConfigTask, pollAITaskUntilComplete, AIGenerateConfigRequest, AIGenerateConfigResponse, SymbolCapitalConfig } from '../services/api'
import { getConfig, StrategyInstance, WithdrawalPolicy } from '../services/config'
import { getStrategyTypes } from '../services/strategy'
import { getExchanges } from '../services/api'
import { getCapitalAllocation, type ExchangeCapitalDetail } from '../services/capital'

interface AIConfigWizardProps {
  isOpen: boolean
  onClose: () => void
  onSuccess?: () => void
  // 從父组件傳入的已选交易所和币种
  exchange?: string
  symbols?: string[]
}

type WizardStep = 
  | 'ai-setup' 
  | 'asset-alloc' 
  | 'strategy-split' 
  | 'param-tuning' 
  | 'withdrawal-setup' 
  | 'preview' 
  | 'success';

const AIConfigWizard: React.FC<AIConfigWizardProps> = ({ 
  isOpen, 
  onClose, 
  onSuccess,
  exchange: propsExchange,
  symbols: propsSymbols 
}) => {
  const { t } = useTranslation()
  const toast = useToast()
  const [step, setStep] = useState<WizardStep>('ai-setup')
  const [loading, setLoading] = useState(false)
  
  // Gemini API Key
  const [geminiApiKey, setGeminiApiKey] = useState('')
  const [showApiKey, setShowApiKey] = useState(false)
  const [isKeyFromConfig, setIsKeyFromConfig] = useState(false) // 標記 Key 是否来自配置文件
  
  // 资產分配状態 - 按交易所分组
  // exchange -> symbol -> capital (USDT金額)
  const [exchangeSymbolCapitals, setExchangeSymbolCapitals] = useState<Record<string, Record<string, number>>>({}) // exchange -> symbol -> capital
  const [selectedExchanges, setSelectedExchanges] = useState<string[]>([]) // 已选擇的交易所列表
  const [exchangeBalances, setExchangeBalances] = useState<Record<string, number>>({}) // exchange -> availableBalance
  const [exchangeTotalCapitals, setExchangeTotalCapitals] = useState<Record<string, number>>({}) // exchange -> totalCapital (用戶输入的USDT總額)
  const [exchangeDetails, setExchangeDetails] = useState<ExchangeCapitalDetail[]>([]) // 交易所详情列表
  const [loadingBalances, setLoadingBalances] = useState(false)
  
  // 交易所啟用/禁用状態 - 記錄每個交易所是否啟用（默认啟用）
  const [exchangeEnabled, setExchangeEnabled] = useState<Record<string, boolean>>({})
  
  // 向后兼容：保留舊的资產分配状態（用於單交易所模式）
  const [selectedSymbols, setSelectedSymbols] = useState<string[]>([])
  const [symbolAllocations, setSymbolAllocations] = useState<Record<string, number>>({}) // symbol -> weight (0-1)

  // 策略分配状態 - 使用複合键 "exchangeId:symbol" -> strategies
  const [strategySplits, setStrategySplits] = useState<Record<string, StrategyInstance[]>>({})

  // 网格参數状態 - 使用複合键 "exchangeId:symbol" -> params
  interface GridParams {
    priceInterval: number  // 價格間隔 (USDT)
    orderWindow: number    // 買/賣單視窗
    orderAmount: number    // 每單金額 (USDT)
    maxGridLevels: number  // 最大网格层數
  }
  const [gridParams, setGridParams] = useState<Record<string, GridParams>>({})

  // 可用的策略類型列表
  const [availableStrategyTypes, setAvailableStrategyTypes] = useState<string[]>(['grid', 'dca'])

  // 提現策略状態
  const [withdrawalPolicy, setWithdrawalPolicy] = useState<WithdrawalPolicy>({
    enabled: true,
    threshold: 0.1, // 預設 10%
    mode: 'threshold',
    withdraw_ratio: 1, // 默认划轉全部利润
    principal_protection: {
      enabled: true,
      breakeven_protection: true,
      withdraw_principal: false,
      principal_withdraw_at: 1.0,
      max_loss_ratio: 0.2,
    },
  })
  
  // 资金配置模式: 'total' = 總金額模式, 'per_symbol' = 按币种分配
  const [capitalMode, setCapitalMode] = useState<'total' | 'per_symbol'>('total')
  
  // 總金額模式的资金
  const [totalCapital, setTotalCapital] = useState(10000)
  
  // 按币种分配模式的资金
  const [symbolCapitals, setSymbolCapitals] = useState<SymbolCapitalConfig[]>([])
  
  // 风險偏好
  const [riskProfile, setRiskProfile] = useState<'conservative' | 'balanced' | 'aggressive'>('balanced')
  
  const [aiConfig, setAiConfig] = useState<AIGenerateConfigResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [taskProgress, setTaskProgress] = useState<number>(0)
  const [taskStatus, setTaskStatus] = useState<string>('')

  const bg = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.700')
  const infoBg = useColorModeValue('gray.50', 'gray.700')

  // 使用傳入的交易所，如果没有傳入则從配置獲取或默认
  const [exchange, setExchange] = useState(propsExchange || 'binance')
  const symbols = propsSymbols || [] // 可選的所有币种列表

  // 弹窗打开時，預填配置中的 Gemini Key 和访问模式（若存在）
  useEffect(() => {
    if (!isOpen) return
    const loadConfigData = async () => {
      try {
        const cfg = await getConfig()
        
        // 預填交易所
        if (!propsExchange && cfg?.app?.current_exchange) {
          setExchange(cfg.app.current_exchange)
        }

        // 优先從配置文件读取 Gemini API Key
        const keyFromConfig =
          cfg?.ai?.gemini_api_key ||
          cfg?.ai?.api_key ||
          ''
        if (keyFromConfig) {
          // 如果配置文件中已有值，直接使用（覆盖之前的值）
          setGeminiApiKey(keyFromConfig)
          setIsKeyFromConfig(true) // 標記 Key 来自配置文件
        } else {
          setIsKeyFromConfig(false)
        }
        
        // 加載访问模式配置
        if (cfg?.ai?.access_mode) {
          setAccessMode(cfg.ai.access_mode as 'native' | 'proxy')
        }
        
        // 加載代理配置
        if (cfg?.ai?.proxy) {
          if (cfg.ai.proxy.base_url) {
            setProxyBaseURL(cfg.ai.proxy.base_url)
          }
          if (cfg.ai.proxy.username) {
            setProxyUsername(cfg.ai.proxy.username)
          }
          if (cfg.ai.proxy.password) {
            setProxyPassword(cfg.ai.proxy.password)
          }
        }

        // 初始化已选币种
        if (propsSymbols && propsSymbols.length > 0) {
          setSelectedSymbols(propsSymbols)
          const equalWeight = 1 / propsSymbols.length
          const initialAlloc: Record<string, number> = {}
          propsSymbols.forEach(s => initialAlloc[s] = equalWeight)
          setSymbolAllocations(initialAlloc)
        }

        // 加載可用的策略類型
        try {
          const typesResp = await getStrategyTypes()
          if (typesResp.types && typesResp.types.length > 0) {
            setAvailableStrategyTypes(typesResp.types)
          }
        } catch (err) {
          console.error('加載策略類型失败:', err)
          // 使用默认策略類型
        }

        // 加載交易所列表和餘額
        await loadExchangesAndBalances()
      } catch (err) {
        console.error('加載配置失败:', err)
      }
    }
    loadConfigData()
  }, [isOpen, propsExchange, propsSymbols])

  // 加載交易所列表和餘額
  const loadExchangesAndBalances = async () => {
    setLoadingBalances(true)
    try {
      // 獲取交易所列表
      const exchangesResp = await getExchanges()
      const exchangesList = exchangesResp.exchanges || []
      
      if (exchangesList.length > 0) {
        setSelectedExchanges(exchangesList)
        
        // 獲取每個交易所的餘額
        try {
          const capitalResp = await getCapitalAllocation()
          const details = capitalResp.exchanges || []
          setExchangeDetails(details)
          
          // 構建餘額映射
          const balances: Record<string, number> = {}
          const totalCapitals: Record<string, number> = {}
          details.forEach(detail => {
            // 支持多種计价币：USDT, U, USDC, BUSD
            const quoteAssets = ['USDT', 'U', 'USDC', 'BUSD']
            const matchedAsset = detail.assets.find(a => quoteAssets.includes(a.asset))
            if (matchedAsset) {
              balances[detail.exchangeId] = matchedAsset.availableBalance
              // 默认使用交易所可用餘額作為總资金（取整）
              totalCapitals[detail.exchangeId] = Math.floor(matchedAsset.availableBalance)
            } else {
              balances[detail.exchangeId] = 0
              totalCapitals[detail.exchangeId] = 0
            }
          })
          setExchangeBalances(balances)
          setExchangeTotalCapitals(totalCapitals)
          
          // 初始化每個交易所的币种资金分配
          const initialCapitals: Record<string, Record<string, number>> = {}
          const initialEnabled: Record<string, boolean> = {}
          exchangesList.forEach(ex => {
            initialCapitals[ex] = {}
            initialEnabled[ex] = true // 默认所有交易所都啟用
          })
          setExchangeSymbolCapitals(initialCapitals)
          setExchangeEnabled(initialEnabled)
        } catch (err) {
          console.error('加載交易所餘額失败:', err)
          toast({
            title: t('aiConfig.wizard.loadBalanceFailed'),
            description: t('aiConfig.wizard.loadBalanceFailedDesc'),
            status: 'warning',
            duration: 3000,
          })
        }
      }
    } catch (err) {
      console.error('加載交易所列表失败:', err)
    } finally {
      setLoadingBalances(false)
    }
  }

  // 當币种列表变化時，初始化按币种分配的资金
  useEffect(() => {
    if (selectedSymbols.length > 0) {
      const defaultCapitalPerSymbol = Math.floor(totalCapital / selectedSymbols.length)
      setSymbolCapitals(selectedSymbols.map(symbol => ({
        symbol,
        capital: defaultCapitalPerSymbol
      })))
    }
  }, [selectedSymbols, totalCapital])

  const handleNext = () => {
    if (step === 'ai-setup') {
      if (!geminiApiKey.trim()) {
        setError(t('aiConfig.wizard.enterApiKeyError'))
        return
      }
      setStep('asset-alloc')
    } else if (step === 'asset-alloc') {
      // 驗证每個已啟用交易所的资金分配
      let hasError = false
      let errorMsg = ''
      
      // 只驗证已啟用的交易所
      const enabledExchanges = selectedExchanges.filter(ex => exchangeEnabled[ex] !== false)
      
      for (const exchangeId of enabledExchanges) {
        const exchangeSymbols = exchangeSymbolCapitals[exchangeId] || {}
        const totalAllocated = Object.values(exchangeSymbols).reduce((sum, cap) => sum + cap, 0)
        const totalCapital = exchangeTotalCapitals[exchangeId] || 0
        
        if (totalCapital <= 0) {
          hasError = true
          const exchangeName = exchangeNames[exchangeId] || exchangeId
          errorMsg = t('aiConfig.wizard.capitalNotSet', { exchange: exchangeName })
          break
        }
        
        if (totalAllocated > totalCapital) {
          hasError = true
          const exchangeName = exchangeNames[exchangeId] || exchangeId
          errorMsg = t('aiConfig.wizard.capitalExceeded', { exchange: exchangeName, allocated: Math.round(totalAllocated).toString(), total: Math.round(totalCapital).toString() })
          break
        }
        
        // 检查是否至少有一個币种有资金分配
        const hasAllocation = Object.values(exchangeSymbols).some(cap => cap > 0)
        if (!hasAllocation && Object.keys(exchangeSymbols).length > 0) {
          hasError = true
          const exchangeName = exchangeNames[exchangeId] || exchangeId
          errorMsg = t('aiConfig.wizard.capitalAllZero', { exchange: exchangeName })
          break
        }
      }
      
      if (hasError) {
        setError(errorMsg)
        toast({
          title: t('aiConfig.wizard.validationFailed'),
          description: errorMsg,
          status: 'error',
          duration: 5000,
        })
        return
      }
      
      // 检查是否至少有一個已啟用的交易所配置了币种（如果所有交易所都被禁用，允許跳過）
      if (enabledExchanges.length > 0) {
        const hasAnySymbol = enabledExchanges.some(ex => {
          const symbols = exchangeSymbolCapitals[ex] || {}
          return Object.keys(symbols).length > 0 && Object.values(symbols).some(cap => cap > 0)
        })
        
        if (!hasAnySymbol) {
          const errorMsg = t('aiConfig.wizard.noSymbolAllocation')
          setError(errorMsg)
          toast({
            title: t('aiConfig.wizard.validationFailed'),
            description: errorMsg,
            status: 'error',
            duration: 5000,
          })
          return
        }
      }
      
      // 驗证通過，清除錯误並進入下一步
      setError(null)
      setStep('strategy-split')
    } else if (step === 'strategy-split') {
      // 驗证每個已啟用交易所的每個币种的策略占比總和是否為 100%
      let hasError = false
      let errorMsg = ''

      const enabledExchanges = selectedExchanges.filter(ex => exchangeEnabled[ex] !== false)
      for (const exchangeId of enabledExchanges) {
        const exchangeSymbols = exchangeSymbolCapitals[exchangeId] || {}
        const symbolsWithCapital = Object.keys(exchangeSymbols).filter(s => exchangeSymbols[s] > 0)
        
        for (const symbol of symbolsWithCapital) {
          const taskKey = `${exchangeId}:${symbol}`
          const strategies = strategySplits[taskKey] || []
          // 计算總权重時，需要考虑所有策略類型的权重（包括為 0 的）
          // strategies 數组只包含用戶設置過的策略，权重存儲為 0-1 的小數
          const totalWeight = strategies.reduce((sum, s) => sum + (s.weight || 0), 0)
          
          // 使用更宽松的容差（0.01 = 1%），並四舍五入到整數百分比進行比较
          const totalPercent = Math.round(totalWeight * 100)
          if (totalPercent !== 100) {
            hasError = true
            const exchangeName = exchangeNames[exchangeId] || exchangeId
            errorMsg = t('aiConfig.wizard.strategyWeightError', { exchange: exchangeName, symbol, current: totalPercent.toString() })
            break
          }
        }
        if (hasError) break
      }

      if (hasError) {
        setError(errorMsg)
        toast({
          title: t('aiConfig.wizard.validationFailed'),
          description: errorMsg,
          status: 'error',
          duration: 5000,
        })
        return
      }

      setError(null)
      setStep('param-tuning')
    } else if (step === 'param-tuning') {
      setError(null)
      setStep('withdrawal-setup')
    } else if (step === 'withdrawal-setup') {
      setError(null)
      handleGenerate()
    }
  }

  const handleBack = () => {
    if (step === 'asset-alloc') setStep('ai-setup')
    else if (step === 'strategy-split') setStep('asset-alloc')
    else if (step === 'param-tuning') setStep('strategy-split')
    else if (step === 'withdrawal-setup') setStep('param-tuning')
    else if (step === 'preview') setStep('withdrawal-setup')
    setError(null)
  }

  // 更新單個币种的资金
  const handleSymbolCapitalChange = (symbol: string, capital: number) => {
    setSymbolCapitals(prev => prev.map(sc => 
      sc.symbol === symbol ? { ...sc, capital } : sc
    ))
  }

  // 獲取网格参數的 key
  const getGridParamsKey = (exchangeId: string, symbol: string) => `${exchangeId}:${symbol}`

  // 根據交易對類型确定合理的價格間隔（USDT）
  // BTC 價格高（~100000），间隔大；ETH（~3000）间隔中等；其他小币间隔小
  const getDefaultPriceInterval = (sym: string): number => {
    const upperSymbol = sym.toUpperCase()
    if (upperSymbol.includes('BTC')) {
      return 100 // BTC 间隔 100 USDT
    } else if (upperSymbol.includes('ETH')) {
      return 10 // ETH 间隔 10 USDT
    } else if (upperSymbol.includes('SOL') || upperSymbol.includes('BNB')) {
      return 2 // SOL/BNB 间隔 2 USDT
    } else if (upperSymbol.includes('DOGE') || upperSymbol.includes('XRP') || upperSymbol.includes('ADA')) {
      return 0.01 // 低價币间隔 0.01 USDT
    } else {
      return 1 // 默认间隔 1 USDT
    }
  }

  // 獲取网格参數，如果不存在则返回默认值
  const getGridParams = (exchangeId: string, symbol: string, symbolCapital: number, strategyWeight: number): GridParams => {
    const key = getGridParamsKey(exchangeId, symbol)
    if (gridParams[key]) {
      return gridParams[key]
    }
    // 计算默认值：每單金額 = 资金 / 20 / 窗口大小
    const minOrderAmount = 100 // Binance 最小訂單金額
    const capital = symbolCapital * strategyWeight
    // 根據资金和最小訂單金額计算合理的默认窗口大小
    const maxPossibleWindow = Math.floor(capital / (minOrderAmount * 2.5))
    const defaultOrderWindow = Math.max(1, Math.min(5, maxPossibleWindow))
    const defaultOrderAmount = parseFloat((capital / (defaultOrderWindow * 2.5)).toFixed(0))
    return {
      priceInterval: getDefaultPriceInterval(symbol),
      orderWindow: defaultOrderWindow,
      orderAmount: Math.max(minOrderAmount, defaultOrderAmount), // 最小 100 USDT
      maxGridLevels: 20,
    }
  }

  // 更新网格参數
  const updateGridParams = (exchangeId: string, symbol: string, updates: Partial<GridParams>) => {
    const key = getGridParamsKey(exchangeId, symbol)
    setGridParams(prev => ({
      ...prev,
      [key]: {
        ...prev[key],
        ...updates,
      }
    }))
  }

  // 一键优化：根據资金量和交易對自动计算合理的网格参數
  const handleOptimizeGridParams = (exchangeId: string, symbol: string, symbolCapital: number, strategyWeight: number) => {
    const capital = symbolCapital * strategyWeight
    const minOrderAmount = 100 // Binance 最小訂單金額要求
    
    // 优化逻辑:
    // 1. 首先确保每單金額 >= 100 USDT (Binance 要求)
    // 2. 根據资金大小计算合适的窗口大小
    // 3. 公式: 窗口大小 = 资金 / (每單金額 × 2.5)，其中 2.5 是安全系數(買賣双向+餘量)
    // 4. 價格間隔根據交易對類型設置合理的 USDT 绝對值
    
    let orderWindow: number
    let maxGridLevels: number
    let orderAmount: number
    
    // 计算在保证最小訂單金額的情况下，能支援的最大窗口數
    // 窗口大小 = 资金 / (最小訂單金額 × 2.5)
    const maxPossibleWindow = Math.floor(capital / (minOrderAmount * 2.5))
    
    // 獲取該交易對的默认價格間隔（使用外部定义的函數）
    const priceInterval = getDefaultPriceInterval(symbol)
    
    if (maxPossibleWindow < 1) {
      // 资金不足以支撑最小訂單
      toast({
        title: t('aiConfig.wizard.insufficientCapital'),
        description: t('aiConfig.wizard.insufficientCapitalDesc', { capital: capital.toFixed(0) }),
        status: 'warning',
        duration: 5000,
      })
      orderWindow = 1
      orderAmount = minOrderAmount
      maxGridLevels = 5
    } else if (capital < 1000) {
      // 小资金：尽量少的窗口，保证每單金額
      orderWindow = Math.min(3, maxPossibleWindow)
      orderAmount = parseFloat((capital / (orderWindow * 2.5)).toFixed(0))
      maxGridLevels = 10
    } else if (capital < 3000) {
      // 中小资金
      orderWindow = Math.min(5, maxPossibleWindow)
      orderAmount = parseFloat((capital / (orderWindow * 2.5)).toFixed(0))
      maxGridLevels = 20
    } else if (capital < 8000) {
      // 中等资金
      orderWindow = Math.min(10, maxPossibleWindow)
      orderAmount = parseFloat((capital / (orderWindow * 2.5)).toFixed(0))
      maxGridLevels = 40
    } else {
      // 大资金
      orderWindow = Math.min(15, maxPossibleWindow)
      orderAmount = parseFloat((capital / (orderWindow * 2.5)).toFixed(0))
      maxGridLevels = 100
    }
    
    // 确保每單金額至少 100 USDT
    orderAmount = Math.max(minOrderAmount, orderAmount)
    
    const key = getGridParamsKey(exchangeId, symbol)
    setGridParams(prev => ({
      ...prev,
      [key]: {
        priceInterval,
        orderWindow,
        orderAmount,
        maxGridLevels,
      }
    }))
    
    toast({
      title: t('aiConfig.wizard.optimizeComplete'),
      description: t('aiConfig.wizard.optimizeCompleteDesc', { capital: capital.toFixed(0), symbol, window: orderWindow.toString(), amount: orderAmount.toString() }),
      status: 'success',
      duration: 3000,
    })
  }

  // 计算按币种分配的總资金
  const totalSymbolCapitals = symbolCapitals.reduce((sum, sc) => sum + sc.capital, 0)

  const handleGenerate = async () => {
    // 驗证 Gemini API Key
    if (!geminiApiKey.trim()) {
      setError(t('aiConfig.wizard.enterApiKeyError'))
      return
    }

    // 收集所有已啟用交易所的币种信息
    const enabledExchanges = selectedExchanges.filter(ex => exchangeEnabled[ex] !== false)
    const allSymbols = new Set<string>()
    const exchangeSymbolMap: Record<string, string[]> = {} // exchange -> symbols
    
    for (const exchangeId of enabledExchanges) {
      const symbols = Object.keys(exchangeSymbolCapitals[exchangeId] || {}).filter(
        symbol => (exchangeSymbolCapitals[exchangeId][symbol] || 0) > 0
      )
      if (symbols.length > 0) {
        exchangeSymbolMap[exchangeId] = symbols
        symbols.forEach(s => allSymbols.add(s))
      }
    }

    if (allSymbols.size === 0) {
      setError(t('aiConfig.wizard.noSymbolAllocation'))
      return
    }

    setLoading(true)
    setError(null)

    try {
      // 使用第一個有币种的已啟用交易所作為主交易所（用於向后兼容）
      const primaryExchange = enabledExchanges.find(ex => exchangeSymbolMap[ex]?.length > 0) || exchange
      const primarySymbols = Array.from(allSymbols)
      
      // 计算總资金（所有已啟用交易所的币种资金總和）
      let totalCapitalValue = 0
      const symbolCapitalsList: SymbolCapitalConfig[] = []
      
      for (const exchangeId of enabledExchanges) {
        const symbols = exchangeSymbolCapitals[exchangeId] || {}
        for (const [symbol, capital] of Object.entries(symbols)) {
          if (capital > 0) {
            totalCapitalValue += capital
            // 如果同一個币种在多個交易所都有分配，累加金額
            const existing = symbolCapitalsList.find(sc => sc.symbol === symbol)
            if (existing) {
              existing.capital += capital
            } else {
              symbolCapitalsList.push({ symbol, capital })
            }
          }
        }
      }
      
      // 计算币种比例分配（基於總资金）
      const symbolAllocationsMap: Record<string, number> = {}
      symbolCapitalsList.forEach(sc => {
        symbolAllocationsMap[sc.symbol] = sc.capital / totalCapitalValue
      })

      // 傳遞 API Key 给后端
      const formData: AIGenerateConfigRequest = {
        exchange: primaryExchange,
        symbols: primarySymbols,
        capital_mode: 'per_symbol', // 使用按币种分配模式
        risk_profile: riskProfile,
        gemini_api_key: geminiApiKey,  // 傳遞 API Key
        
        // 资產优先重構新增字段
        symbol_allocations: symbolAllocationsMap,
        strategy_splits: strategySplits, // 現在包含複合键 exchangeId:symbol
        withdrawal_policy: withdrawalPolicy,
        symbol_capitals: symbolCapitalsList, // 按币种分配的资金
      }

      // 創建异步任務
      const taskResponse = await createAIConfigTask(formData)
      setTaskProgress(0)
      setTaskStatus('pending')
      
      // 輪詢任務状態
      const config = await pollAITaskUntilComplete(
        taskResponse.task_id,
        (progress, status) => {
          setTaskProgress(progress)
          setTaskStatus(status)
        }
      )
      
      setAiConfig(config)
      setTaskProgress(100)
      setTaskStatus('completed')
      setStep('preview')
      toast({
        title: t('aiConfig.wizard.generateSuccess'),
        status: 'success',
        duration: 3000,
      })
    } catch (err: any) {
      const errorMsg = err.message || t('aiConfig.wizard.generateFailedDefault')
      setError(errorMsg)
      toast({
        title: t('aiConfig.wizard.generateFailed'),
        description: errorMsg,
        status: 'error',
        duration: 5000,
      })
    } finally {
      setLoading(false)
    }
  }

  const handleApply = async () => {
    if (!aiConfig) return

    setLoading(true)
    setError(null)

    try {
      await applyAIConfig(aiConfig)
      setStep('success')
      toast({
        title: t('aiConfig.wizard.applySuccess'),
        description: t('aiConfig.wizard.applySuccessDesc'),
        status: 'success',
        duration: 5000,
      })
      if (onSuccess) {
        onSuccess()
      }
    } catch (err: any) {
      const errorMsg = err.message || t('aiConfig.wizard.applyFailedDefault')
      setError(errorMsg)
      toast({
        title: t('aiConfig.wizard.applyFailed'),
        description: errorMsg,
        status: 'error',
        duration: 5000,
      })
    } finally {
      setLoading(false)
    }
  }

  // 直接保存配置（不依赖AI生成）
  const handleSaveDirectly = async () => {
    // 收集所有已啟用交易所的币种信息
    const enabledExchanges = selectedExchanges.filter(ex => exchangeEnabled[ex] !== false)
    const allSymbols = new Set<string>()
    const exchangeSymbolMap: Record<string, string[]> = {}
    
    for (const exchangeId of enabledExchanges) {
      const symbols = Object.keys(exchangeSymbolCapitals[exchangeId] || {}).filter(
        symbol => (exchangeSymbolCapitals[exchangeId][symbol] || 0) > 0
      )
      if (symbols.length > 0) {
        exchangeSymbolMap[exchangeId] = symbols
        symbols.forEach(s => allSymbols.add(s))
      }
    }

    if (allSymbols.size === 0) {
      setError(t('aiConfig.wizard.noSymbolAllocation'))
      toast({
        title: t('aiConfig.wizard.validationFailed'),
        description: t('aiConfig.wizard.noSymbolAllocation'),
        status: 'error',
        duration: 5000,
      })
      return
    }

    setLoading(true)
    setError(null)

    try {
      // 構建 symbols_config（分级资產配置）
      const symbolsConfig: any[] = []
      const gridConfigs: any[] = []

      for (const exchangeId of enabledExchanges) {
        const exchangeSymbols = exchangeSymbolCapitals[exchangeId] || {}
        for (const [symbol, capital] of Object.entries(exchangeSymbols)) {
          if (capital > 0) {
            const taskKey = `${exchangeId}:${symbol}`
            const strategies = strategySplits[taskKey] || []
            const gridStrategy = strategies.find(s => s.type === 'grid')
            
            // 獲取网格参數
            const gridParams = gridStrategy 
              ? getGridParams(exchangeId, symbol, capital, gridStrategy.weight)
              : {
                  priceInterval: getDefaultPriceInterval(symbol),
                  orderWindow: 5,
                  orderAmount: 100,
                  maxGridLevels: 20,
                }

            // 構建币种配置
            symbolsConfig.push({
              exchange: exchangeId,
              symbol,
              total_allocated_capital: capital,
              strategies,
              withdrawal_policy: withdrawalPolicy,
              price_interval: gridParams.priceInterval,
              order_quantity: gridParams.orderAmount,
              buy_window_size: gridParams.orderWindow,
              sell_window_size: gridParams.orderWindow,
              max_grid_levels: gridParams.maxGridLevels,
            })

            // 構建网格配置（向后兼容）
            if (gridStrategy && gridStrategy.weight > 0) {
              gridConfigs.push({
                exchange: exchangeId,
                symbol,
                price_interval: gridParams.priceInterval,
                order_quantity: gridParams.orderAmount,
                buy_window_size: gridParams.orderWindow,
                sell_window_size: gridParams.orderWindow,
              })
            }
          }
        }
      }

      // 構建分配配置（向后兼容）
      const allocationConfigs: any[] = []
      let totalCapitalValue = 0
      for (const exchangeId of enabledExchanges) {
        const exchangeSymbols = exchangeSymbolCapitals[exchangeId] || {}
        for (const [symbol, capital] of Object.entries(exchangeSymbols)) {
          if (capital > 0) {
            totalCapitalValue += capital
          }
        }
      }

      for (const exchangeId of enabledExchanges) {
        const exchangeSymbols = exchangeSymbolCapitals[exchangeId] || {}
        for (const [symbol, capital] of Object.entries(exchangeSymbols)) {
          if (capital > 0) {
            allocationConfigs.push({
              exchange: exchangeId,
              symbol,
              max_amount_usdt: capital,
              max_percentage: totalCapitalValue > 0 ? (capital / totalCapitalValue) * 100 : 0,
            })
          }
        }
      }

      // 構建配置對象
      const config: AIGenerateConfigResponse = {
        explanation: t('aiConfig.wizard.manualConfigExplanation'),
        grid_config: gridConfigs,
        allocation: allocationConfigs,
        symbols_config: symbolsConfig,
      }

      // 直接保存配置
      await applyAIConfig(config)
      setStep('success')
      toast({
        title: t('aiConfig.wizard.saveSuccess'),
        description: t('aiConfig.wizard.saveSuccessDesc'),
        status: 'success',
        duration: 5000,
      })
      if (onSuccess) {
        onSuccess()
      }
    } catch (err: any) {
      const errorMsg = err.message || t('aiConfig.wizard.saveFailedDefault')
      setError(errorMsg)
      toast({
        title: t('aiConfig.wizard.saveFailed'),
        description: errorMsg,
        status: 'error',
        duration: 5000,
      })
    } finally {
      setLoading(false)
    }
  }

  const handleReset = () => {
    setStep('form')
    setAiConfig(null)
    setError(null)
  }

  const handleClose = () => {
    handleReset()
    onClose()
  }

  // AI 推荐：资產比例分配
  const [recommendingAllocations, setRecommendingAllocations] = useState(false)
  const handleAIRecommendAllocations = async () => {
    if (!geminiApiKey.trim()) {
      toast({ title: t('aiConfig.wizard.setApiKeyFirst'), status: 'warning', duration: 3000 })
      return
    }
    if (selectedSymbols.length === 0) {
      toast({ title: t('aiConfig.wizard.selectSymbolsFirst'), status: 'warning', duration: 3000 })
      return
    }

    setRecommendingAllocations(true)
    toast({ title: t('aiConfig.wizard.aiAnalyzing'), status: 'info', duration: 2000 })

    try {
      const request: AIGenerateConfigRequest = {
        exchange,
        symbols: selectedSymbols,
        capital_mode: 'total',
        total_capital: totalCapital,
        risk_profile: riskProfile,
        gemini_api_key: geminiApiKey,
      }

      const result = await generateAIConfig(request)
      
      // 從 AI 結果中提取资產分配比例
      if (result.allocation && result.allocation.length > 0) {
        const totalAlloc = result.allocation.reduce((sum, a) => sum + (a.max_percentage || 0), 0)
        const newAllocations: Record<string, number> = {}
        result.allocation.forEach(a => {
          if (selectedSymbols.includes(a.symbol)) {
            // 將百分比轉换為 0-1 的权重
            newAllocations[a.symbol] = totalAlloc > 0 ? (a.max_percentage || 0) / totalAlloc : 1 / selectedSymbols.length
          }
        })
        // 补充未分配的币种
        selectedSymbols.forEach(s => {
          if (!(s in newAllocations)) {
            newAllocations[s] = 1 / selectedSymbols.length
          }
        })
        setSymbolAllocations(newAllocations)
        toast({ title: t('aiConfig.wizard.aiRecommendApplied'), status: 'success', duration: 3000 })
      } else {
        // 如果没有返回分配結果，使用均等分配
        const equalWeight = 1 / selectedSymbols.length
        const newAllocations: Record<string, number> = {}
        selectedSymbols.forEach(s => newAllocations[s] = equalWeight)
        setSymbolAllocations(newAllocations)
        toast({ title: t('aiConfig.wizard.aiEqualAllocation'), status: 'info', duration: 3000 })
      }
    } catch (err: any) {
      console.error('AI 推荐失败:', err)
      toast({ 
        title: t('aiConfig.wizard.aiRecommendFailed'), 
        description: err.message || t('aiConfig.wizard.aiRecommendFailedDesc'), 
        status: 'error', 
        duration: 5000 
      })
    } finally {
      setRecommendingAllocations(false)
    }
  }

  // 默认推荐：單個币种的策略比例（不再使用 AI）
  const [recommendingStrategy, setRecommendingStrategy] = useState<string | null>(null)
  const handleAIRecommendStrategy = async (exchangeId: string, symbol: string) => {
    const taskKey = `${exchangeId}:${symbol}`
    setRecommendingStrategy(taskKey)

    try {
      // 獲取該幣種的资金量
      const symbolCapital = exchangeSymbolCapitals[exchangeId]?.[symbol] || 0
      
      // 根據风險偏好和资金量為所有可用策略分配权重
      const strategyWeights = getRecommendedStrategyWeights(riskProfile, availableStrategyTypes, symbolCapital)

      // 归一化权重，确保總和為 1
      const totalWeight = Object.values(strategyWeights).reduce((a, b) => a + b, 0)
      const normalizedWeights: Record<string, number> = {}
      for (const [type, weight] of Object.entries(strategyWeights)) {
        normalizedWeights[type] = totalWeight > 0 ? weight / totalWeight : 0
      }

      // 過滤掉权重為0的策略，並确保每個策略分配的资金至少满足最小訂單要求
      const minOrderAmount = 100 // 币安最小訂單金額
      const filteredStrategies = availableStrategyTypes
        .filter(type => {
          const weight = normalizedWeights[type] || 0
          if (weight <= 0) return false
          
          // 检查分配的资金是否满足最小訂單要求
          const allocatedCapital = symbolCapital * weight
          return allocatedCapital >= minOrderAmount
        })
        .map(type => ({
          type,
          weight: normalizedWeights[type],
          config: {},
        }))

      // 如果過滤后没有策略，使用最保守的配置（只使用网格和DCA）
      if (filteredStrategies.length === 0) {
        const conservativeStrategies: StrategyInstance[] = []
        if (availableStrategyTypes.includes('grid') && symbolCapital >= minOrderAmount) {
          conservativeStrategies.push({
            type: 'grid',
            weight: 0.6,
            config: {},
          })
        }
        if (availableStrategyTypes.includes('dca') && symbolCapital >= minOrderAmount) {
          conservativeStrategies.push({
            type: 'dca',
            weight: conservativeStrategies.length > 0 ? 0.4 : 1.0,
            config: {},
          })
        }
        
        if (conservativeStrategies.length > 0) {
          setStrategySplits(prev => ({
            ...prev,
            [taskKey]: conservativeStrategies
          }))
          
          toast({ 
            title: t('aiConfig.wizard.strategyApplied', { exchange: exchangeId, symbol }), 
            description: t('aiConfig.wizard.smallCapitalConservative', { config: conservativeStrategies.map(s => `${getStrategyDisplayName(s.type)} ${(s.weight * 100).toFixed(0)}%`).join(', ') }),
            status: 'info', 
            duration: 4000 
          })
          return
        }
      }

      // 重新归一化過滤后的策略权重
      const filteredTotalWeight = filteredStrategies.reduce((sum, s) => sum + s.weight, 0)
      const finalStrategies = filteredStrategies.map(s => ({
        ...s,
        weight: filteredTotalWeight > 0 ? s.weight / filteredTotalWeight : 0
      }))

      setStrategySplits(prev => ({
        ...prev,
        [taskKey]: finalStrategies
      }))

      // 構建描述信息
      const description = finalStrategies
        .filter(s => s.weight > 0.01)
        .map(s => {
          const allocatedCapital = symbolCapital * s.weight
          return `${getStrategyDisplayName(s.type)} ${(s.weight * 100).toFixed(0)}% (${allocatedCapital.toFixed(0)} USDT)`
        })
        .join(', ')

      const warningMsg = symbolCapital < 10000 
        ? t('aiConfig.wizard.smallCapitalWarning') 
        : ''

      toast({ 
        title: t('aiConfig.wizard.strategyApplied', { exchange: exchangeId, symbol }), 
        description: description + (warningMsg ? `\n${warningMsg}` : ''),
        status: 'success', 
        duration: 4000 
      })
    } catch (err: any) {
      console.error('推荐策略失败:', err)
      toast({ 
        title: t('aiConfig.wizard.recommendFailed'), 
        description: err.message || t('aiConfig.wizard.recommendFailedDefault'), 
        status: 'error', 
        duration: 5000 
      })
    } finally {
      setRecommendingStrategy(null)
    }
  }

  // 交易所显示名称映射
  const exchangeNames: Record<string, string> = {
    binance: 'Binance',
    bitget: 'Bitget',
    bybit: 'Bybit',
    gate: 'Gate.io',
    okx: 'OKX',
    huobi: 'Huobi (HTX)',
    kucoin: 'KuCoin',
  }

  // 策略類型显示名称映射
  const getStrategyDisplayName = (type: string): string => {
    const names: Record<string, string> = {
      grid: t('aiConfig.wizard.strategyNames.grid'),
      dca: t('aiConfig.wizard.strategyNames.dca'),
      martingale: t('aiConfig.wizard.strategyNames.martingale'),
      trend: t('aiConfig.wizard.strategyNames.trend'),
      mean_reversion: t('aiConfig.wizard.strategyNames.mean_reversion'),
      breakout: t('aiConfig.wizard.strategyNames.breakout'),
      combo: t('aiConfig.wizard.strategyNames.combo'),
      momentum: t('aiConfig.wizard.strategyNames.momentum'),
    }
    return names[type] || type
  }

  // 策略類型說明映射
  const getStrategyDescription = (type: string): string => {
    const descriptions: Record<string, string> = {
      grid: t('aiConfig.wizard.strategyDescs.grid'),
      dca: t('aiConfig.wizard.strategyDescs.dca'),
      martingale: t('aiConfig.wizard.strategyDescs.martingale'),
      trend: t('aiConfig.wizard.strategyDescs.trend'),
      mean_reversion: t('aiConfig.wizard.strategyDescs.mean_reversion'),
      breakout: t('aiConfig.wizard.strategyDescs.breakout'),
      combo: t('aiConfig.wizard.strategyDescs.combo'),
      momentum: t('aiConfig.wizard.strategyDescs.momentum'),
    }
    return descriptions[type] || t('aiConfig.wizard.strategyDescs.default')
  }

  // 根據风險偏好和资金量獲取推荐的策略权重分配
  const getRecommendedStrategyWeights = (
    profile: 'conservative' | 'balanced' | 'aggressive',
    types: string[],
    capital: number = 0
  ): Record<string, number> => {
    // 策略所需的最小资金量（USDT）
    const strategyMinCapitals: Record<string, number> = {
      grid: 250,           // 網格策略：最小 250 USDT
      dca: 200,            // DCA 定投：最小 200 USDT
      trend: 500,          // 趋势跟踪：最小 500 USDT
      mean_reversion: 500, // 均值回归：最小 500 USDT
      martingale: 2000,   // 马丁格尔：需要大资金，最小 2000 USDT
      breakout: 1000,      // 突破策略：需要较大资金，最小 1000 USDT
      momentum: 1000,      // 动量策略：需要较大资金，最小 1000 USDT
      combo: 1500,         // 组合策略：需要较大资金，最小 1500 USDT
    }

    // 不同风險偏好下各策略的基础权重
    const weightProfiles: Record<string, Record<string, number>> = {
      conservative: {
        grid: 0.40,
        dca: 0.40,
        mean_reversion: 0.15,
        trend: 0.05,
        martingale: 0.00,
        breakout: 0.00,
        momentum: 0.00,
        combo: 0.00,
      },
      balanced: {
        grid: 0.45,
        dca: 0.30,
        trend: 0.15,
        mean_reversion: 0.10,
        martingale: 0.00,
        momentum: 0.00,
        breakout: 0.00,
        combo: 0.00,
      },
      aggressive: {
        grid: 0.35,
        martingale: 0.00, // 小资金時設為0，大资金時再分配
        trend: 0.25,
        momentum: 0.15,
        breakout: 0.15,
        dca: 0.10,
        mean_reversion: 0.00,
        combo: 0.00,
      },
    }

    const profileWeights = weightProfiles[profile] || weightProfiles.balanced
    const result: Record<string, number> = {}

    // 根據资金量調整策略权重
    for (const type of types) {
      const baseWeight = profileWeights[type] || 0
      const minCapital = strategyMinCapitals[type] || 0
      
      // 如果资金量小於策略所需的最小资金量，將該策略权重設為0
      if (capital > 0 && minCapital > 0 && capital < minCapital) {
        result[type] = 0
        continue
      }

      // 小资金時（< 10000 USDT），更保守的配置
      if (capital > 0 && capital < 10000) {
        if (type === 'martingale' || type === 'breakout' || type === 'momentum') {
          // 小资金時排除高风險策略
          result[type] = 0
        } else if (type === 'grid' || type === 'dca') {
          // 小资金時优先使用稳健策略
          result[type] = baseWeight * 1.2 // 增加网格和DCA的权重
        } else {
          result[type] = baseWeight
        }
      } else if (capital >= 10000 && capital < 50000) {
        // 中等资金（10000-50000 USDT）
        if (type === 'martingale') {
          // 中等资金時，马丁格尔权重降低
          result[type] = baseWeight * 0.5
        } else {
          result[type] = baseWeight
        }
      } else {
        // 大资金（>= 50000 USDT），使用原始权重
        // 對於激進模式，如果资金量足够，恢複马丁格尔权重
        if (profile === 'aggressive' && type === 'martingale' && capital >= 20000) {
          result[type] = 0.25 // 大资金時可以使用马丁格尔
        } else {
          result[type] = baseWeight
        }
      }
    }

    return result
  }

  return (
    <Modal isOpen={isOpen} onClose={handleClose} size="xl" scrollBehavior="inside">
      <ModalOverlay />
      <ModalContent bg={bg}>
        <ModalHeader>{t('aiConfig.wizard.modalTitle')}</ModalHeader>
        <ModalCloseButton />
        <ModalBody>
          {step === 'ai-setup' && (
            <VStack spacing={6} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>{t('aiConfig.wizard.step1Title')}</AlertTitle>
                  <AlertDescription fontSize="sm">
                    {t('aiConfig.wizard.step1Desc')}
                  </AlertDescription>
                </Box>
              </Alert>

              <FormControl isRequired>
                <FormLabel>{t('aiConfig.wizard.geminiApiKeyLabel')}</FormLabel>
                <InputGroup size="md">
                  <Input
                    type={showApiKey ? 'text' : 'password'}
                    placeholder={t('aiConfig.wizard.geminiApiKeyPlaceholder')}
                    value={geminiApiKey}
                    onChange={(e) => {
                      setGeminiApiKey(e.target.value)
                      setIsKeyFromConfig(false) // 用戶修改后，標記為不再来自配置文件
                    }}
                    borderRadius="xl"
                  />
                  <InputRightElement width="3rem">
                    <IconButton
                      h="1.75rem"
                      size="sm"
                      onClick={() => setShowApiKey(!showApiKey)}
                      icon={showApiKey ? <ViewOffIcon /> : <ViewIcon />}
                      aria-label={showApiKey ? t('aiConfig.wizard.hideKey') : t('aiConfig.wizard.showKey')}
                      variant="ghost"
                    />
                  </InputRightElement>
                </InputGroup>
                {isKeyFromConfig && geminiApiKey && (
                  <Text fontSize="xs" color="green.500" mt={1}>
                    {t('aiConfig.wizard.keyFromConfig')}
                  </Text>
                )}
                <Text fontSize="xs" color="gray.500" mt={isKeyFromConfig && geminiApiKey ? 0 : 2}>
                  {t('aiConfig.wizard.noKeyYet')} <a href="https://aistudio.google.com/app/apikey" target="_blank" rel="noopener noreferrer" style={{ color: '#3182ce', textDecoration: 'underline' }}>{t('aiConfig.wizard.getKeyLink')}</a>
                </Text>
              </FormControl>
            </VStack>
          )}

          {step === 'asset-alloc' && (
            <VStack spacing={6} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>{t('aiConfig.wizard.step2Title')}</AlertTitle>
                  <AlertDescription fontSize="sm">
                    {t('aiConfig.wizard.step2Desc')}
                  </AlertDescription>
                </Box>
              </Alert>

              {error && (
                <Alert status="error" borderRadius="md">
                  <AlertIcon />
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}

              {loadingBalances ? (
                <Center py={8}>
                  <Spinner size="lg" />
                  <Text ml={4}>{t('aiConfig.wizard.loadingBalances')}</Text>
                </Center>
              ) : selectedExchanges.length === 0 ? (
                <Alert status="warning" borderRadius="md">
                  <AlertIcon />
                  <AlertDescription>
                    {t('aiConfig.wizard.noExchanges')}
                  </AlertDescription>
                </Alert>
              ) : (
                <VStack spacing={4} align="stretch">
                  {selectedExchanges.map(exchangeId => {
                    const exchangeDetail = exchangeDetails.find(d => d.exchangeId === exchangeId)
                    const usdtAsset = exchangeDetail?.assets.find(a => a.asset === 'USDT')
                    const availableBalance = exchangeBalances[exchangeId] || 0
                    const totalCapital = exchangeTotalCapitals[exchangeId] || 0
                    const exchangeSymbols = exchangeSymbolCapitals[exchangeId] || {}
                    const totalAllocated = Object.values(exchangeSymbols).reduce((sum, cap) => sum + cap, 0)
                    const isOverBalance = totalAllocated > totalCapital
                    const exchangeName = exchangeNames[exchangeId] || exchangeId
                    const isTestnet = exchangeDetail?.isTestnet || false
                    const isEnabled = exchangeEnabled[exchangeId] !== false // 默认為 true

                    return (
                      <Box 
                        key={exchangeId} 
                        p={4} 
                        bg={isEnabled ? infoBg : 'gray.100'} 
                        borderRadius="2xl" 
                        border="1px" 
                        borderColor={isEnabled ? borderColor : 'gray.300'}
                        opacity={isEnabled ? 1 : 0.6}
                      >
                        <HStack justify="space-between" mb={4}>
                          <HStack spacing={3}>
                            <Switch
                              isChecked={isEnabled}
                              onChange={(e) => {
                                setExchangeEnabled(prev => ({
                                  ...prev,
                                  [exchangeId]: e.target.checked
                                }))
                              }}
                              colorScheme="blue"
                            />
                            <Text fontWeight="bold" fontSize="md">{exchangeName}</Text>
                            {!isEnabled && (
                              <Badge colorScheme="gray" fontSize="xs">{t('aiConfig.wizard.disabled')}</Badge>
                            )}
                            {isTestnet && (
                              <Badge colorScheme="orange" fontSize="xs">{t('aiConfig.wizard.testnet')}</Badge>
                            )}
                          </HStack>
                          {isEnabled && (
                            <HStack spacing={4}>
                              <VStack spacing={1} align="end">
                                <HStack spacing={2}>
                                  <Text fontSize="xs" color="gray.500">
                                    {t('aiConfig.wizard.availableBalance')} <Text as="span" fontWeight="bold" color="blue.500">{availableBalance.toFixed(2)} USDT</Text>
                                  </Text>
                                  {availableBalance > 0 && Math.round(totalCapital) !== Math.floor(availableBalance) && (
                                    <Button
                                      size="xs"
                                      variant="ghost"
                                      colorScheme="blue"
                                      onClick={() => {
                                        setExchangeTotalCapitals(prev => ({
                                          ...prev,
                                          [exchangeId]: Math.floor(availableBalance)
                                        }))
                                      }}
                                    >
                                      {t('aiConfig.wizard.useAll')}
                                    </Button>
                                  )}
                                </HStack>
                                <HStack spacing={2}>
                                  <Text fontSize="xs" color="gray.500">{t('aiConfig.wizard.usdtTotal')}</Text>
                                  <NumberInput
                                    size="xs"
                                    value={Math.round(totalCapital)}
                                    onChange={(_, val) => {
                                      setExchangeTotalCapitals(prev => ({
                                        ...prev,
                                        [exchangeId]: Math.round(val || 0)
                                      }))
                                    }}
                                    min={0}
                                    precision={0}
                                    w="120px"
                                  >
                                    <NumberInputField />
                                  </NumberInput>
                                  <Text fontSize="xs" color="gray.500">{t('aiConfig.wizard.usdt')}</Text>
                                </HStack>
                              </VStack>
                              <Text fontSize="xs" color={isOverBalance ? "red.500" : "gray.500"}>
                                {t('aiConfig.wizard.allocated')} <Text as="span" fontWeight="bold" color={isOverBalance ? "red.500" : "green.500"}>{Math.round(totalAllocated)} USDT</Text>
                              </Text>
                            </HStack>
                          )}
                        </HStack>

                        {isEnabled && isOverBalance && (
                          <Alert status="error" borderRadius="md" mb={4} size="sm">
                            <AlertIcon />
                            <AlertDescription fontSize="xs">
                              {t('aiConfig.wizard.overBudgetWarning', { allocated: Math.round(totalAllocated).toString(), total: Math.round(totalCapital).toString() })}
                            </AlertDescription>
                          </Alert>
                        )}

                        {isEnabled && (
                          <>
                            <VStack spacing={3} align="stretch">
                              {Object.keys(exchangeSymbols).length === 0 ? (
                                <Center py={4} color="gray.500" fontSize="sm">
                                  {t('aiConfig.wizard.addSymbolHint')}
                                </Center>
                              ) : (
                                Object.entries(exchangeSymbols).map(([symbol, capital]) => (
                                  <HStack key={symbol} spacing={4}>
                                    <Badge colorScheme="green" minW="80px" textAlign="center">{symbol}</Badge>
                                    <Box flex={1}>
                                      <NumberInput
                                        size="sm"
                                        value={Math.round(capital)}
                                        onChange={(_, val) => {
                                          setExchangeSymbolCapitals(prev => ({
                                            ...prev,
                                            [exchangeId]: {
                                              ...prev[exchangeId],
                                              [symbol]: Math.round(val || 0)
                                            }
                                          }))
                                        }}
                                        min={0}
                                        max={totalCapital}
                                        precision={0}
                                      >
                                        <NumberInputField />
                                      </NumberInput>
                                    </Box>
                                    <Text fontSize="xs" color="gray.500" minW="80px">
                                      {t('aiConfig.wizard.usdt')}
                                    </Text>
                                    <IconButton
                                      size="xs"
                                      icon={<Text>×</Text>}
                                      aria-label={t('aiConfig.wizard.remove')}
                                      onClick={() => {
                                        const newSymbols = { ...exchangeSymbols }
                                        delete newSymbols[symbol]
                                        setExchangeSymbolCapitals(prev => ({
                                          ...prev,
                                          [exchangeId]: newSymbols
                                        }))
                                      }}
                                    />
                                  </HStack>
                                ))
                              )}
                            </VStack>

                            <Divider my={3} />

                            <Box>
                              <Text fontSize="sm" fontWeight="bold" mb={2}>{t('aiConfig.wizard.addSymbolLabel')}</Text>
                              <Wrap>
                                {symbols.filter(s => !exchangeSymbols[s]).map(s => (
                                  <WrapItem key={s}>
                                    <Button
                                      size="xs"
                                      variant="outline"
                                      onClick={() => {
                                        setExchangeSymbolCapitals(prev => ({
                                          ...prev,
                                          [exchangeId]: {
                                            ...prev[exchangeId],
                                            [s]: 0
                                          }
                                        }))
                                      }}
                                    >
                                      + {s}
                                    </Button>
                                  </WrapItem>
                                ))}
                              </Wrap>
                            </Box>
                          </>
                        )}
                        
                        {!isEnabled && (
                          <Box py={4} textAlign="center">
                            <Text fontSize="sm" color="gray.500">
                              {t('aiConfig.wizard.exchangeDisabledMsg')}
                            </Text>
                          </Box>
                        )}
                      </Box>
                    )
                  })}
                </VStack>
              )}
            </VStack>
          )}

          {step === 'strategy-split' && (
            <VStack spacing={6} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>{t('aiConfig.wizard.step3Title')}</AlertTitle>
                  <AlertDescription fontSize="sm">
                    {t('aiConfig.wizard.step3Desc')}
                  </AlertDescription>
                </Box>
              </Alert>

              <VStack spacing={6} align="stretch">
                {selectedExchanges
                  .filter(exchangeId => exchangeEnabled[exchangeId] !== false) // 只显示已啟用的交易所
                  .map(exchangeId => {
                    const exchangeSymbols = exchangeSymbolCapitals[exchangeId] || {}
                    const symbolsWithCapital = Object.keys(exchangeSymbols).filter(s => exchangeSymbols[s] > 0)
                    const exchangeName = exchangeNames[exchangeId] || exchangeId

                    if (symbolsWithCapital.length === 0) return null

                    return (
                    <Box key={exchangeId} p={4} bg={bg} borderRadius="2xl" border="1px" borderColor={borderColor}>
                      <Heading size="sm" mb={4} color="blue.500">{exchangeName}</Heading>
                      <VStack spacing={6} align="stretch">
                        {symbolsWithCapital.map(symbol => {
                          const taskKey = `${exchangeId}:${symbol}`
                          return (
                            <Box key={symbol} p={4} bg={infoBg} borderRadius="xl" border="1px" borderColor={borderColor}>
                              <HStack justify="space-between" mb={4}>
                                <Badge colorScheme="green" fontSize="md" px={3} py={1}>{symbol}</Badge>
                                <Button 
                                  size="xs" 
                                  colorScheme="purple" 
                                  variant="ghost" 
                                  onClick={() => handleAIRecommendStrategy(exchangeId, symbol)}
                                >
                                  {t('aiConfig.wizard.recommendConfig')}
                                </Button>
                              </HStack>

                              <VStack spacing={3} align="stretch">
                                {availableStrategyTypes.map(type => {
                                  const strategies = strategySplits[taskKey] || []
                                  const existing = strategies.find(s => s.type === type)
                                  const weight = existing ? existing.weight : 0
                                  
                                  return (
                                    <HStack key={type} spacing={4}>
                                      <Tooltip 
                                        label={getStrategyDescription(type)} 
                                        placement="top"
                                        hasArrow
                                        fontSize="sm"
                                        bg="gray.700"
                                        color="white"
                                        px={3}
                                        py={2}
                                        borderRadius="md"
                                        maxW="300px"
                                      >
                                        <Text 
                                          fontSize="sm" 
                                          minW="90px" 
                                          fontWeight="bold"
                                          cursor="help"
                                          borderBottom="1px dashed"
                                          borderColor="gray.400"
                                          _hover={{ borderColor: "blue.500" }}
                                        >
                                          {getStrategyDisplayName(type)}
                                        </Text>
                                      </Tooltip>
                                      <Box flex={1}>
                                        <HStack>
                                          <NumberInput
                                            size="sm"
                                            maxW="100px"
                                            value={Math.round(weight * 100)}
                                            onChange={(valueString, valueNumber) => {
                                              // valueNumber 可能是 NaN，使用 valueString 解析更可靠
                                              const numVal = parseFloat(valueString) || 0
                                              const others = strategies.filter(s => s.type !== type)
                                              const updated = [...others, { type, weight: numVal / 100, name: `${type}-${symbol}`, config: existing?.config || {} }]
                                              setStrategySplits(prev => ({ ...prev, [taskKey]: updated }))
                                            }}
                                            min={0}
                                            max={100}
                                          >
                                            <NumberInputField />
                                          </NumberInput>
                                          <Text fontSize="xs" color="gray.500">%</Text>
                                        </HStack>
                                      </Box>
                                    </HStack>
                                  )
                                })}
                              </VStack>
                            </Box>
                          )
                        })}
                      </VStack>
                    </Box>
                  )
                })}
              </VStack>
            </VStack>
          )}

          {step === 'param-tuning' && (
            <VStack spacing={6} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>{t('aiConfig.wizard.step4Title')}</AlertTitle>
                  <AlertDescription fontSize="sm">
                    {t('aiConfig.wizard.step4Desc')}
                  </AlertDescription>
                </Box>
              </Alert>

              <VStack spacing={6} align="stretch">
                {selectedExchanges
                  .filter(exchangeId => exchangeEnabled[exchangeId] !== false) // 只显示已啟用的交易所
                  .map(exchangeId => {
                    const exchangeSymbols = exchangeSymbolCapitals[exchangeId] || {}
                    const symbolsWithCapital = Object.keys(exchangeSymbols).filter(s => exchangeSymbols[s] > 0)
                    const exchangeName = exchangeNames[exchangeId] || exchangeId

                    if (symbolsWithCapital.length === 0) return null

                    return (
                    <Box key={exchangeId} p={4} bg={bg} borderRadius="2xl" border="1px" borderColor={borderColor}>
                      <Heading size="sm" mb={4} color="blue.500">{exchangeName}</Heading>
                      <VStack spacing={6} align="stretch">
                        {symbolsWithCapital.map(symbol => {
                          const taskKey = `${exchangeId}:${symbol}`
                          const strategies = strategySplits[taskKey] || []
                          const gridStrategy = strategies.find(s => s.type === 'grid')
                          
                          if (!gridStrategy || gridStrategy.weight === 0) return null

                          const symbolCapital = exchangeSymbols[symbol]
                          const currentParams = getGridParams(exchangeId, symbol, symbolCapital, gridStrategy.weight)

                          return (
                            <Box key={symbol} p={4} bg={infoBg} borderRadius="xl" border="1px" borderColor={borderColor}>
                              <HStack justify="space-between" mb={4}>
                                <Badge colorScheme="green" fontSize="sm">{t('aiConfig.wizard.gridParamsLabel', { symbol })}</Badge>
                                <Button size="xs" colorScheme="blue" variant="outline" onClick={() => {
                                  handleOptimizeGridParams(exchangeId, symbol, symbolCapital, gridStrategy.weight)
                                }}>
                                  {t('aiConfig.wizard.optimizeParams')}
                                </Button>
                              </HStack>

                              <VStack spacing={4} align="stretch">
                                <HStack>
                                  <FormControl flex={1}>
                                    <FormLabel fontSize="xs">{t('aiConfig.wizard.priceInterval')}</FormLabel>
                                    <NumberInput 
                                      size="sm" 
                                      value={currentParams.priceInterval} 
                                      step={1} 
                                      min={0.001}
                                      onChange={(_, val) => updateGridParams(exchangeId, symbol, { priceInterval: val || getDefaultPriceInterval(symbol) })}
                                    >
                                      <NumberInputField borderRadius="lg" />
                                    </NumberInput>
                                  </FormControl>
                                  <FormControl flex={1}>
                                    <FormLabel fontSize="xs">{t('aiConfig.wizard.orderWindow')}</FormLabel>
                                    <NumberInput 
                                      size="sm" 
                                      value={currentParams.orderWindow} 
                                      min={1}
                                      onChange={(_, val) => updateGridParams(exchangeId, symbol, { orderWindow: val || 10 })}
                                    >
                                      <NumberInputField borderRadius="lg" />
                                    </NumberInput>
                                  </FormControl>
                                </HStack>
                                <HStack>
                                  <FormControl flex={1}>
                                    <FormLabel fontSize="xs">{t('aiConfig.wizard.orderAmount')}</FormLabel>
                                    <NumberInput 
                                      size="sm" 
                                      value={currentParams.orderAmount} 
                                      min={5}
                                      onChange={(_, val) => updateGridParams(exchangeId, symbol, { orderAmount: val || 5 })}
                                    >
                                      <NumberInputField borderRadius="lg" />
                                    </NumberInput>
                                  </FormControl>
                                  <FormControl flex={1}>
                                    <FormLabel fontSize="xs">{t('aiConfig.wizard.maxGridLevels')}</FormLabel>
                                    <NumberInput 
                                      size="sm" 
                                      value={currentParams.maxGridLevels} 
                                      min={1}
                                      onChange={(_, val) => updateGridParams(exchangeId, symbol, { maxGridLevels: val || 50 })}
                                    >
                                      <NumberInputField borderRadius="lg" />
                                    </NumberInput>
                                  </FormControl>
                                </HStack>
                              </VStack>
                            </Box>
                          )
                        })}
                      </VStack>
                    </Box>
                  )
                })}
              </VStack>
            </VStack>
          )}

          {step === 'withdrawal-setup' && (
            <VStack spacing={6} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>{t('aiConfig.wizard.step5Title')}</AlertTitle>
                  <AlertDescription fontSize="sm">
                    {t('aiConfig.wizard.step5Desc')}
                  </AlertDescription>
                </Box>
              </Alert>

              {/* 基础划轉設置 */}
              <Box p={5} bg={infoBg} borderRadius="2xl" border="1px" borderColor={borderColor}>
                <VStack spacing={5} align="stretch">
                  <FormControl display="flex" alignItems="center" justifyContent="space-between">
                    <Box>
                      <FormLabel mb="0" fontWeight="bold">{t('aiConfig.wizard.enableAutoTransfer')}</FormLabel>
                      <Text fontSize="xs" color="gray.500">{t('aiConfig.wizard.autoTransferDesc')}</Text>
                    </Box>
                    <RadioGroup 
                      value={withdrawalPolicy.enabled ? 'true' : 'false'} 
                      onChange={(v) => setWithdrawalPolicy(prev => ({ ...prev, enabled: v === 'true' }))}
                    >
                      <Stack direction="row">
                        <Radio value="true">{t('aiConfig.wizard.on')}</Radio>
                        <Radio value="false">{t('aiConfig.wizard.off')}</Radio>
                      </Stack>
                    </RadioGroup>
                  </FormControl>

                  {withdrawalPolicy.enabled && (
                    <>
                      <Divider />
                      
                      {/* 划轉模式选擇 */}
                      <FormControl>
                        <FormLabel fontWeight="bold">{t('aiConfig.wizard.transferMode')}</FormLabel>
                        <Select 
                          value={withdrawalPolicy.mode || 'threshold'}
                          onChange={(e) => setWithdrawalPolicy(prev => ({ ...prev, mode: e.target.value as any }))}
                          borderRadius="xl"
                        >
                          <option value="threshold">{t('aiConfig.wizard.modeThreshold')}</option>
                          <option value="fixed">{t('aiConfig.wizard.modeFixed')}</option>
                          <option value="tiered">{t('aiConfig.wizard.modeTiered')}</option>
                          <option value="scheduled">{t('aiConfig.wizard.modeScheduled')}</option>
                        </Select>
                      </FormControl>

                      {/* 阈值模式設置 */}
                      {(withdrawalPolicy.mode === 'threshold' || !withdrawalPolicy.mode) && (
                        <FormControl>
                          <FormLabel fontWeight="bold">{t('aiConfig.wizard.withdrawThreshold')}</FormLabel>
                          <HStack>
                            <NumberInput 
                              flex={1}
                              value={(withdrawalPolicy.threshold || 0.1) * 100}
                              onChange={(_, val) => setWithdrawalPolicy(prev => ({ ...prev, threshold: val / 100 }))}
                              min={1}
                              max={100}
                            >
                              <NumberInputField borderRadius="xl" />
                            </NumberInput>
                            <Text fontWeight="bold">%</Text>
                          </HStack>
                          <Text fontSize="xs" color="gray.500" mt={1}>
                            {t('aiConfig.wizard.withdrawThresholdDesc')}
                          </Text>
                        </FormControl>
                      )}

                      {/* 固定金額模式設置 */}
                      {withdrawalPolicy.mode === 'fixed' && (
                        <FormControl>
                          <FormLabel fontWeight="bold">{t('aiConfig.wizard.fixedAmount')}</FormLabel>
                          <NumberInput 
                            value={withdrawalPolicy.fixed_amount || 100}
                            onChange={(_, val) => setWithdrawalPolicy(prev => ({ ...prev, fixed_amount: val }))}
                            min={10}
                          >
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                          <Text fontSize="xs" color="gray.500" mt={1}>
                            {t('aiConfig.wizard.fixedAmountDesc')}
                          </Text>
                        </FormControl>
                      )}

                      {/* 阶梯模式設置 */}
                      {withdrawalPolicy.mode === 'tiered' && (
                        <Box>
                          <FormLabel fontWeight="bold">{t('aiConfig.wizard.tieredRules')}</FormLabel>
                          <VStack spacing={2} align="stretch">
                            {(withdrawalPolicy.tiered_rules || [
                              { profit_threshold: 0.1, withdraw_ratio: 0.3 },
                              { profit_threshold: 0.2, withdraw_ratio: 0.5 },
                              { profit_threshold: 0.3, withdraw_ratio: 0.7 },
                            ]).map((rule, idx) => (
                              <HStack key={idx} spacing={2}>
                                <Text fontSize="sm" minW="80px">{t('aiConfig.wizard.profitGte')}</Text>
                                <NumberInput 
                                  size="sm" 
                                  maxW="70px"
                                  value={rule.profit_threshold * 100}
                                  onChange={(_, val) => {
                                    const rules = [...(withdrawalPolicy.tiered_rules || [])]
                                    rules[idx] = { ...rules[idx], profit_threshold: val / 100 }
                                    setWithdrawalPolicy(prev => ({ ...prev, tiered_rules: rules }))
                                  }}
                                >
                                  <NumberInputField />
                                </NumberInput>
                                <Text fontSize="sm">{t('aiConfig.wizard.transferAt')}</Text>
                                <NumberInput 
                                  size="sm" 
                                  maxW="70px"
                                  value={rule.withdraw_ratio * 100}
                                  onChange={(_, val) => {
                                    const rules = [...(withdrawalPolicy.tiered_rules || [])]
                                    rules[idx] = { ...rules[idx], withdraw_ratio: val / 100 }
                                    setWithdrawalPolicy(prev => ({ ...prev, tiered_rules: rules }))
                                  }}
                                >
                                  <NumberInputField />
                                </NumberInput>
                                <Text fontSize="sm">%</Text>
                              </HStack>
                            ))}
                          </VStack>
                          <Text fontSize="xs" color="gray.500" mt={1}>
                            {t('aiConfig.wizard.tieredExample')}
                          </Text>
                        </Box>
                      )}

                      {/* 定時模式設置 */}
                      {withdrawalPolicy.mode === 'scheduled' && (
                        <HStack spacing={4}>
                          <FormControl flex={1}>
                            <FormLabel fontWeight="bold">{t('aiConfig.wizard.transferCycle')}</FormLabel>
                            <Select 
                              value={withdrawalPolicy.schedule?.frequency || 'daily'}
                              onChange={(e) => setWithdrawalPolicy(prev => ({ 
                                ...prev, 
                                schedule: { ...prev.schedule, enabled: true, frequency: e.target.value as any } 
                              }))}
                              borderRadius="xl"
                            >
                              <option value="daily">{t('aiConfig.wizard.daily')}</option>
                              <option value="weekly">{t('aiConfig.wizard.weekly')}</option>
                              <option value="monthly">{t('aiConfig.wizard.monthly')}</option>
                            </Select>
                          </FormControl>
                          <FormControl flex={1}>
                            <FormLabel fontWeight="bold">{t('aiConfig.wizard.transferTime')}</FormLabel>
                            <Input 
                              type="time" 
                              value={withdrawalPolicy.schedule?.time_of_day || '23:00'}
                              onChange={(e) => setWithdrawalPolicy(prev => ({ 
                                ...prev, 
                                schedule: { ...prev.schedule, time_of_day: e.target.value } 
                              }))}
                              borderRadius="xl"
                            />
                          </FormControl>
                        </HStack>
                      )}

                      {/* 划轉比例 */}
                      <FormControl>
                        <FormLabel fontWeight="bold">{t('aiConfig.wizard.transferRatio')}</FormLabel>
                        <HStack>
                          <NumberInput 
                            flex={1}
                            value={(withdrawalPolicy.withdraw_ratio || 1) * 100}
                            onChange={(_, val) => setWithdrawalPolicy(prev => ({ ...prev, withdraw_ratio: val / 100 }))}
                            min={10}
                            max={100}
                          >
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                          <Text fontWeight="bold">%</Text>
                        </HStack>
                        <Text fontSize="xs" color="gray.500" mt={1}>
                          {t('aiConfig.wizard.transferRatioDesc')}
                        </Text>
                      </FormControl>
                    </>
                  )}
                </VStack>
              </Box>

              {/* 本金保护設置 */}
              <Box p={5} bg={infoBg} borderRadius="2xl" border="1px" borderColor={borderColor}>
                <VStack spacing={4} align="stretch">
                  <Text fontWeight="bold" fontSize="md">{t('aiConfig.wizard.principalProtection')}</Text>
                  
                  <FormControl display="flex" alignItems="center" justifyContent="space-between">
                    <Box>
                      <FormLabel mb="0" fontSize="sm">{t('aiConfig.wizard.breakevenProtection')}</FormLabel>
                      <Text fontSize="xs" color="gray.500">{t('aiConfig.wizard.breakevenProtectionDesc')}</Text>
                    </Box>
                    <RadioGroup 
                      value={withdrawalPolicy.principal_protection?.breakeven_protection ? 'true' : 'false'} 
                      onChange={(v) => setWithdrawalPolicy(prev => ({ 
                        ...prev, 
                        principal_protection: { 
                          ...prev.principal_protection, 
                          enabled: true,
                          breakeven_protection: v === 'true' 
                        } 
                      }))}
                    >
                      <Stack direction="row">
                        <Radio value="true" size="sm">{t('aiConfig.wizard.on')}</Radio>
                        <Radio value="false" size="sm">{t('aiConfig.wizard.off')}</Radio>
                      </Stack>
                    </RadioGroup>
                  </FormControl>

                  <FormControl display="flex" alignItems="center" justifyContent="space-between">
                    <Box>
                      <FormLabel mb="0" fontSize="sm">{t('aiConfig.wizard.withdrawPrincipal')}</FormLabel>
                      <Text fontSize="xs" color="gray.500">{t('aiConfig.wizard.withdrawPrincipalDesc')}</Text>
                    </Box>
                    <RadioGroup 
                      value={withdrawalPolicy.principal_protection?.withdraw_principal ? 'true' : 'false'} 
                      onChange={(v) => setWithdrawalPolicy(prev => ({ 
                        ...prev, 
                        principal_protection: { 
                          ...prev.principal_protection, 
                          enabled: true,
                          withdraw_principal: v === 'true' 
                        } 
                      }))}
                    >
                      <Stack direction="row">
                        <Radio value="true" size="sm">{t('aiConfig.wizard.on')}</Radio>
                        <Radio value="false" size="sm">{t('aiConfig.wizard.off')}</Radio>
                      </Stack>
                    </RadioGroup>
                  </FormControl>

                  <FormControl>
                    <FormLabel fontSize="sm">{t('aiConfig.wizard.maxLossLimit')}</FormLabel>
                    <HStack>
                      <NumberInput 
                        size="sm"
                        flex={1}
                        value={(withdrawalPolicy.principal_protection?.max_loss_ratio || 0.2) * 100}
                        onChange={(_, val) => setWithdrawalPolicy(prev => ({ 
                          ...prev, 
                          principal_protection: { 
                            ...prev.principal_protection, 
                            enabled: true,
                            max_loss_ratio: val / 100 
                          } 
                        }))}
                        min={5}
                        max={50}
                      >
                        <NumberInputField borderRadius="xl" />
                      </NumberInput>
                      <Text fontSize="sm">%</Text>
                    </HStack>
                    <Text fontSize="xs" color="gray.500" mt={1}>
                      {t('aiConfig.wizard.maxLossLimitDesc')}
                    </Text>
                  </FormControl>
                </VStack>
              </Box>

              <Alert status="warning" borderRadius="xl" fontSize="xs">
                <AlertIcon />
                {t('aiConfig.wizard.withdrawWarning')}
              </Alert>
            </VStack>
          )}

          {step === 'preview' && aiConfig && (
            <VStack spacing={4} align="stretch">
              <Alert status="success" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>{t('aiConfig.wizard.configGenSuccess')}</AlertTitle>
                  <AlertDescription fontSize="sm">
                    {t('aiConfig.wizard.previewHint')}
                  </AlertDescription>
                </Box>
              </Alert>

              <Box>
                <Text fontWeight="bold" mb={2}>{t('aiConfig.wizard.aiThinking')}</Text>
                <Box
                  p={4}
                  bg={infoBg}
                  borderRadius="md"
                  fontSize="sm"
                  whiteSpace="pre-wrap"
                >
                  {aiConfig.explanation}
                </Box>
              </Box>

              <Divider />

              {aiConfig.symbols_config && aiConfig.symbols_config.length > 0 ? (
                <Box>
                  <Text fontWeight="bold" mb={2}>{t('aiConfig.wizard.tieredConfigDetail')}</Text>
                  <VStack spacing={4} align="stretch">
                    {aiConfig.symbols_config.map((sc, idx) => (
                      <Box key={idx} p={3} border="1px" borderColor={borderColor} borderRadius="lg">
                        <HStack justify="space-between" mb={2}>
                          <Badge colorScheme="green">{sc.symbol}</Badge>
                          <Text fontSize="xs" fontWeight="bold">{t('aiConfig.wizard.allocatedCapital', { capital: sc.total_allocated_capital })}</Text>
                        </HStack>
                        <VStack align="stretch" spacing={1} pl={2}>
                          <HStack justify="space-between">
                            <Text fontSize="xs" color="gray.500">{t('aiConfig.wizard.strategyCombo')}</Text>
                            <HStack>
                              {sc.strategies.map((s, si) => (
                                <Badge key={si} variant="outline" size="xs">{s.type}({(s.weight*100).toFixed(0)}%)</Badge>
                              ))}
                            </HStack>
                          </HStack>
                          <HStack justify="space-between">
                            <Text fontSize="xs" color="gray.500">{t('aiConfig.wizard.gridParamsPreview')}</Text>
                            <Text fontSize="xs">{t('aiConfig.wizard.gridParamsPreviewDetail', { interval: sc.price_interval, window: sc.buy_window_size, amount: sc.order_quantity })}</Text>
                          </HStack>
                          <HStack justify="space-between">
                            <Text fontSize="xs" color="gray.500">{t('aiConfig.wizard.withdrawStrategy')}</Text>
                            <Text fontSize="xs">{sc.withdrawal_policy?.enabled ? t('aiConfig.wizard.withdrawOn', { threshold: (sc.withdrawal_policy.threshold*100).toFixed(0) }) : t('aiConfig.wizard.withdrawOff')}</Text>
                          </HStack>
                        </VStack>
                      </Box>
                    ))}
                  </VStack>
                </Box>
              ) : (
                <>
                  <Box>
                    <Text fontWeight="bold" mb={2}>{t('aiConfig.wizard.gridConfigTitle')}</Text>
                    <TableContainer>
                      <Table size="sm" variant="simple">
                        <Thead>
                          <Tr>
                            <Th>{t('aiConfig.wizard.thSymbol')}</Th>
                            <Th>{t('aiConfig.wizard.thPriceInterval')}</Th>
                            <Th>{t('aiConfig.wizard.thOrderAmount')}</Th>
                            <Th>{t('aiConfig.wizard.thBuyWindow')}</Th>
                            <Th>{t('aiConfig.wizard.thSellWindow')}</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {aiConfig.grid_config.map((grid, idx) => (
                            <Tr key={idx}>
                              <Td><Badge>{grid.symbol}</Badge></Td>
                              <Td>{grid.price_interval.toFixed(2)}</Td>
                              <Td>{grid.order_quantity.toFixed(2)} USDT</Td>
                              <Td>{grid.buy_window_size}</Td>
                              <Td>{grid.sell_window_size}</Td>
                            </Tr>
                          ))}
                        </Tbody>
                      </Table>
                    </TableContainer>
                  </Box>

                  <Divider />

                  <Box>
                    <Text fontWeight="bold" mb={2}>{t('aiConfig.wizard.capitalAllocTitle')}</Text>
                    <TableContainer>
                      <Table size="sm" variant="simple">
                        <Thead>
                          <Tr>
                            <Th>{t('aiConfig.wizard.thSymbol')}</Th>
                            <Th>{t('aiConfig.wizard.thMaxAmount')}</Th>
                            <Th>{t('aiConfig.wizard.thMaxPercent')}</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {aiConfig.allocation.map((alloc, idx) => (
                            <Tr key={idx}>
                              <Td><Badge>{alloc.symbol}</Badge></Td>
                              <Td>{alloc.max_amount_usdt.toFixed(2)}</Td>
                              <Td>{alloc.max_percentage.toFixed(1)}%</Td>
                            </Tr>
                          ))}
                        </Tbody>
                      </Table>
                    </TableContainer>
                  </Box>
                </>
              )}
            </VStack>
          )}

          {step === 'success' && (
            <VStack spacing={4} align="stretch">
              <Alert status="success" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>{t('aiConfig.wizard.applySuccessTitle')}</AlertTitle>
                  <AlertDescription fontSize="sm">
                    {t('aiConfig.wizard.applySuccessDescFull')}
                  </AlertDescription>
                </Box>
              </Alert>
            </VStack>
          )}

          {loading && (
            <VStack spacing={4} py={8}>
              <Spinner size="lg" />
              {taskStatus && (
                <VStack spacing={2} w="full" px={4}>
                  <Text fontSize="sm" color="gray.600">
                    {taskStatus === 'pending' && t('aiConfig.wizard.taskPending')}
                    {taskStatus === 'running' && t('aiConfig.wizard.taskRunning')}
                    {taskStatus === 'completed' && t('aiConfig.wizard.taskCompleted')}
                    {taskStatus === 'failed' && t('aiConfig.wizard.taskFailed')}
                  </Text>
                  <Progress 
                    value={taskProgress} 
                    colorScheme="blue" 
                    size="sm" 
                    w="full" 
                    borderRadius="md"
                    isAnimated={taskStatus === 'running'}
                  />
                  <Text fontSize="xs" color="gray.500">
                    {taskProgress}%
                  </Text>
                </VStack>
              )}
            </VStack>
          )}
        </ModalBody>

        <ModalFooter>
          <HStack spacing={3} w="full">
            {step !== 'success' && (
              <Button variant="ghost" onClick={handleClose}>
                {t('aiConfig.wizard.cancel')}
              </Button>
            )}
            <Box flex={1} />
            {['asset-alloc', 'strategy-split', 'param-tuning', 'withdrawal-setup', 'preview'].includes(step) && (
              <Button variant="outline" onClick={handleBack}>
                {t('aiConfig.wizard.prevStep')}
              </Button>
            )}
            {['ai-setup', 'asset-alloc', 'strategy-split', 'param-tuning'].includes(step) && (
              <Button
                colorScheme="blue"
                onClick={handleNext}
                isDisabled={step === 'ai-setup' && !geminiApiKey.trim()}
              >
                {t('aiConfig.wizard.nextStep')}
              </Button>
            )}
            {step === 'withdrawal-setup' && (
              <Button
                colorScheme="blue"
                onClick={handleSaveDirectly}
                isLoading={loading}
              >
                {t('aiConfig.wizard.saveConfig')}
              </Button>
            )}
            {step === 'preview' && (
              <Button
                colorScheme="green"
                onClick={handleApply}
                isLoading={loading}
              >
                {t('aiConfig.wizard.applyConfig')}
              </Button>
            )}
            {step === 'success' && (
              <Button colorScheme="blue" onClick={handleClose}>
                {t('aiConfig.wizard.done')}
              </Button>
            )}
          </HStack>
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}

export default AIConfigWizard
