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
  Tabs,
  TabList,
  TabPanels,
  Tab,
  TabPanel,
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
  
  // 新手一键設置相关状態
  const [quickSetupMarketType, setQuickSetupMarketType] = useState<'spot' | 'futures'>('futures')
  const [quickSetupSelectedSymbols, setQuickSetupSelectedSymbols] = useState<string[]>([])
  const [quickSetupTotalCapital, setQuickSetupTotalCapital] = useState<number>(1000)
  const [quickSetupLoading, setQuickSetupLoading] = useState(false)
  const [quickSetupSymbolPrices, setQuickSetupSymbolPrices] = useState<Record<string, number>>({})
  
  const [marketTypeTab, setMarketTypeTab] = useState<'all' | 'spot' | 'futures'>('all')
  const [formData, setFormData] = useState<SymbolConfig>({
    exchange: config.app?.current_exchange || '',
    symbol: '',
    market_type: 'futures',
    price_interval: 2,
    order_quantity: 30,
    min_order_value: 20,
    buy_window_size: 10,
    sell_window_size: 10,
    reconcile_interval: 60,
    order_cleanup_threshold: 50,
    cleanup_batch_size: 20,
    margin_lock_duration_seconds: 20,
    position_safety_check: config.trading?.position_safety_check ?? 100,
    direction: 'LONG',
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
  
  // 支援現貨的交易所列表
  const spotSupportedExchanges = ['binance', 'bitget', 'bybit', 'gate', 'okx']
  
  // 常用交易對列表（根據交易所格式不同）
  const getCommonSymbols = (exchange: string): string[] => {
    const currentExchange = exchange || config.app?.current_exchange || 'binance'
    if (currentExchange === 'gate') {
      return ['BTC_USDT', 'ETH_USDT', 'SOL_USDT', 'XRP_USDT', 'DOGE_USDT', 'ADA_USDT', 'MATIC_USDT', 'AVAX_USDT']
    }
    // 其他交易所使用標准格式
    return ['BTCUSDT', 'ETHUSDT', 'SOLUSDT', 'XRPUSDT', 'DOGEUSDT', 'ADAUSDT', 'MATICUSDT', 'AVAXUSDT']
  }

  useEffect(() => {
    setSymbols(config.trading?.symbols || [])
  }, [config])
  
  // 加載常用交易對的價格
  const loadQuickSetupPrices = async () => {
    const currentExchange = config.app?.current_exchange || 'binance'
    const commonSymbols = getCommonSymbols(currentExchange)
    const prices: Record<string, number> = {}
    
    try {
      const response = await getSymbols()
      for (const symbol of commonSymbols) {
        // 尝試匹配交易對（考虑不同交易所可能有不同的格式）
        const symbolInfo = response.symbols.find(
          s => {
            // 標准化比较：移除下划線和大小写差异
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
      console.error('加載價格失败:', error)
      // 即使失败也继续，用戶仍可以手动設置
    }
  }
  
  useEffect(() => {
    if (isQuickSetupOpen) {
      loadQuickSetupPrices()
    }
  }, [isQuickSetupOpen, quickSetupMarketType])

  // 獲取交易對當前價格
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
      // 價格間隔建议：币值的 0.1% - 1%
      const minInterval = price * 0.001 // 0.1%
      const maxInterval = price * 0.01  // 1%
      recommendations.price_interval = {
        min: Math.max(0.0001, minInterval),
        max: maxInterval,
        suggested: Math.max(0.1, Math.min(maxInterval, price * 0.005)), // 建议 0.5%
      }
    }

    if (capital > 0) {
      // 订單金額建议：根據资金量
      if (capital < 500) {
        recommendations.order_quantity.suggested = 20
      } else if (capital < 2000) {
        recommendations.order_quantity.suggested = 50
      } else if (capital < 10000) {
        recommendations.order_quantity.suggested = 100
      } else {
        recommendations.order_quantity.suggested = 200
      }

      // 窗口大小建议：根據资金量和订單金額计算
      // 假設每個格子需要 order_quantity 的资金
      // 建议格子數 = 總资金 / (订單金額 * 2) （買單和賣單各一半）
      const suggestedGrids = Math.floor(capital / (recommendations.order_quantity.suggested * 2))
      recommendations.buy_window_size.suggested = Math.max(5, Math.min(20, Math.floor(suggestedGrids / 2)))
      recommendations.sell_window_size.suggested = Math.max(5, Math.min(20, Math.floor(suggestedGrids / 2)))

      // 最小訂單價值建议：订單金額的 50% - 100%
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
          title: '無法加載交易對',
          description: `请先配置 ${exchangeNames[exchange] || exchange} 的 API Key 和 Secret Key`,
          status: 'warning',
          duration: 3000,
        })
        setAvailableSymbols([])
        return
      }

      const response = await getExchangeSymbols({
        exchange,
        market_type: formData.market_type || 'futures',
        api_key: exchangeConfig.api_key,
        secret_key: exchangeConfig.secret_key,
        passphrase: exchangeConfig.passphrase,
        testnet: exchangeConfig.testnet || false,
      })
      setAvailableSymbols(response.symbols || [])
    } catch (error: any) {
      toast({
        title: '加載交易對失败',
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
      market_type: 'futures',
      direction: 'LONG',
      price_interval: 2,
      order_quantity: 30,
      min_order_value: 20,
      buy_window_size: 10,
      sell_window_size: 10,
      reconcile_interval: 60,
      order_cleanup_threshold: 50,
      cleanup_batch_size: 20,
      margin_lock_duration_seconds: 20,
      position_safety_check: config.trading?.position_safety_check ?? 100,
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
    // 加載價格和资金信息
    if (symbolData.symbol) {
      const exchange = symbolData.exchange || config.app?.current_exchange || ''
      if (exchange) {
        fetchSymbolPrice(symbolData.symbol, exchange)
      }
    }
    // 如果有分配资金配置，加載它
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
      title: '刪除成功',
      status: 'success',
      duration: 2000,
    })
  }

  const handleSave = () => {
    if (!formData.symbol) {
      toast({
        title: '请選擇交易對',
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
      // 检查是否已存在（同一交易所+交易對+市場類型視為重複）
      const mt = formData.market_type || 'futures'
      if (newSymbols.some(s => s.symbol === formData.symbol && (s.exchange || '') === (formData.exchange || '') && (s.market_type || 'futures') === mt)) {
        toast({
          title: '交易對已存在',
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
  
  // 新手一键設置处理函數
  const handleQuickSetup = async () => {
    if (quickSetupSelectedSymbols.length === 0) {
      toast({
        title: '请至少选擇一個交易對',
        status: 'warning',
        duration: 2000,
      })
      return
    }
    
    if (quickSetupTotalCapital <= 0) {
      toast({
        title: '请输入總资金',
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
        if (symbols.some(s => s.symbol === symbol && (s.exchange || currentExchange) === currentExchange && (s.market_type || 'futures') === quickSetupMarketType)) {
          existingSymbols.push(symbol)
          continue
        }
        
        const price = quickSetupSymbolPrices[symbol] || null
        const rec = calculateRecommendations(symbol, price, capitalPerSymbol)
        
        const symbolConfig: SymbolConfig = {
          exchange: currentExchange,
          symbol,
          market_type: quickSetupMarketType,
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
          position_safety_check: config.trading?.position_safety_check ?? 100,
        }
        
        newSymbols.push(symbolConfig)
      }
      
      if (newSymbols.length === 0) {
        // 所有交易對都已存在，提示用戶刪除
        toast({
          title: '所有选中的交易對都已存在',
          description: `以下交易對在 ${exchangeName} 已存在：${existingSymbols.join(', ')}。请先刪除現有配置后再添加。`,
          status: 'warning',
          duration: 5000,
          isClosable: true,
        })
        setQuickSetupLoading(false)
        return
      }
      
      // 如果有部分交易對已存在，也提示
      if (existingSymbols.length > 0) {
        toast({
          title: '部分交易對已存在',
          description: `以下交易對已跳過：${existingSymbols.join(', ')}。已成功添加 ${newSymbols.length} 個新交易對。`,
          status: 'info',
          duration: 4000,
          isClosable: true,
        })
      }
      
      const updatedSymbols = [...symbols, ...newSymbols]
      setSymbols(updatedSymbols)
      onUpdate(updatedSymbols)
      
      toast({
        title: '一键設置成功',
        description: `已添加 ${newSymbols.length} 個交易對配置`,
        status: 'success',
        duration: 3000,
      })
      
      onQuickSetupClose()
      setQuickSetupSelectedSymbols([])
    } catch (error: any) {
      toast({
        title: '一键設置失败',
        description: error.message || '请稍后重試',
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
        <HStack justify="space-between" wrap="wrap" spacing={2}>
          <Tabs
            index={marketTypeTab === 'all' ? 0 : marketTypeTab === 'spot' ? 1 : 2}
            onChange={(i) => setMarketTypeTab(i === 0 ? 'all' : i === 1 ? 'spot' : 'futures')}
            variant="enclosed"
            size="sm"
          >
            <TabList>
              <Tab>全部 ({symbols.length})</Tab>
              <Tab>現貨 ({symbols.filter((s) => s.market_type === 'spot').length})</Tab>
              <Tab>合約 ({symbols.filter((s) => (s.market_type || 'futures') === 'futures').length})</Tab>
            </TabList>
          </Tabs>
          <HStack spacing={2}>
            <Button
              leftIcon={<StarIcon />}
              colorScheme="purple"
              size="sm"
              onClick={onQuickSetupOpen}
              borderRadius="md"
              variant="outline"
            >
              新手一键設置
            </Button>
            <Button
              leftIcon={<AddIcon />}
              colorScheme="blue"
              size="sm"
              onClick={handleAdd}
              borderRadius="md"
            >
              添加交易對
            </Button>
          </HStack>
        </HStack>

        {(() => {
          const filtered =
            marketTypeTab === 'all'
              ? symbols
              : marketTypeTab === 'spot'
                ? symbols.filter((s) => s.market_type === 'spot')
                : symbols.filter((s) => (s.market_type || 'futures') === 'futures')
          return filtered.length === 0 ? (
          <Alert status="info" borderRadius="md">
            <AlertIcon />
            <AlertDescription>
              {symbols.length === 0
                ? '还没有配置任何交易對。点击"添加交易對"按钮开始配置。'
                : `當前 Tab 下没有${marketTypeTab === 'spot' ? '現貨' : marketTypeTab === 'futures' ? '合約' : ''}交易對。`}
            </AlertDescription>
          </Alert>
        ) : (
          <TableContainer>
            <Table variant="simple" size="sm">
              <Thead>
                <Tr>
                  <Th>交易所</Th>
                  <Th>市场</Th>
                  <Th>交易對</Th>
                  <Th>方向</Th>
                  <Th>價格間隔</Th>
                  <Th>订單金額</Th>
                  <Th>買單窗口</Th>
                  <Th>賣單視窗</Th>
                  <Th>操作</Th>
                </Tr>
              </Thead>
              <Tbody>
                {filtered.map((sym, index) => {
                  // 找到原始數组中的索引用於编辑和刪除
                  const originalIndex = symbols.findIndex(s => 
                    s.symbol === sym.symbol && 
                    (s.exchange || config.app?.current_exchange) === (sym.exchange || config.app?.current_exchange) &&
                    (s.market_type || 'futures') === (sym.market_type || 'futures')
                  )
                  
                  return (
                    <Tr key={`${sym.exchange || config.app?.current_exchange}:${sym.symbol}:${sym.market_type || 'futures'}`}>
                      <Td>
                        <Badge colorScheme="blue">
                          {sym.exchange ? exchangeNames[sym.exchange] || sym.exchange : exchangeNames[config.app?.current_exchange || ''] || config.app?.current_exchange}
                        </Badge>
                      </Td>
                      <Td>
                        <Badge 
                          colorScheme={sym.market_type === 'spot' ? 'green' : 'purple'}
                          variant="solid"
                          size="sm"
                          borderRadius="full"
                          px={3}
                        >
                          {sym.market_type === 'spot' ? '🟢 現貨' : '📈 合約'}
                        </Badge>
                      </Td>
                      <Td fontWeight="600">{sym.symbol}</Td>
                      <Td>
                        <Badge colorScheme={(sym.direction === 'SHORT' ? 'orange' : 'green') as any} fontSize="xs">
                          {(sym.direction === 'SHORT' ? '做空' : '做多')}
                        </Badge>
                      </Td>
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
                            onClick={() => handleEdit(originalIndex)}
                          />
                          <IconButton
                            aria-label="刪除"
                            icon={<DeleteIcon />}
                            size="xs"
                            colorScheme="red"
                            variant="ghost"
                            onClick={() => handleDelete(originalIndex)}
                          />
                        </HStack>
                      </Td>
                    </Tr>
                  )
                })}
              </Tbody>
            </Table>
          </TableContainer>
        )
        })()}
      </VStack>

      {/* 添加/编辑模態框 */}
      <Modal isOpen={isAddOpen || isEditOpen} onClose={editingIndex >= 0 ? onEditClose : onAddClose} size="xl">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{editingIndex >= 0 ? '编辑交易對' : '添加交易對'}</ModalHeader>
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
                      // 如果已有交易對，重新加載價格
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

              <FormControl>
                <FormLabel>市場類型</FormLabel>
                <Select
                  value={formData.market_type || 'futures'}
                  onChange={(e) => {
                    const market_type = (e.target.value === 'spot' ? 'spot' : 'futures') as 'spot' | 'futures'
                    setFormData({ ...formData, market_type })
                    if (formData.exchange) {
                      loadAvailableSymbols(formData.exchange)
                    }
                  }}
                >
                  <option value="futures">合約</option>
                  <option value="spot">現貨</option>
                </Select>
              </FormControl>

              <FormControl>
                <FormLabel>{t('configuration.direction')}</FormLabel>
                <Select
                  value={formData.direction || 'LONG'}
                  onChange={(e) => setFormData({ ...formData, direction: e.target.value as 'LONG' | 'SHORT' })}
                >
                  <option value="LONG">{t('configuration.directionLong')}</option>
                  <option value="SHORT">{t('configuration.directionShort')}</option>
                </Select>
              </FormControl>

              <FormControl isRequired>
                <FormLabel>交易對</FormLabel>
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
                    placeholder="選擇交易對"
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
                    {availableSymbols.length > 0 ? '刷新交易對列表' : `從交易所加載${formData.market_type === 'spot' ? '現貨' : '合約'}交易對列表`}
                  </Button>
                )}
                {formData.symbol && currentPrice && (
                  <Text fontSize="xs" color="gray.500" mt={1}>
                    當前價格: <Code>{currentPrice.toFixed(2)} USDT</Code>
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
                            title: '请先選擇交易對',
                            description: '需要交易對信息才能计算建议值',
                            status: 'warning',
                            duration: 3000,
                          })
                          return
                        }
                        if (!currentPrice) {
                          toast({
                            title: '無法獲取當前價格',
                            description: '请确保已選擇交易對並等待價格加載',
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
                            ? '已根據當前價格和分配资金自动填充所有建议值'
                            : '已根據當前價格填充價格間隔建议值（请設置分配资金以獲取完整建议）',
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
                  用於计算建议的网格参數
                </Text>
              </FormControl>

              <Divider />

              {/* 配置规则說明 */}
              <Alert status="info" borderRadius="md" fontSize="sm">
                <AlertIcon />
                <VStack align="start" spacing={1}>
                  <Text fontWeight="600">配置规则說明</Text>
                  <Text fontSize="xs">
                    • <strong>價格間隔</strong>: 建议為币值的 0.1% - 1%，太小會導致频繁交易，太大會錯過机會
                  </Text>
                  <Text fontSize="xs">
                    • <strong>订單金額</strong>: 根據分配资金量設置，建议單笔订單不超過總资金的 5%
                  </Text>
                  <Text fontSize="xs">
                    • <strong>窗口大小</strong>: 根據资金量和订單金額计算，确保有足够资金覆盖所有网格
                  </Text>
                  <Text fontSize="xs">
                    • <strong>最小訂單價值</strong>: 建议不小於订單金額的 50%，避免過小的订單
                  </Text>
                </VStack>
              </Alert>

              <FormControl>
                <FormLabel>
                  <HStack>
                    <Text>價格間隔 (USDT)</Text>
                    <Tooltip label="建议為币值的 0.1% - 1%。如果已選擇交易對，會根據當前價格自动计算建议值。">
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
                      {isInRange ? "✓" : "⚠"} 建议範圍: {rec.price_interval.min.toFixed(2)} - {rec.price_interval.max.toFixed(2)} USDT
                      {!isInRange && ` (推荐: ${rec.price_interval.suggested.toFixed(2)} USDT)`}
                    </Text>
                  )
                })()}
              </FormControl>

              <FormControl>
                <FormLabel>
                  <HStack>
                    <Text>订單金額 (USDT)</Text>
                    <Tooltip label="每單購買金額。建议根據分配资金量設置：小額资金(500以下)建议20-30，中等资金(500-2000)建议50-100，大額资金(2000+)建议100-200。">
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
                      {isReasonable ? "✓" : "⚠"} 根據资金量建议: {suggested} USDT
                      {allocatedCapital > 0 && ` (约占總资金 ${((suggested / allocatedCapital) * 100).toFixed(1)}%)`}
                    </Text>
                  )
                })()}
              </FormControl>

              <HStack spacing={4} width="100%">
                <FormControl>
                  <FormLabel>
                    <HStack>
                      <Text>買單窗口大小</Text>
                      <Tooltip label="買單网格层數。建议根據资金量和订單金額计算，确保有足够资金覆盖所有网格。">
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
                      <Text>賣單視窗大小</Text>
                      <Tooltip label="賣單网格层數。建议與買單窗口大小相同或相近。">
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
                    <Text>最小訂單價值 (USDT)</Text>
                    <Tooltip label="小於此值的订單不會挂單。建议不小於订單金額的 50%。">
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
                      {isReasonable ? "✓" : "⚠"} 建议: {suggested} USDT (订單金額的 {((suggested / formData.order_quantity) * 100).toFixed(0)}%)
                    </Text>
                  )
                })()}
              </FormControl>

              <FormControl>
                <FormLabel>
                  <HStack>
                    <Text>持倉安全性檢查（最少倉數）</Text>
                    <Tooltip label="啟動前檢查：賬戶餘額至少能向下購買並持有該數量倉位，否則拒絕啟動。餘額不足時可調低此值（如 30），或補充保證金。">
                      <InfoIcon boxSize={3} color="gray.400" />
                    </Tooltip>
                  </HStack>
                </FormLabel>
                <NumberInput
                  value={formData.position_safety_check ?? 100}
                  onChange={(_, v) => setFormData({ ...formData, position_safety_check: v })}
                  min={1}
                >
                  <NumberInputField />
                </NumberInput>
                <Text fontSize="xs" color="gray.500" mt={1}>
                  預設 100；當前最大可持有倉數由「餘額×槓桿÷每筆金額」決定
                </Text>
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
      
      {/* 新手一键設置模態框 */}
      <Modal isOpen={isQuickSetupOpen} onClose={onQuickSetupClose} size="xl">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>
            <HStack spacing={2}>
              <StarIcon color="purple.500" />
              <Text>新手一键設置</Text>
            </HStack>
          </ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <VStack spacing={4} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle fontSize="sm" mb={1}>
                    快速配置常用交易對
                  </AlertTitle>
                  <AlertDescription fontSize="sm">
                    系统會根據價格和资金自动计算最优参數
                  </AlertDescription>
                </Box>
              </Alert>
              
              {/* 显示當前交易所 */}
              <Box
                p={3}
                bg="purple.50"
                borderRadius="md"
                border="1px solid"
                borderColor="purple.200"
              >
                <VStack spacing={2} align="start">
                  <HStack spacing={2} width="100%">
                    <Text fontSize="sm" fontWeight="600" color="purple.700">
                      目標交易所：
                    </Text>
                    <Badge colorScheme="purple" fontSize="sm" px={2} py={1}>
                      {exchangeNames[config.app?.current_exchange || 'binance'] || (config.app?.current_exchange || 'binance').toUpperCase()}
                    </Badge>
                    <Text fontSize="xs" color="gray.600" ml="auto">
                      交易對將添加到該交易所
                    </Text>
                  </HStack>
                  <HStack spacing={2} width="100%">
                    <Text fontSize="sm" fontWeight="600" color={quickSetupMarketType === 'spot' ? 'green.700' : 'blue.700'}>
                      市場類型：
                    </Text>
                    <Badge colorScheme={quickSetupMarketType === 'spot' ? 'green' : 'blue'} fontSize="sm" px={2} py={1}>
                      {quickSetupMarketType === 'spot' ? '🟢 現貨交易' : '📈 合約交易'}
                    </Badge>
                    {quickSetupMarketType === 'spot' && (
                      <Text fontSize="xs" color="green.600" ml="auto">
                        現貨無槓桿，風險相對較低
                      </Text>
                    )}
                  </HStack>
                  {quickSetupMarketType === 'spot' && config.app?.current_exchange && !spotSupportedExchanges.includes(config.app.current_exchange) && (
                    <Alert status="warning" size="sm" borderRadius="md" mt={2}>
                      <AlertIcon boxSize={3} />
                      <AlertDescription fontSize="xs">
                        當前交易所 {exchangeNames[config.app.current_exchange] || config.app.current_exchange} 暫時不支援現貨交易。
                        支援現貨的交易所：{spotSupportedExchanges.map(ex => exchangeNames[ex]).filter(Boolean).join('、')}
                      </AlertDescription>
                    </Alert>
                  )}
                </VStack>
              </Box>
              
              <FormControl>
                <FormLabel>市場類型</FormLabel>
                <Select
                  value={quickSetupMarketType}
                  onChange={(e) => setQuickSetupMarketType(e.target.value === 'spot' ? 'spot' : 'futures')}
                >
                  <option value="futures">合約</option>
                  <option value="spot">現貨</option>
                </Select>
              </FormControl>
              
              <FormControl>
                <FormLabel>總资金 (USDT)</FormLabel>
                <NumberInput
                  value={quickSetupTotalCapital}
                  onChange={(_, v) => setQuickSetupTotalCapital(v)}
                  min={100}
                  precision={2}
                >
                  <NumberInputField />
                </NumberInput>
                <Text fontSize="xs" color="gray.500" mt={1}>
                  资金將平均分配给选中的交易對
                </Text>
              </FormControl>
              
              <Divider />
              
              <FormControl>
                <FormLabel>選擇交易對</FormLabel>
                <CheckboxGroup
                  value={quickSetupSelectedSymbols}
                  onChange={(values) => setQuickSetupSelectedSymbols(values as string[])}
                >
                  <SimpleGrid columns={2} spacing={3}>
                    {getCommonSymbols(config.app?.current_exchange || '').map((symbol) => {
                      const currentExchange = config.app?.current_exchange || 'binance'
                      const price = quickSetupSymbolPrices[symbol]
                      // 计算每個交易對的资金分配（基於當前选中的數量）
                      const selectedCount = quickSetupSelectedSymbols.length || 1
                      const capitalPerSymbol = quickSetupTotalCapital / selectedCount
                      const rec = calculateRecommendations(symbol, price || null, capitalPerSymbol)
                      const isSelected = quickSetupSelectedSymbols.includes(symbol)
                      // 检查是否已存在
                      const alreadyExists = symbols.some(
                        s => s.symbol === symbol && (s.exchange || currentExchange) === currentExchange && (s.market_type || 'futures') === quickSetupMarketType
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
                                  <Tooltip label="該交易對在當前交易所已存在，请先刪除后再添加">
                                    <InfoIcon color="orange.500" boxSize={3} />
                                  </Tooltip>
                                )}
                              </HStack>
                              {price ? (
                                <Text fontSize="xs" color="gray.500">
                                  當前價格: {price.toFixed(2)} USDT
                                </Text>
                              ) : (
                                <Text fontSize="xs" color="gray.400">
                                  價格加載中...
                                </Text>
                              )}
                              {isSelected && !alreadyExists && (
                                <VStack align="start" spacing={0} mt={1}>
                                  <Text fontSize="xs" color="purple.600" fontWeight="500">
                                    分配资金: {capitalPerSymbol.toFixed(2)} USDT
                                  </Text>
                                  <Text fontSize="xs" color="blue.500">
                                    建议参數: 间隔 {rec.price_interval.suggested.toFixed(2)} | 
                                    订單 {rec.order_quantity.suggested} | 
                                    窗口 {rec.buy_window_size.suggested}
                                  </Text>
                                </VStack>
                              )}
                              {alreadyExists && (
                                <Text fontSize="xs" color="orange.600" fontWeight="500" mt={1}>
                                  該交易對已存在，無法重複添加
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
                      选中的交易對中包含已存在的配置，这些交易對將被跳過。请先刪除現有配置后再添加。
                    </AlertDescription>
                  </Alert>
                )}
              </FormControl>
              
              <Divider />
              
              {/* 计算公式說明 */}
              <Accordion allowToggle>
                <AccordionItem>
                  <AccordionButton>
                    <Box flex="1" textAlign="left">
                      <Text fontWeight="600">📐 基於價格的计算公式說明</Text>
                    </Box>
                    <AccordionIcon />
                  </AccordionButton>
                  <AccordionPanel pb={4}>
                    <VStack align="start" spacing={3} fontSize="sm">
                      <Box>
                        <Text fontWeight="600" mb={1}>1. 價格間隔计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          價格間隔 = 當前價格 × (0.1% ~ 1%)<br/>
                          建议值 = 當前價格 × 0.5%
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          價格間隔太小會導致频繁交易，太大會錯過交易机會
                        </Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>2. 订單金額计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          根據總资金量分级：<br/>
                          • 资金 &lt; 500 USDT: 建议 20-30 USDT<br/>
                          • 资金 500-2000 USDT: 建议 50-100 USDT<br/>
                          • 资金 2000-10000 USDT: 建议 100-200 USDT<br/>
                          • 资金 &gt; 10000 USDT: 建议 200+ USDT
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          單笔订單建议不超過總资金的 5%
                        </Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>3. 网格價格计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          買入网格價格 = 當前價格 - (网格层數 × 價格間隔)<br/>
                          賣出网格價格 = 當前價格 + (网格层數 × 價格間隔)
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          例如：當前價格 50000，间隔 100，買入窗口 10 层<br/>
                          最低買入價 = 50000 - (10 × 100) = 49000
                        </Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>4. 窗口大小计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          建议网格數 = 總资金 ÷ (订單金額 × 2)<br/>
                          窗口大小 = max(5, min(20, 建议网格數 ÷ 2))
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          确保有足够资金覆盖所有网格（買單和賣單各一半）
                        </Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>5. 最小訂單價值计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          最小訂單價值 = 订單金額 × 50% ~ 100%<br/>
                          建议值 = 订單金額 × 50%
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          小於此值的订單不會挂單，避免過小的订單產生過多手续费
                        </Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>6. 單笔订單數量计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          订單數量 = 订單金額 ÷ 當前價格<br/>
                          例如：订單金額 30 USDT，價格 3000 USDT<br/>
                          數量 = 30 ÷ 3000 = 0.01 BTC
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>
                          订單金額固定，價格越低買入數量越多
                        </Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>7. 總资金需求计算</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50">
                          總资金需求 = (買單窗口 + 賣單視窗) × 订單金額<br/>
                          例如：買單 10 层，賣單 10 层，订單 30 USDT<br/>
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
              一键添加 {quickSetupSelectedSymbols.length > 0 ? `(${quickSetupSelectedSymbols.length}個)` : ''}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </Box>
  )
}

export default SymbolManager
