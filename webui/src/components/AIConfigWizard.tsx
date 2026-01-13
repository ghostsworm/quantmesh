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
} from '@chakra-ui/react'
import { ViewIcon, ViewOffIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { generateAIConfig, applyAIConfig, AIGenerateConfigRequest, AIGenerateConfigResponse, SymbolCapitalConfig } from '../services/api'
import { getConfig, StrategyInstance, WithdrawalPolicy } from '../services/config'
import { getStrategyTypes } from '../services/strategy'

interface AIConfigWizardProps {
  isOpen: boolean
  onClose: () => void
  onSuccess?: () => void
  // 从父组件传入的已选交易所和币种
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
  
  // AI 访问模式
  const [accessMode, setAccessMode] = useState<'native' | 'proxy'>('proxy') // 默认推荐 proxy
  
  // 代理服务配置
  const [proxyBaseURL, setProxyBaseURL] = useState('https://gemini.facev.app')
  const [proxyUsername, setProxyUsername] = useState('admin123')
  const [proxyPassword, setProxyPassword] = useState('admin123')
  const [showProxyPassword, setShowProxyPassword] = useState(false)

  // 资产分配状态
  const [selectedSymbols, setSelectedSymbols] = useState<string[]>([])
  const [symbolAllocations, setSymbolAllocations] = useState<Record<string, number>>({}) // symbol -> weight (0-1)

  // 策略分配状态
  const [strategySplits, setStrategySplits] = useState<Record<string, StrategyInstance[]>>({}) // symbol -> strategies

  // 可用的策略类型列表
  const [availableStrategyTypes, setAvailableStrategyTypes] = useState<string[]>(['grid', 'dca'])

  // 提现策略状态
  const [withdrawalPolicy, setWithdrawalPolicy] = useState<WithdrawalPolicy>({
    enabled: true,
    threshold: 0.1, // 默认 10%
    mode: 'threshold',
    withdraw_ratio: 1, // 默认划转全部利润
    principal_protection: {
      enabled: true,
      breakeven_protection: true,
      withdraw_principal: false,
      principal_withdraw_at: 1.0,
      max_loss_ratio: 0.2,
    },
  })
  
  // 资金配置模式: 'total' = 总金额模式, 'per_symbol' = 按币种分配
  const [capitalMode, setCapitalMode] = useState<'total' | 'per_symbol'>('total')
  
  // 总金额模式的资金
  const [totalCapital, setTotalCapital] = useState(10000)
  
  // 按币种分配模式的资金
  const [symbolCapitals, setSymbolCapitals] = useState<SymbolCapitalConfig[]>([])
  
  // 风险偏好
  const [riskProfile, setRiskProfile] = useState<'conservative' | 'balanced' | 'aggressive'>('balanced')
  
  const [aiConfig, setAiConfig] = useState<AIGenerateConfigResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  const bg = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.700')
  const infoBg = useColorModeValue('gray.50', 'gray.700')

  // 使用传入的交易所，如果没有传入则从配置获取或默认
  const [exchange, setExchange] = useState(propsExchange || 'binance')
  const symbols = propsSymbols || [] // 可选的所有币种列表

  // 弹窗打开时，预填配置中的 Gemini Key 和访问模式（若存在）
  useEffect(() => {
    if (!isOpen) return
    const loadConfigData = async () => {
      try {
        const cfg = await getConfig()
        
        // 预填交易所
        if (!propsExchange && cfg?.app?.current_exchange) {
          setExchange(cfg.app.current_exchange)
        }

        const keyFromConfig =
          cfg?.ai?.gemini_api_key ||
          cfg?.ai?.api_key ||
          ''
        if (keyFromConfig) {
          setGeminiApiKey(prev => prev || keyFromConfig)
        }
        
        // 加载访问模式配置
        if (cfg?.ai?.access_mode) {
          setAccessMode(cfg.ai.access_mode as 'native' | 'proxy')
        }
        
        // 加载代理配置
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

        // 加载可用的策略类型
        try {
          const typesResp = await getStrategyTypes()
          if (typesResp.types && typesResp.types.length > 0) {
            setAvailableStrategyTypes(typesResp.types)
          }
        } catch (err) {
          console.error('加载策略类型失败:', err)
          // 使用默认策略类型
        }
      } catch (err) {
        console.error('加载配置失败:', err)
      }
    }
    loadConfigData()
  }, [isOpen, propsExchange, propsSymbols])

  // 当币种列表变化时，初始化按币种分配的资金
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
        setError('请输入 Gemini API Key')
        return
      }
      setStep('asset-alloc')
    } else if (step === 'asset-alloc') {
      if (selectedSymbols.length === 0) {
        setError('请选择至少一个交易币种')
        return
      }
      setStep('strategy-split')
    } else if (step === 'strategy-split') {
      setStep('param-tuning')
    } else if (step === 'param-tuning') {
      setStep('withdrawal-setup')
    } else if (step === 'withdrawal-setup') {
      handleGenerate()
    }
    setError(null)
  }

  const handleBack = () => {
    if (step === 'asset-alloc') setStep('ai-setup')
    else if (step === 'strategy-split') setStep('asset-alloc')
    else if (step === 'param-tuning') setStep('strategy-split')
    else if (step === 'withdrawal-setup') setStep('param-tuning')
    else if (step === 'preview') setStep('withdrawal-setup')
    setError(null)
  }

  // 更新单个币种的资金
  const handleSymbolCapitalChange = (symbol: string, capital: number) => {
    setSymbolCapitals(prev => prev.map(sc => 
      sc.symbol === symbol ? { ...sc, capital } : sc
    ))
  }

  // 计算按币种分配的总资金
  const totalSymbolCapitals = symbolCapitals.reduce((sum, sc) => sum + sc.capital, 0)

  const handleGenerate = async () => {
    // 验证 Gemini API Key
    if (!geminiApiKey.trim()) {
      setError('请输入 Gemini API Key')
      return
    }

    // 验证币种
    if (selectedSymbols.length === 0) {
      setError('请选择交易币种')
      return
    }

    setLoading(true)
    setError(null)

    try {
      // 传递 API Key 和访问模式配置给后端
      const formData: AIGenerateConfigRequest = {
        exchange,
        symbols: selectedSymbols,
        capital_mode: capitalMode,
        risk_profile: riskProfile,
        gemini_api_key: geminiApiKey,  // 传递 API Key
        access_mode: accessMode,  // 传递访问模式
        
        // 资产优先重构新增字段
        symbol_allocations: symbolAllocations,
        strategy_splits: strategySplits,
        withdrawal_policy: withdrawalPolicy,
      }
      
      // 如果使用代理模式，传递代理配置
      if (accessMode === 'proxy') {
        formData.proxy_base_url = proxyBaseURL
        formData.proxy_username = proxyUsername
        formData.proxy_password = proxyPassword
      }

      if (capitalMode === 'total') {
        formData.total_capital = totalCapital
      } else {
        formData.symbol_capitals = symbolCapitals.filter(sc => sc.capital > 0)
      }

      const config = await generateAIConfig(formData)
      setAiConfig(config)
      setStep('preview')
      toast({
        title: '配置生成成功',
        status: 'success',
        duration: 3000,
      })
    } catch (err: any) {
      const errorMsg = err.message || '生成配置失败，请检查 Gemini API Key 是否正确'
      setError(errorMsg)
      toast({
        title: '生成配置失败',
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
        title: '配置应用成功',
        description: '请重启服务使配置生效',
        status: 'success',
        duration: 5000,
      })
      if (onSuccess) {
        onSuccess()
      }
    } catch (err: any) {
      const errorMsg = err.message || '应用配置失败'
      setError(errorMsg)
      toast({
        title: '应用配置失败',
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

  // AI 推荐：资产比例分配
  const [recommendingAllocations, setRecommendingAllocations] = useState(false)
  const handleAIRecommendAllocations = async () => {
    if (!geminiApiKey.trim()) {
      toast({ title: '请先设置 Gemini API Key', status: 'warning', duration: 3000 })
      return
    }
    if (selectedSymbols.length === 0) {
      toast({ title: '请先选择交易币种', status: 'warning', duration: 3000 })
      return
    }

    setRecommendingAllocations(true)
    toast({ title: 'AI 正在分析行情推荐比例...', status: 'info', duration: 2000 })

    try {
      const request: AIGenerateConfigRequest = {
        exchange,
        symbols: selectedSymbols,
        capital_mode: 'total',
        total_capital: totalCapital,
        risk_profile: riskProfile,
        gemini_api_key: geminiApiKey,
        access_mode: accessMode,
        proxy_base_url: accessMode === 'proxy' ? proxyBaseURL : undefined,
        proxy_username: accessMode === 'proxy' ? proxyUsername : undefined,
        proxy_password: accessMode === 'proxy' ? proxyPassword : undefined,
      }

      const result = await generateAIConfig(request)
      
      // 从 AI 结果中提取资产分配比例
      if (result.allocation && result.allocation.length > 0) {
        const totalAlloc = result.allocation.reduce((sum, a) => sum + (a.max_percentage || 0), 0)
        const newAllocations: Record<string, number> = {}
        result.allocation.forEach(a => {
          if (selectedSymbols.includes(a.symbol)) {
            // 将百分比转换为 0-1 的权重
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
        toast({ title: 'AI 推荐比例已应用', status: 'success', duration: 3000 })
      } else {
        // 如果没有返回分配结果，使用均等分配
        const equalWeight = 1 / selectedSymbols.length
        const newAllocations: Record<string, number> = {}
        selectedSymbols.forEach(s => newAllocations[s] = equalWeight)
        setSymbolAllocations(newAllocations)
        toast({ title: 'AI 建议均等分配', status: 'info', duration: 3000 })
      }
    } catch (err: any) {
      console.error('AI 推荐失败:', err)
      toast({ 
        title: 'AI 推荐失败', 
        description: err.message || '请检查网络和 API Key', 
        status: 'error', 
        duration: 5000 
      })
    } finally {
      setRecommendingAllocations(false)
    }
  }

  // AI 推荐：单个币种的策略比例
  const [recommendingStrategy, setRecommendingStrategy] = useState<string | null>(null)
  const handleAIRecommendStrategy = async (symbol: string) => {
    if (!geminiApiKey.trim()) {
      toast({ title: '请先设置 Gemini API Key', status: 'warning', duration: 3000 })
      return
    }

    setRecommendingStrategy(symbol)
    toast({ title: `AI 正在为 ${symbol} 推荐策略比例...`, status: 'info', duration: 2000 })

    try {
      const symbolCapital = totalCapital * (symbolAllocations[symbol] || 0)
      const request: AIGenerateConfigRequest = {
        exchange,
        symbols: [symbol],
        capital_mode: 'per_symbol',
        symbol_capitals: [{ symbol, capital: symbolCapital }],
        risk_profile: riskProfile,
        gemini_api_key: geminiApiKey,
        access_mode: accessMode,
        proxy_base_url: accessMode === 'proxy' ? proxyBaseURL : undefined,
        proxy_username: accessMode === 'proxy' ? proxyUsername : undefined,
        proxy_password: accessMode === 'proxy' ? proxyPassword : undefined,
      }

      const result = await generateAIConfig(request)
      
      // 根据风险偏好为所有可用策略分配权重
      const strategyWeights = getRecommendedStrategyWeights(riskProfile, availableStrategyTypes)

      // 如果 AI 返回了具体配置，根据配置调整
      if (result.grid_config && result.grid_config.length > 0) {
        const gridConfig = result.grid_config.find(g => g.symbol === symbol)
        if (gridConfig && strategyWeights['grid']) {
          // 根据网格参数推断风险偏好微调
          const gridLayers = gridConfig.grid_risk_control?.max_grid_layers || 10
          if (gridLayers > 15) {
            strategyWeights['grid'] = Math.min(strategyWeights['grid'] * 1.15, 0.9)
          } else if (gridLayers < 8) {
            strategyWeights['grid'] = strategyWeights['grid'] * 0.85
          }
        }
      }

      // 归一化权重，确保总和为 1
      const totalWeight = Object.values(strategyWeights).reduce((a, b) => a + b, 0)
      const normalizedWeights: Record<string, number> = {}
      for (const [type, weight] of Object.entries(strategyWeights)) {
        normalizedWeights[type] = totalWeight > 0 ? weight / totalWeight : 0
      }

      const newStrategies: StrategyInstance[] = availableStrategyTypes
        .filter(type => normalizedWeights[type] > 0)
        .map(type => ({
          type,
          weight: normalizedWeights[type],
          name: `${type}-${symbol}`,
        }))

      setStrategySplits(prev => ({
        ...prev,
        [symbol]: newStrategies
      }))

      // 构建描述信息
      const description = newStrategies
        .filter(s => s.weight > 0.01)
        .map(s => `${getStrategyDisplayName(s.type)} ${(s.weight * 100).toFixed(0)}%`)
        .join(', ')

      toast({ 
        title: `${symbol} 策略比例已更新`, 
        description: description || '已分配策略权重',
        status: 'success', 
        duration: 3000 
      })
    } catch (err: any) {
      console.error('AI 推荐策略失败:', err)
      toast({ 
        title: 'AI 推荐失败', 
        description: err.message || '请检查网络和 API Key', 
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

  // 策略类型显示名称映射
  const getStrategyDisplayName = (type: string): string => {
    const names: Record<string, string> = {
      grid: '网格策略',
      dca: 'DCA 定投',
      martingale: '马丁格尔',
      trend: '趋势跟踪',
      mean_reversion: '均值回归',
      breakout: '突破策略',
      combo: '组合策略',
      momentum: '动量策略',
    }
    return names[type] || type
  }

  // 根据风险偏好获取推荐的策略权重分配
  const getRecommendedStrategyWeights = (
    profile: 'conservative' | 'balanced' | 'aggressive',
    types: string[]
  ): Record<string, number> => {
    // 不同风险偏好下各策略的基础权重
    const weightProfiles: Record<string, Record<string, number>> = {
      conservative: {
        grid: 0.35,
        dca: 0.35,
        mean_reversion: 0.15,
        trend: 0.10,
        martingale: 0.05,
        breakout: 0.00,
        momentum: 0.00,
        combo: 0.00,
      },
      balanced: {
        grid: 0.40,
        dca: 0.25,
        trend: 0.15,
        mean_reversion: 0.10,
        martingale: 0.05,
        momentum: 0.05,
        breakout: 0.00,
        combo: 0.00,
      },
      aggressive: {
        grid: 0.30,
        martingale: 0.25,
        trend: 0.20,
        momentum: 0.10,
        breakout: 0.10,
        dca: 0.05,
        mean_reversion: 0.00,
        combo: 0.00,
      },
    }

    const profileWeights = weightProfiles[profile] || weightProfiles.balanced
    const result: Record<string, number> = {}

    // 只返回可用策略类型的权重
    for (const type of types) {
      result[type] = profileWeights[type] || 0
    }

    return result
  }

  return (
    <Modal isOpen={isOpen} onClose={handleClose} size="xl" scrollBehavior="inside">
      <ModalOverlay />
      <ModalContent bg={bg}>
        <ModalHeader>AI 智能配置助手</ModalHeader>
        <ModalCloseButton />
        <ModalBody>
          {step === 'ai-setup' && (
            <VStack spacing={6} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>第一步：AI 环境设置</AlertTitle>
                  <AlertDescription fontSize="sm">
                    配置您的 Gemini AI 访问权限。我们建议使用中转服务以保证连接稳定性。
                  </AlertDescription>
                </Box>
              </Alert>

              <FormControl isRequired>
                <FormLabel>AI 访问方式</FormLabel>
                <RadioGroup value={accessMode} onChange={(value) => setAccessMode(value as 'native' | 'proxy')}>
                  <Stack direction="column" spacing={3}>
                    <Radio value="proxy">
                      <VStack align="start" spacing={0}>
                        <Text fontSize="sm" fontWeight="bold">中转服务 (推荐)</Text>
                        <Text fontSize="xs" color="gray.500">通过 gemini.facev.app 访问，速度更快更稳定</Text>
                      </VStack>
                    </Radio>
                    <Radio value="native">
                      <VStack align="start" spacing={0}>
                        <Text fontSize="sm" fontWeight="bold">原生直连</Text>
                        <Text fontSize="xs" color={accessMode === 'native' ? "red.500" : "gray.500"}>
                          直接访问 Google Gemini API (在国内环境极易超时失败)
                        </Text>
                      </VStack>
                    </Radio>
                  </Stack>
                </RadioGroup>
              </FormControl>

              <FormControl isRequired>
                <FormLabel>Gemini API Key</FormLabel>
                <InputGroup size="md">
                  <Input
                    type={showApiKey ? 'text' : 'password'}
                    placeholder="输入您的 Gemini API Key"
                    value={geminiApiKey}
                    onChange={(e) => setGeminiApiKey(e.target.value)}
                    borderRadius="xl"
                  />
                  <InputRightElement width="3rem">
                    <IconButton
                      h="1.75rem"
                      size="sm"
                      onClick={() => setShowApiKey(!showApiKey)}
                      icon={showApiKey ? <ViewOffIcon /> : <ViewIcon />}
                      aria-label={showApiKey ? '隐藏' : '显示'}
                      variant="ghost"
                    />
                  </InputRightElement>
                </InputGroup>
                <Text fontSize="xs" color="gray.500" mt={2}>
                  还没有 Key? <a href="https://aistudio.google.com/app/apikey" target="_blank" rel="noopener noreferrer" style={{ color: '#3182ce', textDecoration: 'underline' }}>点击这里从 Google AI Studio 获取</a>
                </Text>
              </FormControl>

              {accessMode === 'proxy' && (
                <Box p={4} bg={infoBg} borderRadius="2xl" border="1px" borderColor={borderColor}>
                  <Text fontWeight="bold" fontSize="sm" mb={4}>中转服务详情</Text>
                  <VStack spacing={4}>
                    <FormControl>
                      <FormLabel fontSize="xs">服务地址</FormLabel>
                      <Input
                        size="sm"
                        value={proxyBaseURL}
                        onChange={(e) => setProxyBaseURL(e.target.value)}
                        borderRadius="lg"
                      />
                    </FormControl>
                    <HStack w="full" spacing={4}>
                      <FormControl>
                        <FormLabel fontSize="xs">Basic Auth 账号</FormLabel>
                        <Input
                          size="sm"
                          value={proxyUsername}
                          onChange={(e) => setProxyUsername(e.target.value)}
                          borderRadius="lg"
                        />
                      </FormControl>
                      <FormControl>
                        <FormLabel fontSize="xs">Basic Auth 密码</FormLabel>
                        <InputGroup size="sm">
                          <Input
                            type={showProxyPassword ? 'text' : 'password'}
                            value={proxyPassword}
                            onChange={(e) => setProxyPassword(e.target.value)}
                            borderRadius="lg"
                          />
                          <InputRightElement>
                            <IconButton
                              size="xs"
                              onClick={() => setShowProxyPassword(!showProxyPassword)}
                              icon={showProxyPassword ? <ViewOffIcon /> : <ViewIcon />}
                              aria-label="显示/隐藏"
                              variant="ghost"
                            />
                          </InputRightElement>
                        </InputGroup>
                      </FormControl>
                    </HStack>
                  </VStack>
                </Box>
              )}
            </VStack>
          )}

          {step === 'asset-alloc' && (
            <VStack spacing={6} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>第二步：资产比例分配</AlertTitle>
                  <AlertDescription fontSize="sm">
                    选择您想交易的币种，并设定每个币种占总可用资金 ({totalCapital} USDT) 的比例。
                  </AlertDescription>
                </Box>
              </Alert>

              <Box p={4} bg={infoBg} borderRadius="2xl" border="1px" borderColor={borderColor}>
                <HStack justify="space-between" mb={4}>
                  <Text fontWeight="bold">已选币种权重</Text>
                  <Button 
                    size="xs" 
                    colorScheme="purple" 
                    variant="ghost" 
                    onClick={handleAIRecommendAllocations}
                    isLoading={recommendingAllocations}
                    loadingText="AI 分析中..."
                    isDisabled={selectedSymbols.length === 0}
                  >
                    AI 智能推荐比例
                  </Button>
                </HStack>

                <VStack spacing={4} align="stretch">
                  {selectedSymbols.length === 0 ? (
                    <Center py={4} color="gray.500">
                      请点击下方按钮选择要交易的币种
                    </Center>
                  ) : (
                    selectedSymbols.map(symbol => (
                      <HStack key={symbol} spacing={4}>
                        <Badge colorScheme="green" minW="80px" textAlign="center">{symbol}</Badge>
                        <Box flex={1}>
                          <HStack>
                            <NumberInput
                              size="sm"
                              maxW="100px"
                              value={((symbolAllocations[symbol] || 0) * 100).toFixed(0)}
                              onChange={(_, val) => {
                                setSymbolAllocations(prev => ({ ...prev, [symbol]: val / 100 }))
                              }}
                              min={0}
                              max={100}
                            >
                              <NumberInputField />
                            </NumberInput>
                            <Text fontSize="xs" color="gray.500">%</Text>
                          </HStack>
                        </Box>
                        <Text fontSize="xs" color="gray.500">
                          ≈ {(totalCapital * (symbolAllocations[symbol] || 0)).toFixed(2)} USDT
                        </Text>
                        <IconButton
                          size="xs"
                          icon={<Text>×</Text>}
                          aria-label="移除"
                          onClick={() => setSelectedSymbols(prev => prev.filter(s => s !== symbol))}
                        />
                      </HStack>
                    ))
                  )}
                </VStack>

                <Divider my={4} />
                
                <HStack justify="space-between">
                  <Text fontSize="sm" fontWeight="bold">总计权重:</Text>
                  <Text fontSize="sm" fontWeight="bold" color={
                    Object.values(symbolAllocations).reduce((a, b) => a + b, 0) > 1.0 ? "red.500" : "blue.500"
                  }>
                    {(Object.values(symbolAllocations).reduce((a, b) => a + b, 0) * 100).toFixed(0)}%
                  </Text>
                </HStack>
              </Box>

              <Box>
                <Text fontSize="sm" fontWeight="bold" mb={2}>从交易所可用币种添加:</Text>
                <Wrap>
                  {symbols.filter(s => !selectedSymbols.includes(s)).map(s => (
                    <WrapItem key={s}>
                      <Button
                        size="xs"
                        variant="outline"
                        onClick={() => {
                          setSelectedSymbols(prev => [...prev, s])
                          setSymbolAllocations(prev => ({ ...prev, [s]: 0 }))
                        }}
                      >
                        + {s}
                      </Button>
                    </WrapItem>
                  ))}
                </Wrap>
              </Box>
            </VStack>
          )}

          {step === 'strategy-split' && (
            <VStack spacing={6} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>第三步：策略细分</AlertTitle>
                  <AlertDescription fontSize="sm">
                    为每个币种配置具体的交易策略组合（网格、DCA 等）及其资金权重。
                  </AlertDescription>
                </Box>
              </Alert>

              <VStack spacing={6} align="stretch">
                {selectedSymbols.map(symbol => (
                  <Box key={symbol} p={4} bg={infoBg} borderRadius="2xl" border="1px" borderColor={borderColor}>
                    <HStack justify="space-between" mb={4}>
                      <Badge colorScheme="green" fontSize="md" px={3} py={1}>{symbol}</Badge>
                      <HStack>
                        <Text fontSize="xs" color="gray.500">分配资金: {(totalCapital * (symbolAllocations[symbol] || 0)).toFixed(2)} USDT</Text>
                        <Button 
                          size="xs" 
                          colorScheme="purple" 
                          variant="ghost" 
                          onClick={() => handleAIRecommendStrategy(symbol)}
                          isLoading={recommendingStrategy === symbol}
                          loadingText="AI 分析中..."
                        >
                          AI 推荐
                        </Button>
                      </HStack>
                    </HStack>

                    <VStack spacing={3} align="stretch">
                      {availableStrategyTypes.map(type => {
                        const strategies = strategySplits[symbol] || []
                        const existing = strategies.find(s => s.type === type)
                        const weight = existing ? existing.weight : 0
                        
                        return (
                          <HStack key={type} spacing={4}>
                            <Text fontSize="sm" minW="90px" fontWeight="bold">
                              {getStrategyDisplayName(type)}
                            </Text>
                            <Box flex={1}>
                              <HStack>
                                <NumberInput
                                  size="sm"
                                  maxW="100px"
                                  value={(weight * 100).toFixed(0)}
                                  onChange={(_, val) => {
                                    const others = strategies.filter(s => s.type !== type)
                                    const updated = [...others, { type, weight: val / 100, name: `${type}-${symbol}`, config: existing?.config || {} }]
                                    setStrategySplits(prev => ({ ...prev, [symbol]: updated }))
                                  }}
                                  min={0}
                                  max={100}
                                >
                                  <NumberInputField />
                                </NumberInput>
                                <Text fontSize="xs" color="gray.500">%</Text>
                              </HStack>
                            </Box>
                            <Text fontSize="xs" color="gray.500">
                              ≈ {(totalCapital * (symbolAllocations[symbol] || 0) * weight).toFixed(2)} USDT
                            </Text>
                          </HStack>
                        )
                      })}
                    </VStack>
                  </Box>
                ))}
              </VStack>
            </VStack>
          )}

          {step === 'param-tuning' && (
            <VStack spacing={6} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>第四步：参数调优</AlertTitle>
                  <AlertDescription fontSize="sm">
                    根据选定的策略和资金，微调交易参数。内置公式已自动生成默认值。
                  </AlertDescription>
                </Box>
              </Alert>

              <VStack spacing={6} align="stretch">
                {selectedSymbols.map(symbol => {
                  const strategies = strategySplits[symbol] || []
                  const gridStrategy = strategies.find(s => s.type === 'grid')
                  
                  if (!gridStrategy || gridStrategy.weight === 0) return null

                  const symbolCapital = totalCapital * (symbolAllocations[symbol] || 0) * gridStrategy.weight
                  
                  // 简单公式：每单金额 = 资金 / 20 / 窗口大小
                  const defaultOrderQuantity = (symbolCapital / 20 / 10).toFixed(2)

                  return (
                    <Box key={symbol} p={4} bg={infoBg} borderRadius="2xl" border="1px" borderColor={borderColor}>
                      <HStack justify="space-between" mb={4}>
                        <Badge colorScheme="green" fontSize="sm">网格参数: {symbol}</Badge>
                        <Button size="xs" colorScheme="purple" variant="ghost" onClick={() => {
                          toast({ title: `AI 正在根据市场波动率为 ${symbol} 优化参数...`, status: 'info' })
                        }}>
                          AI 一键优化
                        </Button>
                      </HStack>

                      <VStack spacing={4} align="stretch">
                        <HStack>
                          <FormControl flex={1}>
                            <FormLabel fontSize="xs">价格间隔 (%)</FormLabel>
                            <NumberInput size="sm" defaultValue={0.5} step={0.1} min={0.1}>
                              <NumberInputField borderRadius="lg" />
                            </NumberInput>
                          </FormControl>
                          <FormControl flex={1}>
                            <FormLabel fontSize="xs">买/卖单窗口</FormLabel>
                            <NumberInput size="sm" defaultValue={10} min={1}>
                              <NumberInputField borderRadius="lg" />
                            </NumberInput>
                          </FormControl>
                        </HStack>
                        <HStack>
                          <FormControl flex={1}>
                            <FormLabel fontSize="xs">每单金额 (USDT)</FormLabel>
                            <NumberInput size="sm" defaultValue={parseFloat(defaultOrderQuantity)} min={5}>
                              <NumberInputField borderRadius="lg" />
                            </NumberInput>
                          </FormControl>
                          <FormControl flex={1}>
                            <FormLabel fontSize="xs">最大网格层数</FormLabel>
                            <NumberInput size="sm" defaultValue={50} min={1}>
                              <NumberInputField borderRadius="lg" />
                            </NumberInput>
                          </FormControl>
                        </HStack>
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
                  <AlertTitle>第五步：盈利保护设置</AlertTitle>
                  <AlertDescription fontSize="sm">
                    配置盈利划转和本金保护规则，确保您的利润得到妥善管理。
                  </AlertDescription>
                </Box>
              </Alert>

              {/* 基础划转设置 */}
              <Box p={5} bg={infoBg} borderRadius="2xl" border="1px" borderColor={borderColor}>
                <VStack spacing={5} align="stretch">
                  <FormControl display="flex" alignItems="center" justifyContent="space-between">
                    <Box>
                      <FormLabel mb="0" fontWeight="bold">启用利润自动划转</FormLabel>
                      <Text fontSize="xs" color="gray.500">盈利达到阈值后自动划转到现货钱包</Text>
                    </Box>
                    <RadioGroup 
                      value={withdrawalPolicy.enabled ? 'true' : 'false'} 
                      onChange={(v) => setWithdrawalPolicy(prev => ({ ...prev, enabled: v === 'true' }))}
                    >
                      <Stack direction="row">
                        <Radio value="true">开启</Radio>
                        <Radio value="false">关闭</Radio>
                      </Stack>
                    </RadioGroup>
                  </FormControl>

                  {withdrawalPolicy.enabled && (
                    <>
                      <Divider />
                      
                      {/* 划转模式选择 */}
                      <FormControl>
                        <FormLabel fontWeight="bold">划转模式</FormLabel>
                        <Select 
                          value={withdrawalPolicy.mode || 'threshold'}
                          onChange={(e) => setWithdrawalPolicy(prev => ({ ...prev, mode: e.target.value as any }))}
                          borderRadius="xl"
                        >
                          <option value="threshold">阈值触发 - 盈利达到比例后划转</option>
                          <option value="fixed">固定金额 - 每赚固定金额就划转</option>
                          <option value="tiered">阶梯划转 - 不同盈利水平不同划转比例</option>
                          <option value="scheduled">定时划转 - 按时间周期划转</option>
                        </Select>
                      </FormControl>

                      {/* 阈值模式设置 */}
                      {(withdrawalPolicy.mode === 'threshold' || !withdrawalPolicy.mode) && (
                        <FormControl>
                          <FormLabel fontWeight="bold">提现触发阈值 (%)</FormLabel>
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
                            盈利达到本金的此比例时触发划转
                          </Text>
                        </FormControl>
                      )}

                      {/* 固定金额模式设置 */}
                      {withdrawalPolicy.mode === 'fixed' && (
                        <FormControl>
                          <FormLabel fontWeight="bold">固定划转金额 (USDT)</FormLabel>
                          <NumberInput 
                            value={withdrawalPolicy.fixed_amount || 100}
                            onChange={(_, val) => setWithdrawalPolicy(prev => ({ ...prev, fixed_amount: val }))}
                            min={10}
                          >
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                          <Text fontSize="xs" color="gray.500" mt={1}>
                            每次盈利达到此金额时自动划转
                          </Text>
                        </FormControl>
                      )}

                      {/* 阶梯模式设置 */}
                      {withdrawalPolicy.mode === 'tiered' && (
                        <Box>
                          <FormLabel fontWeight="bold">阶梯划转规则</FormLabel>
                          <VStack spacing={2} align="stretch">
                            {(withdrawalPolicy.tiered_rules || [
                              { profit_threshold: 0.1, withdraw_ratio: 0.3 },
                              { profit_threshold: 0.2, withdraw_ratio: 0.5 },
                              { profit_threshold: 0.3, withdraw_ratio: 0.7 },
                            ]).map((rule, idx) => (
                              <HStack key={idx} spacing={2}>
                                <Text fontSize="sm" minW="80px">盈利 ≥</Text>
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
                                <Text fontSize="sm">% 时划转</Text>
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
                            例如：盈利 10% 时划转 30%，盈利 20% 时划转 50%
                          </Text>
                        </Box>
                      )}

                      {/* 定时模式设置 */}
                      {withdrawalPolicy.mode === 'scheduled' && (
                        <HStack spacing={4}>
                          <FormControl flex={1}>
                            <FormLabel fontWeight="bold">划转周期</FormLabel>
                            <Select 
                              value={withdrawalPolicy.schedule?.frequency || 'daily'}
                              onChange={(e) => setWithdrawalPolicy(prev => ({ 
                                ...prev, 
                                schedule: { ...prev.schedule, enabled: true, frequency: e.target.value as any } 
                              }))}
                              borderRadius="xl"
                            >
                              <option value="daily">每日</option>
                              <option value="weekly">每周</option>
                              <option value="monthly">每月</option>
                            </Select>
                          </FormControl>
                          <FormControl flex={1}>
                            <FormLabel fontWeight="bold">划转时间</FormLabel>
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

                      {/* 划转比例 */}
                      <FormControl>
                        <FormLabel fontWeight="bold">划转比例 (%)</FormLabel>
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
                          触发条件满足时，划转利润的百分比（剩余部分继续复利）
                        </Text>
                      </FormControl>
                    </>
                  )}
                </VStack>
              </Box>

              {/* 本金保护设置 */}
              <Box p={5} bg={infoBg} borderRadius="2xl" border="1px" borderColor={borderColor}>
                <VStack spacing={4} align="stretch">
                  <Text fontWeight="bold" fontSize="md">🛡️ 本金保护</Text>
                  
                  <FormControl display="flex" alignItems="center" justifyContent="space-between">
                    <Box>
                      <FormLabel mb="0" fontSize="sm">回本即保护</FormLabel>
                      <Text fontSize="xs" color="gray.500">盈利回本后自动设置保本止损</Text>
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
                        <Radio value="true" size="sm">开启</Radio>
                        <Radio value="false" size="sm">关闭</Radio>
                      </Stack>
                    </RadioGroup>
                  </FormControl>

                  <FormControl display="flex" alignItems="center" justifyContent="space-between">
                    <Box>
                      <FormLabel mb="0" fontSize="sm">盈利后划转本金</FormLabel>
                      <Text fontSize="xs" color="gray.500">利润足够时优先保护本金</Text>
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
                        <Radio value="true" size="sm">开启</Radio>
                        <Radio value="false" size="sm">关闭</Radio>
                      </Stack>
                    </RadioGroup>
                  </FormControl>

                  <FormControl>
                    <FormLabel fontSize="sm">最大亏损限制 (%)</FormLabel>
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
                      最大允许亏损本金的比例，超过后停止交易
                    </Text>
                  </FormControl>
                </VStack>
              </Box>

              <Alert status="warning" borderRadius="xl" fontSize="xs">
                <AlertIcon />
                注意：提现操作涉及合约账户向现货账户的资金划转，请确保您的 API 拥有划转权限。
              </Alert>
            </VStack>
          )}

          {step === 'preview' && aiConfig && (
            <VStack spacing={4} align="stretch">
              <Alert status="success" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>配置生成成功</AlertTitle>
                  <AlertDescription fontSize="sm">
                    请仔细查看 AI 生成的多级资产配置方案，确认无误后点击"应用配置"
                  </AlertDescription>
                </Box>
              </Alert>

              <Box>
                <Text fontWeight="bold" mb={2}>AI 配置思路</Text>
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
                  <Text fontWeight="bold" mb={2}>分级配置详情</Text>
                  <VStack spacing={4} align="stretch">
                    {aiConfig.symbols_config.map((sc, idx) => (
                      <Box key={idx} p={3} border="1px" borderColor={borderColor} borderRadius="lg">
                        <HStack justify="space-between" mb={2}>
                          <Badge colorScheme="green">{sc.symbol}</Badge>
                          <Text fontSize="xs" fontWeight="bold">分配资金: {sc.total_allocated_capital} USDT</Text>
                        </HStack>
                        <VStack align="stretch" spacing={1} pl={2}>
                          <HStack justify="space-between">
                            <Text fontSize="xs" color="gray.500">策略组合:</Text>
                            <HStack>
                              {sc.strategies.map((s, si) => (
                                <Badge key={si} variant="outline" size="xs">{s.type}({(s.weight*100).toFixed(0)}%)</Badge>
                              ))}
                            </HStack>
                          </HStack>
                          <HStack justify="space-between">
                            <Text fontSize="xs" color="gray.500">网格参数:</Text>
                            <Text fontSize="xs">间隔 {sc.price_interval}% | 窗口 {sc.buy_window_size} | 每单 {sc.order_quantity}U</Text>
                          </HStack>
                          <HStack justify="space-between">
                            <Text fontSize="xs" color="gray.500">提现策略:</Text>
                            <Text fontSize="xs">{sc.withdrawal_policy?.enabled ? `开启 (${(sc.withdrawal_policy.threshold*100).toFixed(0)}% 触发)` : '关闭'}</Text>
                          </HStack>
                        </VStack>
                      </Box>
                    ))}
                  </VStack>
                </Box>
              ) : (
                <>
                  <Box>
                    <Text fontWeight="bold" mb={2}>网格参数配置</Text>
                    <TableContainer>
                      <Table size="sm" variant="simple">
                        <Thead>
                          <Tr>
                            <Th>币种</Th>
                            <Th>价格间隔</Th>
                            <Th>每单金额</Th>
                            <Th>买单窗口</Th>
                            <Th>卖单窗口</Th>
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
                    <Text fontWeight="bold" mb={2}>资金分配配置</Text>
                    <TableContainer>
                      <Table size="sm" variant="simple">
                        <Thead>
                          <Tr>
                            <Th>币种</Th>
                            <Th>最大金额 (USDT)</Th>
                            <Th>最大百分比 (%)</Th>
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
                  <AlertTitle>配置应用成功！</AlertTitle>
                  <AlertDescription fontSize="sm">
                    配置已成功保存到配置文件。请重启服务使配置生效。
                  </AlertDescription>
                </Box>
              </Alert>
            </VStack>
          )}

          {loading && (
            <Center py={8}>
              <Spinner size="lg" />
            </Center>
          )}
        </ModalBody>

        <ModalFooter>
          <HStack spacing={3} w="full">
            {step !== 'success' && (
              <Button variant="ghost" onClick={handleClose}>
                取消
              </Button>
            )}
            <Box flex={1} />
            {['asset-alloc', 'strategy-split', 'param-tuning', 'withdrawal-setup', 'preview'].includes(step) && (
              <Button variant="outline" onClick={handleBack}>
                上一步
              </Button>
            )}
            {['ai-setup', 'asset-alloc', 'strategy-split', 'param-tuning'].includes(step) && (
              <Button
                colorScheme="blue"
                onClick={handleNext}
                isDisabled={step === 'ai-setup' && !geminiApiKey.trim()}
              >
                下一步
              </Button>
            )}
            {step === 'withdrawal-setup' && (
              <Button
                colorScheme="blue"
                onClick={handleGenerate}
                isLoading={loading}
              >
                生成配置建议
              </Button>
            )}
            {step === 'preview' && (
              <Button
                colorScheme="green"
                onClick={handleApply}
                isLoading={loading}
              >
                应用配置
              </Button>
            )}
            {step === 'success' && (
              <Button colorScheme="blue" onClick={handleClose}>
                完成
              </Button>
            )}
          </HStack>
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}

export default AIConfigWizard
