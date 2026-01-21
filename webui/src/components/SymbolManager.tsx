import React, { useState, useEffect } from 'react'
import {
  Box,
  VStack,
  HStack,
  Button,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
  Badge,
  IconButton,
  useDisclosure,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  FormControl,
  FormLabel,
  Input,
  NumberInput,
  NumberInputField,
  Select,
  Text,
  useToast,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Divider,
  Code,
  Tooltip,
  Checkbox,
  CheckboxGroup,
  SimpleGrid,
  Accordion,
  AccordionItem,
  AccordionButton,
  AccordionPanel,
  AccordionIcon,
} from '@chakra-ui/react'
import { AddIcon, DeleteIcon, EditIcon, InfoIcon, StarIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { Config, SymbolConfig } from '../services/config'
import { getExchangeSymbols } from '../services/setup'
import { getSymbols } from '../services/api'

interface SymbolManagerProps {
  config: Config
  onUpdate: (symbols: SymbolConfig[]) => void
}

const SymbolManager: React.FC<SymbolManagerProps> = ({ config, onUpdate }) => {
  const { t } = useTranslation()
  const toast = useToast()
  const { isOpen: isAddOpen, onOpen: onAddOpen, onClose: onAddClose } = useDisclosure()
  const { isOpen: isEditOpen, onOpen: onEditOpen, onClose: onEditClose } = useDisclosure()
  const { isOpen: isQuickSetupOpen, onOpen: onQuickSetupOpen, onClose: onQuickSetupClose } = useDisclosure()
  
  const [symbols, setSymbols] = useState<SymbolConfig[]>(config.trading?.symbols || [])
  const [editingIndex, setEditingIndex] = useState<number>(-1)
  const [availableSymbols, setAvailableSymbols] = useState<string[]>([])
  const [loadingSymbols, setLoadingSymbols] = useState(false)
  const [currentPrice, setCurrentPrice] = useState<number | null>(null)
  const [allocatedCapital, setAllocatedCapital] = useState<number>(0)
  
  // 新手一键设置相关状态
  const [quickSetupSelectedSymbols, setQuickSetupSelectedSymbols] = useState<string[]>([])
  const [quickSetupTotalCapital, setQuickSetupTotalCapital] = useState<number>(1000)
  const [quickSetupLoading, setQuickSetupLoading] = useState(false)
  const [quickSetupSymbolPrices, setQuickSetupSymbolPrices] = useState<Record<string, number>>({})
  
  const [formData, setFormData] = useState<SymbolConfig>({
    exchange: config.app?.current_exchange || '',
    symbol: '',
    price_interval: 2,
    order_quantity: 30,
    min_order_value: 20,
    buy_window_size: 10,
    sell_window_size: 10,
    reconcile_interval: 60,
    order_cleanup_threshold: 50,
    cleanup_batch_size: 20,
    margin_lock_duration_seconds: 20,
    position_safety_check: 100,
  })

  const exchanges = ['binance', 'bitget', 'bybit', 'gate', 'edgex', 'bit']
  const exchangeNames: Record<string, string> = {
    binance: '币安 (Binance)',
    bitget: 'Bitget',
    bybit: 'Bybit',
    gate: 'Gate.io',
    edgex: 'EdgeX',
    bit: 'Bit.com',
  }
  
  // 常用交易对列表（根据交易所格式不同）
  const getCommonSymbols = (exchange: string): string[] => {
    const currentExchange = exchange || config.app?.current_exchange || 'binance'
    if (currentExchange === 'gate') {
      return ['BTC_USDT', 'ETH_USDT', 'SOL_USDT', 'XRP_USDT', 'DOGE_USDT', 'ADA_USDT', 'MATIC_USDT', 'AVAX_USDT']
    }
    // 其他交易所使用标准格式
    return ['BTCUSDT', 'ETHUSDT', 'SOLUSDT', 'XRPUSDT', 'DOGEUSDT', 'ADAUSDT', 'MATICUSDT', 'AVAXUSDT']
  }

  useEffect(() => {
    setSymbols(config.trading?.symbols || [])
  }, [config])
  
  // 加载常用交易对的价格
  const loadQuickSetupPrices = async () => {
    const currentExchange = config.app?.current_exchange || 'binance'
    const commonSymbols = getCommonSymbols(currentExchange)
    const prices: Record<string, number> = {}
    
    try {
      const response = await getSymbols()
      for (const symbol of commonSymbols) {
        // 尝试匹配交易对（考虑不同交易所可能有不同的格式）
        const symbolInfo = response.symbols.find(
          s => {
            // 标准化比较：移除下划线和大小写差异
            const normalizeSymbol = (sym: string) => sym.replace(/_/g, '').toUpperCase()
            return normalizeSymbol(s.symbol) === normalizeSymbol(symbol) && 
                   s.exchange.toLowerCase() === currentExchange.toLowerCase()
          }
        )
        if (symbolInfo && symbolInfo.current_price > 0) {
          prices[symbol] = symbolInfo.current_price
        }
      }
      setQuickSetupSymbolPrices(prices)
    } catch (error) {
      console.error('加载价格失败:', error)
      // 即使失败也继续，用户仍可以手动设置
    }
  }
  
  useEffect(() => {
    if (isQuickSetupOpen) {
      loadQuickSetupPrices()
    }
  }, [isQuickSetupOpen])

  // 获取交易对当前价格
  const fetchSymbolPrice = async (symbol: string, exchange: string) => {
    try {
      const response = await getSymbols()
      const symbolInfo = response.symbols.find(
        s => s.symbol === symbol && s.exchange === exchange
      )
      if (symbolInfo && symbolInfo.current_price > 0) {
        setCurrentPrice(symbolInfo.current_price)
      } else {
        setCurrentPrice(null)
      }
    } catch (error) {
      setCurrentPrice(null)
    }
  }

  // 计算配置建议值
  const calculateRecommendations = (symbol: string, price: number | null, capital: number) => {
    const recommendations: {
      price_interval: { min: number; max: number; suggested: number }
      order_quantity: { min: number; suggested: number }
      buy_window_size: { min: number; suggested: number }
      sell_window_size: { min: number; suggested: number }
      min_order_value: { min: number; suggested: number }
    } = {
      price_interval: { min: 0.0001, max: Infinity, suggested: 2 },
      order_quantity: { min: 1, suggested: 30 },
      buy_window_size: { min: 1, suggested: 10 },
      sell_window_size: { min: 1, suggested: 10 },
      min_order_value: { min: 1, suggested: 20 },
    }

    if (price && price > 0) {
      // 价格间隔建议：币值的 0.1% - 1%
      const minInterval = price * 0.001 // 0.1%
      const maxInterval = price * 0.01  // 1%
      recommendations.price_interval = {
        min: Math.max(0.0001, minInterval),
        max: maxInterval,
        suggested: Math.max(0.1, Math.min(maxInterval, price * 0.005)), // 建议 0.5%
      }
    }

    if (capital > 0) {
      // 订单金额建议：根据资金量
      if (capital < 500) {
        recommendations.order_quantity.suggested = 20
      } else if (capital < 2000) {
        recommendations.order_quantity.suggested = 50
      } else if (capital < 10000) {
        recommendations.order_quantity.suggested = 100
      } else {
        recommendations.order_quantity.suggested = 200
      }

      // 窗口大小建议：根据资金量和订单金额计算
      // 假设每个格子需要 order_quantity 的资金
      // 建议格子数 = 总资金 / (订单金额 * 2) （买单和卖单各一半）
      const suggestedGrids = Math.floor(capital / (recommendations.order_quantity.suggested * 2))
      recommendations.buy_window_size.suggested = Math.max(5, Math.min(20, Math.floor(suggestedGrids / 2)))
      recommendations.sell_window_size.suggested = Math.max(5, Math.min(20, Math.floor(suggestedGrids / 2)))

      // 最小订单价值建议：订单金额的 50% - 100%
      recommendations.min_order_value.suggested = Math.max(10, recommendations.order_quantity.suggested * 0.5)
    }

    return recommendations
  }

  const loadAvailableSymbols = async (exchange: string) => {
    if (!exchange) return
    
    setLoadingSymbols(true)
    try {
      const exchangeConfig = config.exchanges?.[exchange as keyof typeof config.exchanges]
      if (!exchangeConfig?.api_key || !exchangeConfig?.secret_key) {
        toast({
          title: '无法加载交易对',
          description: `请先配置 ${exchangeNames[exchange] || exchange} 的 API Key 和 Secret Key`,
          status: 'warning',
          duration: 3000,
        })
        setAvailableSymbols([])
        return
      }

      const response = await getExchangeSymbols({
        exchange,
        api_key: exchangeConfig.api_key,
        secret_key: exchangeConfig.secret_key,
        passphrase: exchangeConfig.passphrase,
        testnet: exchangeConfig.testnet || false,
      })
      setAvailableSymbols(response.symbols || [])
    } catch (error: any) {
      toast({
        title: '加载交易对失败',
        description: error.message || '请检查 API 配置',
        status: 'error',
        duration: 3000,
      })
      setAvailableSymbols([])
    } finally {
      setLoadingSymbols(false)
    }
  }

  const handleAdd = () => {
    setFormData({
      exchange: config.app?.current_exchange || '',
      symbol: '',
      price_interval: 2,
      order_quantity: 30,
      min_order_value: 20,
      buy_window_size: 10,
      sell_window_size: 10,
      reconcile_interval: 60,
      order_cleanup_threshold: 50,
      cleanup_batch_size: 20,
      margin_lock_duration_seconds: 20,
      position_safety_check: 100,
    })
    setEditingIndex(-1)
    setCurrentPrice(null)
    setAllocatedCapital(0)
    onAddOpen()
  }

  const handleEdit = (index: number) => {
    const symbolData = symbols[index]
    setFormData(symbolData)
    setEditingIndex(index)
    // 加载价格和资金信息
    if (symbolData.symbol) {
      const exchange = symbolData.exchange || config.app?.current_exchange || ''
      if (exchange) {
        fetchSymbolPrice(symbolData.symbol, exchange)
      }
    }
    // 如果有分配资金配置，加载它
    if (symbolData.total_allocated_capital) {
      setAllocatedCapital(symbolData.total_allocated_capital)
    }
    onEditOpen()
  }

  const handleDelete = (index: number) => {
    const newSymbols = symbols.filter((_, i) => i !== index)
    setSymbols(newSymbols)
    onUpdate(newSymbols)
    toast({
      title: '删除成功',
      status: 'success',
      duration: 2000,
    })
  }

  const handleSave = () => {
    if (!formData.symbol) {
      toast({
        title: '请选择交易对',
        status: 'warning',
        duration: 2000,
      })
      return
    }

    // 保存分配资金到配置中
    const symbolDataToSave = {
      ...formData,
      total_allocated_capital: allocatedCapital > 0 ? allocatedCapital : undefined,
    }

    const newSymbols = [...symbols]
    if (editingIndex >= 0) {
      newSymbols[editingIndex] = symbolDataToSave
    } else {
      // 检查是否已存在
      if (newSymbols.some(s => s.symbol === formData.symbol && s.exchange === formData.exchange)) {
        toast({
          title: '交易对已存在',
          description: `${formData.exchange || config.app?.current_exchange}:${formData.symbol} 已在配置中`,
          status: 'warning',
          duration: 3000,
        })
        return
      }
      newSymbols.push(symbolDataToSave)
    }

    setSymbols(newSymbols)
    onUpdate(newSymbols)
    onAddClose()
    onEditClose()
    toast({
      title: editingIndex >= 0 ? '更新成功' : '添加成功',
      status: 'success',
      duration: 2000,
    })
  }
  
  // 新手一键设置处理函数
  const handleQuickSetup = async () => {
    if (quickSetupSelectedSymbols.length === 0) {
      toast({
        title: '请至少选择一个交易对',
        status: 'warning',
        duration: 2000,
      })
      return
    }
    
    if (quickSetupTotalCapital <= 0) {
      toast({
        title: '请输入总资金',
        status: 'warning',
        duration: 2000,
      })
      return
    }
    
    setQuickSetupLoading(true)
    const currentExchange = config.app?.current_exchange || 'binance'
    const exchangeName = exchangeNames[currentExchange] || currentExchange.toUpperCase()
    const capitalPerSymbol = quickSetupTotalCapital / quickSetupSelectedSymbols.length
    
    try {
      const newSymbols: SymbolConfig[] = []
      const existingSymbols: string[] = []
      
      for (const symbol of quickSetupSelectedSymbols) {
        // 检查是否已存在
        if (symbols.some(s => s.symbol === symbol && (s.exchange || currentExchange) === currentExchange)) {
          existingSymbols.push(symbol)
          continue // 跳过已存在的交易对
        }
        
        const price = quickSetupSymbolPrices[symbol] || null
        const rec = calculateRecommendations(symbol, price, capitalPerSymbol)
        
        const symbolConfig: SymbolConfig = {
          exchange: currentExchange,
          symbol,
          price_interval: rec.price_interval.suggested,
          order_quantity: rec.order_quantity.suggested,
          min_order_value: rec.min_order_value.suggested,
          buy_window_size: rec.buy_window_size.suggested,
          sell_window_size: rec.sell_window_size.suggested,
          total_allocated_capital: capitalPerSymbol,
          reconcile_interval: 60,
          order_cleanup_threshold: 50,
          cleanup_batch_size: 20,
          margin_lock_duration_seconds: 20,
          position_safety_check: 100,
        }
        
        newSymbols.push(symbolConfig)
      }
      
      if (newSymbols.length === 0) {
        // 所有交易对都已存在，提示用户删除
        toast({
          title: '所有选中的交易对都已存在',
          description: `以下交易对在 ${exchangeName} 已存在：${existingSymbols.join(', ')}。请先删除现有配置后再添加。`,
          status: 'warning',
          duration: 5000,
          isClosable: true,
        })
        setQuickSetupLoading(false)
        return
      }
      
      // 如果有部分交易对已存在，也提示
      if (existingSymbols.length > 0) {
        toast({
          title: '部分交易对已存在',
          description: `以下交易对已跳过：${existingSymbols.join(', ')}。已成功添加 ${newSymbols.length} 个新交易对。`,
          status: 'info',
          duration: 4000,
          isClosable: true,
        })
      }
      
      const updatedSymbols = [...symbols, ...newSymbols]
      setSymbols(updatedSymbols)
      onUpdate(updatedSymbols)
      
      toast({
        title: '一键设置成功',
        description: `已添加 ${newSymbols.length} 个交易对配置`,
        status: 'success',
        duration: 3000,
      })
      
      onQuickSetupClose()
      setQuickSetupSelectedSymbols([])
    } catch (error: any) {
      toast({
        title: '一键设置失败',
        description: error.message || '请稍后重试',
        status: 'error',
        duration: 3000,
      })
    } finally {
      setQuickSetupLoading(false)
    }
  }

  return (
    <Box>
      <VStack spacing={4} align="stretch">
        <HStack justify="space-between">
          <Text fontSize="sm" fontWeight="600" color="gray.600">
            当前配置了 {symbols.length} 个交易对
          </Text>
          <HStack spacing={2}>
            <Button
              leftIcon={<StarIcon />}
              colorScheme="purple"
              size="sm"
              onClick={onQuickSetupOpen}
              borderRadius="md"
              variant="outline"
            >
              新手一键设置
            </Button>
            <Button
              leftIcon={<AddIcon />}
              colorScheme="blue"
              size="sm"
              onClick={handleAdd}
              borderRadius="md"
            >
              添加交易对
            </Button>
          </HStack>
        </HStack>

        {symbols.length === 0 ? (
          <Alert status="info" borderRadius="md">
            <AlertIcon />
            <AlertDescription>
              还没有配置任何交易对。点击"添加交易对"按钮开始配置。
            </AlertDescription>
          </Alert>
        ) : (
          <TableContainer>
            <Table variant="simple" size="sm">
              <Thead>
                <Tr>
                  <Th>交易所</Th>
                  <Th>交易对</Th>
                  <Th>价格间隔</Th>
                  <Th>订单金额</Th>
                  <Th>买单窗口</Th>
                  <Th>卖单窗口</Th>
                  <Th>操作</Th>
                </Tr>
              </Thead>
              <Tbody>
                {symbols.map((sym, index) => (
                  <Tr key={`${sym.exchange || config.app?.current_exchange}:${sym.symbol}`}>
                    <Td>
                      <Badge colorScheme="blue">
                        {sym.exchange ? exchangeNames[sym.exchange] || sym.exchange : exchangeNames[config.app?.current_exchange || ''] || config.app?.current_exchange}
                      </Badge>
                    </Td>
                    <Td fontWeight="600">{sym.symbol}</Td>
                    <Td>{sym.price_interval}</Td>
                    <Td>{sym.order_quantity}</Td>
                    <Td>{sym.buy_window_size}</Td>
                    <Td>{sym.sell_window_size}</Td>
                    <Td>
                      <HStack spacing={2}>
                        <IconButton
                          aria-label="编辑"
                          icon={<EditIcon />}
                          size="xs"
                          colorScheme="blue"
                          variant="ghost"
                          onClick={() => handleEdit(index)}
                        />
                        <IconButton
                          aria-label="删除"
                          icon={<DeleteIcon />}
                          size="xs"
                          colorScheme="red"
                          variant="ghost"
                          onClick={() => handleDelete(index)}
                        />
                      </HStack>
                    </Td>
                  </Tr>
                ))}
              </Tbody>
            </Table>
          </TableContainer>
        )}
      </VStack>

      {/* 添加/编辑模态框 */}
      <Modal isOpen={isAddOpen || isEditOpen} onClose={editingIndex >= 0 ? onEditClose : onAddClose} size="xl">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{editingIndex >= 0 ? '编辑交易对' : '添加交易对'}</ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <VStack spacing={4}>
              <FormControl isRequired>
                <FormLabel>交易所</FormLabel>
                <Select
                  value={formData.exchange || ''}
                  onChange={(e) => {
                    const exchange = e.target.value
                    setFormData({ ...formData, exchange })
                    if (exchange) {
                      loadAvailableSymbols(exchange)
                      // 如果已有交易对，重新加载价格
                      if (formData.symbol) {
                        fetchSymbolPrice(formData.symbol, exchange || config.app?.current_exchange || '')
                      }
                    }
                  }}
                >
                  <option value="">使用默认交易所 ({exchangeNames[config.app?.current_exchange || ''] || config.app?.current_exchange})</option>
                  {exchanges.map((ex) => (
                    <option key={ex} value={ex}>{exchangeNames[ex] || ex}</option>
                  ))}
                </Select>
              </FormControl>

              <FormControl isRequired>
                <FormLabel>交易对</FormLabel>
                {availableSymbols.length > 0 ? (
                  <Select
                    value={formData.symbol}
                    onChange={(e) => {
                      const symbol = e.target.value
                      setFormData({ ...formData, symbol })
                      if (symbol && formData.exchange) {
                        fetchSymbolPrice(symbol, formData.exchange || config.app?.current_exchange || '')
                      }
                    }}
                    placeholder="选择交易对"
                  >
                    {availableSymbols.map((sym) => (
                      <option key={sym} value={sym}>{sym}</option>
                    ))}
                  </Select>
                ) : (
                  <Input
                    value={formData.symbol}
                    onChange={(e) => {
                      const symbol = e.target.value.toUpperCase()
                      setFormData({ ...formData, symbol })
                      if (symbol && formData.exchange) {
                        fetchSymbolPrice(symbol, formData.exchange || config.app?.current_exchange || '')
                      }
                    }}
                    placeholder="例如: BCHUSDT"
                  />
                )}
                {formData.exchange && (
                  <Button
                    size="xs"
                    variant="link"
                    mt={1}
                    onClick={() => loadAvailableSymbols(formData.exchange || '')}
                    isLoading={loadingSymbols}
                  >
                    {availableSymbols.length > 0 ? '刷新交易对列表' : '从交易所加载交易对列表'}
                  </Button>
                )}
                {formData.symbol && currentPrice && (
                  <Text fontSize="xs" color="gray.500" mt={1}>
                    当前价格: <Code>{currentPrice.toFixed(2)} USDT</Code>
                  </Text>
                )}
              </FormControl>

              <FormControl>
                <FormLabel>分配资金 (USDT)</FormLabel>
                <HStack spacing={2}>
                  <NumberInput
                    flex={1}
                    value={allocatedCapital}
                    onChange={(_, v) => setAllocatedCapital(v)}
                    min={0}
                    precision={2}
                  >
                    <NumberInputField />
                  </NumberInput>
                  {currentPrice && (
                    <Button
                      size="sm"
                      colorScheme="purple"
                      variant="outline"
                      onClick={() => {
                        if (!formData.symbol) {
                          toast({
                            title: '请先选择交易对',
                            description: '需要交易对信息才能计算建议值',
                            status: 'warning',
                            duration: 3000,
                          })
                          return
                        }
                        if (!currentPrice) {
                          toast({
                            title: '无法获取当前价格',
                            description: '请确保已选择交易对并等待价格加载',
                            status: 'warning',
                            duration: 3000,
                          })
                          return
                        }
                        
                        const rec = calculateRecommendations(formData.symbol, currentPrice, allocatedCapital || 0)
                        const updatedFormData: SymbolConfig = {
                          ...formData,
                          price_interval: rec.price_interval.suggested,
                        }
                        
                        // 如果有分配资金，填充其他建议值
                        if (allocatedCapital > 0) {
                          updatedFormData.order_quantity = rec.order_quantity.suggested
                          updatedFormData.buy_window_size = rec.buy_window_size.suggested
                          updatedFormData.sell_window_size = rec.sell_window_size.suggested
                          updatedFormData.min_order_value = rec.min_order_value.suggested
                        }
                        
                        setFormData(updatedFormData)
                        toast({
                          title: '建议值已应用',
                          description: allocatedCapital > 0 
                            ? '已根据当前价格和分配资金自动填充所有建议值'
                            : '已根据当前价格填充价格间隔建议值（请设置分配资金以获取完整建议）',
                          status: 'success',
                          duration: 3000,
                        })
                      }}
                    >
                      一键引入建议值
                    </Button>
                  )}
                </HStack>
                <Text fontSize="xs" color="gray.500" mt={1}>
                  用于计算建议的网格参数
                </Text>
              </FormControl>

              <Divider />

              {/* 配置规则说明 */}
              <Alert status="info" borderRadius="md" fontSize="sm">
                <AlertIcon />
                <VStack align="start" spacing={1}>
                  <Text fontWeight="600">配置规则说明</Text>
                  <Text fontSize="xs">
                    • <strong>价格间隔</strong>: 建议为币值的 0.1% - 1%，太小会导致频繁交易，太大会错过机会
                  </Text>
                  <Text fontSize="xs">
                    • <strong>订单金额</strong>: 根据分配资金量设置，建议单笔订单不超过总资金的 5%
                  </Text>
                  <Text fontSize="xs">
                    • <strong>窗口大小</strong>: 根据资金量和订单金额计算，确保有足够资金覆盖所有网格
                  </Text>
                  <Text fontSize="xs">
                    • <strong>最小订单价值</strong>: 建议不小于订单金额的 50%，避免过小的订单
                  </Text>
                </VStack>
              </Alert>

              <FormControl>
                <FormLabel>
                  <HStack>
                    <Text>价格间隔 (USDT)</Text>
                    <Tooltip label="建议为币值的 0.1% - 1%。如果已选择交易对，会根据当前价格自动计算建议值。">
                      <InfoIcon boxSize={3} color="gray.400" />
                    </Tooltip>
                  </HStack>
                </FormLabel>
                <NumberInput
                  value={formData.price_interval}
                  onChange={(_, v) => setFormData({ ...formData, price_interval: v })}
                  min={0.0001}
                  precision={6}
                >
                  <NumberInputField />
                </NumberInput>
                {formData.symbol && currentPrice && (() => {
                  const rec = calculateRecommendations(formData.symbol, currentPrice, allocatedCapital)
                  const isInRange = formData.price_interval >= rec.price_interval.min && 
                                   formData.price_interval <= rec.price_interval.max
                  return (
                    <Text fontSize="xs" color={isInRange ? "green.500" : "orange.500"} mt={1}>
                      {isInRange ? "✓" : "⚠"} 建议范围: {rec.price_interval.min.toFixed(2)} - {rec.price_interval.max.toFixed(2)} USDT
                      {!isInRange && ` (推荐: ${rec.price_interval.suggested.toFixed(2)} USDT)`}
                    </Text>
                  )
                })()}
              </FormControl>

              <FormControl>
                <FormLabel>
                  <HStack>
                    <Text>订单金额 (USDT)</Text>
                    <Tooltip label="每单购买金额。建议根据分配资金量设置：小额资金(500以下)建议20-30，中等资金(500-2000)建议50-100，大额资金(2000+)建议100-200。">
                      <InfoIcon boxSize={3} color="gray.400" />
                    </Tooltip>
                  </HStack>
                </FormLabel>
                <NumberInput
                  value={formData.order_quantity}
                  onChange={(_, v) => setFormData({ ...formData, order_quantity: v })}
                  min={1}
                  precision={2}
                >
                  <NumberInputField />
                </NumberInput>
                {allocatedCapital > 0 && (() => {
                  const rec = calculateRecommendations(formData.symbol, currentPrice, allocatedCapital)
                  const suggested = rec.order_quantity.suggested
                  const isReasonable = formData.order_quantity >= suggested * 0.5 && formData.order_quantity <= suggested * 2
                  return (
                    <Text fontSize="xs" color={isReasonable ? "green.500" : "orange.500"} mt={1}>
                      {isReasonable ? "✓" : "⚠"} 根据资金量建议: {suggested} USDT
                      {allocatedCapital > 0 && ` (约占总资金 ${((suggested / allocatedCapital) * 100).toFixed(1)}%)`}
                    </Text>
                  )
                })()}
              </FormControl>

              <HStack spacing={4} width="100%">
                <FormControl>
                  <FormLabel>
                    <HStack>
                      <Text>买单窗口大小</Text>
                      <Tooltip label="买单网格层数。建议根据资金量和订单金额计算，确保有足够资金覆盖所有网格。">
                        <InfoIcon boxSize={3} color="gray.400" />
                      </Tooltip>
                    </HStack>
                  </FormLabel>
                  <NumberInput
                    value={formData.buy_window_size}
                    onChange={(_, v) => setFormData({ ...formData, buy_window_size: v })}
                    min={1}
                  >
                    <NumberInputField />
                  </NumberInput>
                  {allocatedCapital > 0 && formData.order_quantity > 0 && (() => {
                    const rec = calculateRecommendations(formData.symbol, currentPrice, allocatedCapital)
                    const suggested = rec.buy_window_size.suggested
                    const maxAffordable = Math.floor(allocatedCapital / formData.order_quantity)
                    const isReasonable = formData.buy_window_size <= maxAffordable
                    return (
                      <Text fontSize="xs" color={isReasonable ? "green.500" : "red.500"} mt={1}>
                        {isReasonable ? "✓" : "⚠"} 建议: {suggested} 层
                        {!isReasonable && ` (最多可承担 ${maxAffordable} 层)`}
                      </Text>
                    )
                  })()}
                </FormControl>

                <FormControl>
                  <FormLabel>
                    <HStack>
                      <Text>卖单窗口大小</Text>
                      <Tooltip label="卖单网格层数。建议与买单窗口大小相同或相近。">
                        <InfoIcon boxSize={3} color="gray.400" />
                      </Tooltip>
                    </HStack>
                  </FormLabel>
                  <NumberInput
                    value={formData.sell_window_size}
                    onChange={(_, v) => setFormData({ ...formData, sell_window_size: v })}
                    min={1}
                  >
                    <NumberInputField />
                  </NumberInput>
                  {allocatedCapital > 0 && formData.order_quantity > 0 && (() => {
                    const rec = calculateRecommendations(formData.symbol, currentPrice, allocatedCapital)
                    const suggested = rec.sell_window_size.suggested
                    return (
                      <Text fontSize="xs" color="green.500" mt={1}>
                        ✓ 建议: {suggested} 层
                      </Text>
                    )
                  })()}
                </FormControl>
              </HStack>

              <FormControl>
                <FormLabel>
                  <HStack>
                    <Text>最小订单价值 (USDT)</Text>
                    <Tooltip label="小于此值的订单不会挂单。建议不小于订单金额的 50%。">
                      <InfoIcon boxSize={3} color="gray.400" />
                    </Tooltip>
                  </HStack>
                </FormLabel>
                <NumberInput
                  value={formData.min_order_value || 20}
                  onChange={(_, v) => setFormData({ ...formData, min_order_value: v })}
                  min={1}
                  precision={2}
                >
                  <NumberInputField />
                </NumberInput>
                {formData.order_quantity > 0 && (() => {
                  const rec = calculateRecommendations(formData.symbol, currentPrice, allocatedCapital)
                  const suggested = rec.min_order_value.suggested
                  const isReasonable = formData.min_order_value >= suggested * 0.5
                  return (
                    <Text fontSize="xs" color={isReasonable ? "green.500" : "orange.500"} mt={1}>
                      {isReasonable ? "✓" : "⚠"} 建议: {suggested} USDT (订单金额的 {((suggested / formData.order_quantity) * 100).toFixed(0)}%)
                    </Text>
                  )
                })()}
              </FormControl>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" mr={3} onClick={editingIndex >= 0 ? onEditClose : onAddClose}>
              取消
            </Button>
            <Button colorScheme="blue" onClick={handleSave}>
              保存
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
      
      {/* 新手一键设置模态框 */}
      <Modal isOpen={isQuickSetupOpen} onClose={onQuickSetupClose} size="xl">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>
            <HStack spacing={2}>
              <StarIcon color="purple.500" />
              <Text>新手一键设置</Text>
            </HStack>
          </ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <VStack spacing={4} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle fontSize="sm" mb={1}>
                    快速配置常用交易对
                  </AlertTitle>
                  <AlertDescription fontSize="sm">
                    系统会根据价格和资金自动计算最优参数
                  </AlertDescription>
                </Box>
              </Alert>
              
              {/* 显示当前交易所 */}
              <Box
                p={3}
                bg="purple.50"
                borderRadius="md"
                border="1px solid"
                borderColor="purple.200"
              >
                <HStack spacing={2}>
                  <Text fontSize="sm" fontWeight="600" color="purple.700">
                    目标交易所：
                  </Text>
                  <Badge colorScheme="purple" fontSize="sm" px={2} py={1}>
                    {exchangeNames[config.app?.current_exchange || 'binance'] || (config.app?.current_exchange || 'binance').toUpperCase()}
                  </Badge>
                  <Text fontSize="xs" color="gray.600" ml="auto">
                    交易对将添加到该交易所
                  </Text>
                </HStack>
              </Box>
              
              <FormControl>
                <FormLabel>总资金 (USDT)</FormLabel>
                <NumberInput
                  value={quickSetupTotalCapital}
                  onChange={(_, v) => setQuickSetupTotalCapital(v)}
                  min={100}
                  precision={2}
                >
                  <NumberInputField />
                </NumberInput>
                <Text fontSize="xs" color="gray.500" mt={1}>
                  资金将平均分配给选中的交易对
                </Text>
              </FormControl>
              
              <Divider />
              
              <FormControl>
                <FormLabel>选择交易对</FormLabel>
                <CheckboxGroup
                  value={quickSetupSelectedSymbols}
                  onChange={(values) => setQuickSetupSelectedSymbols(values as string[])}
                >
                  <SimpleGrid columns={2} spacing={3}>
                    {getCommonSymbols(config.app?.current_exchange || '').map((symbol) => {
                      const currentExchange = config.app?.current_exchange || 'binance'
                      const price = quickSetupSymbolPrices[symbol]
                      // 计算每个交易对的资金分配（基于当前选中的数量）
                      const selectedCount = quickSetupSelectedSymbols.length || 1
                      const capitalPerSymbol = quickSetupTotalCapital / selectedCount
                      const rec = calculateRecommendations(symbol, price || null, capitalPerSymbol)
                      const isSelected = quickSetupSelectedSymbols.includes(symbol)
                      // 检查是否已存在
                      const alreadyExists = symbols.some(
                        s => s.symbol === symbol && (s.exchange || currentExchange) === currentExchange
                      )
                      
                      return (
                        <Box
                          key={symbol}
                          p={3}
                          border="1px solid"
                          borderColor={
                            alreadyExists 
                              ? 'orange.300' 
                              : isSelected 
                                ? 'purple.300' 
                                : 'gray.200'
                          }
                          borderRadius="md"
                          bg={
                            alreadyExists 
                              ? 'orange.50' 
                              : isSelected 
                                ? 'purple.50' 
                                : 'white'
                          }
                          _hover={{ borderColor: alreadyExists ? 'orange.400' : 'purple.300', bg: alreadyExists ? 'orange.50' : 'purple.50' }}
                          transition="all 0.2s"
                          position="relative"
                        >
                          {alreadyExists && (
                            <Badge
                              position="absolute"
                              top={2}
                              right={2}
                              colorScheme="orange"
                              fontSize="xs"
                            >
                              已存在
                            </Badge>
                          )}
                          <Checkbox value={symbol} size="md" isDisabled={alreadyExists}>
                            <VStack align="start" spacing={1}>
                              <HStack spacing={2}>
                                <Text fontWeight="600">{symbol}</Text>
                                {alreadyExists && (
                                  <Tooltip label="该交易对在当前交易所已存在，请先删除后再添加">
                                    <InfoIcon color="orange.500" boxSize={3} />
                                  </Tooltip>
                                )}
                              </HStack>
                              {price ? (
                                <Text fontSize="xs" color="gray.500">
                                  当前价格: {price.toFixed(2)} USDT
                                </Text>
                              ) : (
                                <Text fontSize="xs" color="gray.400">
                                  价格加载中...
                                </Text>
                              )}
                              {isSelected && !alreadyExists && (
                                <VStack align="start" spacing={0} mt={1}>
                                  <Text fontSize="xs" color="purple.600" fontWeight="500">
                                    分配资金: {capitalPerSymbol.toFixed(2)} USDT
                                  </Text>
                                  <Text fontSize="xs" color="blue.500">
                                    建议参数: 间隔 {rec.price_interval.suggested.toFixed(2)} | 
                                    订单 {rec.order_quantity.suggested} | 
                                    窗口 {rec.buy_window_size.suggested}
                                  </Text>
                                </VStack>
                              )}
                              {alreadyExists && (
                                <Text fontSize="xs" color="orange.600" fontWeight="500" mt={1}>
                                  该交易对已存在，无法重复添加
                                </Text>
                              )}
                            </VStack>
                          </Checkbox>
                        </Box>
                      )
                    })}
                  </SimpleGrid>
                </CheckboxGroup>
                {quickSetupSelectedSymbols.some(symbol => {
                  const currentExchange = config.app?.current_exchange || 'binance'
                  return symbols.some(
                    s => s.symbol === symbol && (s.exchange || currentExchange) === currentExchange
                  )
                }) && (
                  <Alert status="warning" borderRadius="md" mt={2}>
                    <AlertIcon />
                    <AlertDescription fontSize="sm">
                      选中的交易对中包含已存在的配置，这些交易对将被跳过。请先删除现有配置后再添加。
                    </AlertDescription>
                  </Alert>
                )}
              </FormControl>
              
              <Divider />
              
              {/* 计算公式说明 */}
              <Accordion allowToggle>
                <AccordionItem>
                  <AccordionButton>
                    <Box flex="1" textAlign="left">
                      <Text fontWeight="600">📐 基于价格的计算公式说明</Text>
                    </Box>
                    <AccordionIcon />
                  </AccordionButton>
                  <AccordionPanel pb={4}>
                    <VStack align="start" spacing={3} fontSize="sm">
                      <Box>
                        <Text fontWeight="600" mb={1}>1. 价格间隔计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          价格间隔 = 当前价格 × (0.1% ~ 1%)<br/>
                          建议值 = 当前价格 × 0.5%
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          价格间隔太小会导致频繁交易，太大会错过交易机会
                        </Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>2. 订单金额计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          根据总资金量分级：<br/>
                          • 资金 &lt; 500 USDT: 建议 20-30 USDT<br/>
                          • 资金 500-2000 USDT: 建议 50-100 USDT<br/>
                          • 资金 2000-10000 USDT: 建议 100-200 USDT<br/>
                          • 资金 &gt; 10000 USDT: 建议 200+ USDT
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          单笔订单建议不超过总资金的 5%
                        </Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>3. 网格价格计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          买入网格价格 = 当前价格 - (网格层数 × 价格间隔)<br/>
                          卖出网格价格 = 当前价格 + (网格层数 × 价格间隔)
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          例如：当前价格 50000，间隔 100，买入窗口 10 层<br/>
                          最低买入价 = 50000 - (10 × 100) = 49000
                        </Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>4. 窗口大小计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          建议网格数 = 总资金 ÷ (订单金额 × 2)<br/>
                          窗口大小 = max(5, min(20, 建议网格数 ÷ 2))
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          确保有足够资金覆盖所有网格（买单和卖单各一半）
                        </Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>5. 最小订单价值计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          最小订单价值 = 订单金额 × 50% ~ 100%<br/>
                          建议值 = 订单金额 × 50%
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          小于此值的订单不会挂单，避免过小的订单产生过多手续费
                        </Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>6. 单笔订单数量计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          订单数量 = 订单金额 ÷ 当前价格<br/>
                          例如：订单金额 30 USDT，价格 3000 USDT<br/>
                          数量 = 30 ÷ 3000 = 0.01 BTC
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          订单金额固定，价格越低买入数量越多
                        </Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>7. 总资金需求计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          总资金需求 = (买单窗口 + 卖单窗口) × 订单金额<br/>
                          例如：买单 10 层，卖单 10 层，订单 30 USDT<br/>
                          需求 = (10 + 10) × 30 = 600 USDT
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          确保分配的资金足够覆盖所有网格
                        </Text>
                      </Box>
                    </VStack>
                  </AccordionPanel>
                </AccordionItem>
              </Accordion>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" mr={3} onClick={onQuickSetupClose}>
              取消
            </Button>
            <Button
              colorScheme="purple"
              onClick={handleQuickSetup}
              isLoading={quickSetupLoading}
              isDisabled={quickSetupSelectedSymbols.length === 0}
            >
              一键添加 {quickSetupSelectedSymbols.length > 0 ? `(${quickSetupSelectedSymbols.length}个)` : ''}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </Box>
  )
}

export default SymbolManager
