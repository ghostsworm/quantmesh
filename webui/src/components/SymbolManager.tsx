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
    binance: t('exchanges.binance'),
    bitget: t('exchanges.bitget'),
    bybit: t('exchanges.bybit'),
    gate: t('exchanges.gate'),
    edgex: t('exchanges.edgex'),
    bit: t('exchanges.bit'),
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
          title: t('symbolManager.cannotLoadPairs'),
          description: t('symbolManager.configureApiKeyFirst', { exchange: exchangeNames[exchange] || exchange }),
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
        title: t('symbolManager.loadPairsFailed'),
        description: error.message || t('symbolManager.checkApiConfig'),
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
      profit_spread: 0,
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
      title: t('symbolManager.deleteSuccess'),
      status: 'success',
      duration: 2000,
    })
  }

  const handleSave = () => {
    if (!formData.symbol) {
      toast({
        title: t('symbolManager.pleaseSelectPair'),
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
          title: t('symbolManager.pairExists'),
          description: t('symbolManager.pairExistsDesc', { exchange: formData.exchange || config.app?.current_exchange, symbol: formData.symbol }),
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
      title: editingIndex >= 0 ? t('symbolManager.updateSuccess') : t('symbolManager.addSuccess'),
      status: 'success',
      duration: 2000,
    })
  }
  
  // 新手一键設置处理函數
  const handleQuickSetup = async () => {
    if (quickSetupSelectedSymbols.length === 0) {
      toast({
        title: t('symbolManager.selectAtLeastOnePair'),
        status: 'warning',
        duration: 2000,
      })
      return
    }
    
    if (quickSetupTotalCapital <= 0) {
      toast({
        title: t('symbolManager.enterTotalCapital'),
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
          title: t('symbolManager.allSelectedExist'),
          description: t('symbolManager.allSelectedExistDesc', { exchange: exchangeName, symbols: existingSymbols.join(', ') }),
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
          title: t('symbolManager.someExist'),
          description: t('symbolManager.someExistDesc', { symbols: existingSymbols.join(', '), count: newSymbols.length }),
          status: 'info',
          duration: 4000,
          isClosable: true,
        })
      }
      
      const updatedSymbols = [...symbols, ...newSymbols]
      setSymbols(updatedSymbols)
      onUpdate(updatedSymbols)
      
      toast({
        title: t('symbolManager.quickSetupSuccess'),
        description: t('symbolManager.quickSetupSuccessDesc', { count: newSymbols.length }),
        status: 'success',
        duration: 3000,
      })
      
      onQuickSetupClose()
      setQuickSetupSelectedSymbols([])
    } catch (error: any) {
      toast({
        title: t('symbolManager.quickSetupFailed'),
        description: error.message || t('symbolManager.tryLater'),
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
              <Tab>{t('common.all')} ({symbols.length})</Tab>
              <Tab>{t('symbolManager.spot')} ({symbols.filter((s) => s.market_type === 'spot').length})</Tab>
              <Tab>{t('symbolManager.futures')} ({symbols.filter((s) => (s.market_type || 'futures') === 'futures').length})</Tab>
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
              {t('symbolManager.quickSetup')}
            </Button>
            <Button
              leftIcon={<AddIcon />}
              colorScheme="blue"
              size="sm"
              onClick={handleAdd}
              borderRadius="md"
            >
              {t('symbolManager.addPair')}
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
                ? t('symbolManager.noPairs')
                : t('symbolManager.noTabPairs', { type: marketTypeTab === 'spot' ? t('symbolManager.spot') : marketTypeTab === 'futures' ? t('symbolManager.futures') : '' })}
            </AlertDescription>
          </Alert>
        ) : (
          <TableContainer>
            <Table variant="simple" size="sm">
              <Thead>
                <Tr>
                  <Th>{t('symbolManager.exchange')}</Th>
                  <Th>{t('symbolManager.market')}</Th>
                  <Th>{t('symbolManager.symbol')}</Th>
                  <Th>{t('configuration.direction')}</Th>
                  <Th>{t('symbolManager.priceInterval')}</Th>
                  <Th>{t('symbolManager.orderAmount')}</Th>
                  <Th>{t('symbolManager.buyWindow')}</Th>
                  <Th>{t('symbolManager.sellWindow')}</Th>
                  <Th>{t('symbolManager.actions')}</Th>
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
                          {sym.market_type === 'spot' ? `🟢 ${t('symbolManager.spot')}` : `📈 ${t('symbolManager.futures')}`}
                        </Badge>
                      </Td>
                      <Td fontWeight="600">{sym.symbol}</Td>
                      <Td>
                        <Badge colorScheme={(sym.direction === 'SHORT' ? 'orange' : 'green') as any} fontSize="xs">
                          {sym.direction === 'SHORT' ? t('configuration.directionShort') : t('configuration.directionLong')}
                        </Badge>
                      </Td>
                      <Td>{sym.price_interval}</Td>
                      <Td>{sym.order_quantity}</Td>
                      <Td>{sym.buy_window_size}</Td>
                      <Td>{sym.sell_window_size}</Td>
                      <Td>
                        <HStack spacing={2}>
                          <IconButton
                            aria-label={t('symbolManager.edit')}
                            icon={<EditIcon />}
                            size="xs"
                            colorScheme="blue"
                            variant="ghost"
                            onClick={() => handleEdit(originalIndex)}
                          />
                          <IconButton
                            aria-label={t('symbolManager.delete')}
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
          <ModalHeader>{editingIndex >= 0 ? t('symbolManager.editSymbol') : t('symbolManager.addSymbol')}</ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <VStack spacing={4}>
              <FormControl isRequired>
                <FormLabel>{t('symbolManager.exchange')}</FormLabel>
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
                  <option value="">{t('symbolManager.useDefaultExchange', { exchange: exchangeNames[config.app?.current_exchange || ''] || config.app?.current_exchange })}</option>
                  {exchanges.map((ex) => (
                    <option key={ex} value={ex}>{exchangeNames[ex] || ex}</option>
                  ))}
                </Select>
              </FormControl>

              <FormControl>
                <FormLabel>{t('symbolManager.marketType')}</FormLabel>
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
                  <option value="futures">{t('symbolManager.futures')}</option>
                  <option value="spot">{t('symbolManager.spot')}</option>
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
                <FormLabel>{t('symbolManager.symbol')}</FormLabel>
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
                    placeholder={t('symbolManager.selectPairPlaceholder')}
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
                    placeholder={t('symbolManager.inputPairPlaceholder')}
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
                    {availableSymbols.length > 0 ? t('symbolManager.refreshPairList') : t('symbolManager.loadPairListFromExchange', { type: formData.market_type === 'spot' ? t('symbolManager.spot') : t('symbolManager.futures') })}
                  </Button>
                )}
                {formData.symbol && currentPrice && (
                  <Text fontSize="xs" color="gray.500" mt={1}>
                    {t('symbolManager.currentPrice')} <Code>{currentPrice.toFixed(2)} USDT</Code>
                  </Text>
                )}
              </FormControl>

              <FormControl>
                <FormLabel>{t('symbolManager.allocateCapital')}</FormLabel>
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
                            title: t('symbolManager.selectPairFirst'),
                            description: t('symbolManager.needPairForRecommendation'),
                            status: 'warning',
                            duration: 3000,
                          })
                          return
                        }
                        if (!currentPrice) {
                          toast({
                            title: t('symbolManager.cannotGetPrice'),
                            description: t('symbolManager.ensurePairSelected'),
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
                          title: t('symbolManager.recommendationsApplied'),
                          description: allocatedCapital > 0 
                            ? t('symbolManager.recommendationsAppliedWithCapital')
                            : t('symbolManager.recommendationsAppliedPriceOnly'),
                          status: 'success',
                          duration: 3000,
                        })
                      }}
                    >
                      {t('symbolManager.applyRecommendations')}
                    </Button>
                  )}
                </HStack>
                <Text fontSize="xs" color="gray.500" mt={1}>
                  {t('symbolManager.forCalculatingGridParams')}
                </Text>
              </FormControl>

              <Divider />

              {/* 配置规则說明 */}
              <Alert status="info" borderRadius="md" fontSize="sm">
                <AlertIcon />
                <VStack align="start" spacing={1}>
                  <Text fontWeight="600">{t('symbolManager.configRulesTitle')}</Text>
                  <Text fontSize="xs">{t('symbolManager.rulePriceInterval')}</Text>
                  <Text fontSize="xs">{t('symbolManager.ruleOrderAmount')}</Text>
                  <Text fontSize="xs">{t('symbolManager.ruleWindowSize')}</Text>
                  <Text fontSize="xs">{t('symbolManager.ruleMinOrderValue')}</Text>
                </VStack>
              </Alert>

              <FormControl>
                <FormLabel>
                  <HStack>
                    <Text>{t('symbolManager.priceIntervalLabel')}</Text>
                    <Tooltip label={t('symbolManager.priceIntervalTooltip')}>
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
                      {isInRange ? "✓" : "⚠"} {t('symbolManager.suggestedRange', { min: rec.price_interval.min.toFixed(2), max: rec.price_interval.max.toFixed(2) })}
                      {!isInRange && ` (${t('symbolManager.recommended', { value: rec.price_interval.suggested.toFixed(2) })})`}
                    </Text>
                  )
                })()}
              </FormControl>

              <FormControl>
                <FormLabel>
                  <HStack>
                    <Text>{t('configuration.profitSpread')}</Text>
                    <Tooltip label={t('configuration.profitSpreadHint')}>
                      <InfoIcon boxSize={3} color="gray.400" />
                    </Tooltip>
                  </HStack>
                </FormLabel>
                <NumberInput
                  value={formData.profit_spread ?? 0}
                  onChange={(_, v) => setFormData({ ...formData, profit_spread: v === undefined || v === 0 ? undefined : v })}
                  min={0}
                  precision={6}
                >
                  <NumberInputField placeholder={t('configuration.profitSpreadHint')} />
                </NumberInput>
              </FormControl>

              <FormControl>
                <FormLabel>
                  <HStack>
                    <Text>{t('symbolManager.orderAmountLabel')}</Text>
                    <Tooltip label={t('symbolManager.orderAmountTooltip')}>
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
                      {isReasonable ? "✓" : "⚠"} {t('symbolManager.suggestedByCapital', { value: suggested })}
                      {allocatedCapital > 0 && ` (${t('symbolManager.percentOfCapital', { percent: ((suggested / allocatedCapital) * 100).toFixed(1) })})`}
                    </Text>
                  )
                })()}
              </FormControl>

              <HStack spacing={4} width="100%">
                <FormControl>
                  <FormLabel>
                    <HStack>
                      <Text>{t('symbolManager.buyWindowLabel')}</Text>
                      <Tooltip label={t('symbolManager.buyWindowTooltip')}>
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
                        {isReasonable ? "✓" : "⚠"} {t('symbolManager.suggestedLevels', { value: suggested })}
                        {!isReasonable && ` (${t('symbolManager.maxAffordable', { value: maxAffordable })})`}
                      </Text>
                    )
                  })()}
                </FormControl>

                <FormControl>
                  <FormLabel>
                    <HStack>
                      <Text>{t('symbolManager.sellWindowLabel')}</Text>
                      <Tooltip label={t('symbolManager.sellWindowTooltip')}>
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
                        ✓ {t('symbolManager.suggestedLevels', { value: suggested })}
                      </Text>
                    )
                  })()}
                </FormControl>
              </HStack>

              <FormControl>
                <FormLabel>
                  <HStack>
                    <Text>{t('symbolManager.minOrderValueLabel')}</Text>
                    <Tooltip label={t('symbolManager.minOrderValueTooltip')}>
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
                      {isReasonable ? "✓" : "⚠"} {t('symbolManager.suggestedWithPercent', { value: suggested, percent: ((suggested / formData.order_quantity) * 100).toFixed(0) })}
                    </Text>
                  )
                })()}
              </FormControl>

              <FormControl>
                <FormLabel>
                  <HStack>
                    <Text>{t('symbolManager.positionSafetyCheck')}</Text>
                    <Tooltip label={t('symbolManager.positionSafetyTooltip')}>
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
                  {t('symbolManager.positionSafetyDesc')}
                </Text>
              </FormControl>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" me={3} onClick={editingIndex >= 0 ? onEditClose : onAddClose}>
              {t('common.cancel')}
            </Button>
            <Button colorScheme="blue" onClick={handleSave}>
              {t('common.save')}
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
              <Text>{t('symbolManager.quickSetup')}</Text>
            </HStack>
          </ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <VStack spacing={4} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle fontSize="sm" mb={1}>
                    {t('symbolManager.quickSetupAlertTitle')}
                  </AlertTitle>
                  <AlertDescription fontSize="sm">
                    {t('symbolManager.quickSetupAlertDesc')}
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
                      {t('symbolManager.targetExchange')}
                    </Text>
                    <Badge colorScheme="purple" fontSize="sm" px={2} py={1}>
                      {exchangeNames[config.app?.current_exchange || 'binance'] || (config.app?.current_exchange || 'binance').toUpperCase()}
                    </Badge>
                    <Text fontSize="xs" color="gray.600" ms="auto">
                      {t('symbolManager.pairsWillBeAdded')}
                    </Text>
                  </HStack>
                  <HStack spacing={2} width="100%">
                    <Text fontSize="sm" fontWeight="600" color={quickSetupMarketType === 'spot' ? 'green.700' : 'blue.700'}>
                      {t('symbolManager.marketTypeLabel')}
                    </Text>
                    <Badge colorScheme={quickSetupMarketType === 'spot' ? 'green' : 'blue'} fontSize="sm" px={2} py={1}>
                      {quickSetupMarketType === 'spot' ? `🟢 ${t('symbolManager.spotTrading')}` : `📈 ${t('symbolManager.futuresTrading')}`}
                    </Badge>
                    {quickSetupMarketType === 'spot' && (
                      <Text fontSize="xs" color="green.600" ms="auto">
                        {t('symbolManager.spotLowRisk')}
                      </Text>
                    )}
                  </HStack>
                  {quickSetupMarketType === 'spot' && config.app?.current_exchange && !spotSupportedExchanges.includes(config.app.current_exchange) && (
                    <Alert status="warning" size="sm" borderRadius="md" mt={2}>
                      <AlertIcon boxSize={3} />
                      <AlertDescription fontSize="xs">
                        {t('symbolManager.exchangeNotSupportSpot', { exchange: exchangeNames[config.app.current_exchange] || config.app.current_exchange, supported: spotSupportedExchanges.map(ex => exchangeNames[ex]).filter(Boolean).join(', ') })}
                      </AlertDescription>
                    </Alert>
                  )}
                </VStack>
              </Box>
              
              <FormControl>
                <FormLabel>{t('symbolManager.marketType')}</FormLabel>
                <Select
                  value={quickSetupMarketType}
                  onChange={(e) => setQuickSetupMarketType(e.target.value === 'spot' ? 'spot' : 'futures')}
                >
                  <option value="futures">{t('symbolManager.futures')}</option>
                  <option value="spot">{t('symbolManager.spot')}</option>
                </Select>
              </FormControl>
              
              <FormControl>
                <FormLabel>{t('symbolManager.totalCapital')}</FormLabel>
                <NumberInput
                  value={quickSetupTotalCapital}
                  onChange={(_, v) => setQuickSetupTotalCapital(v)}
                  min={100}
                  precision={2}
                >
                  <NumberInputField />
                </NumberInput>
                <Text fontSize="xs" color="gray.500" mt={1}>
                  {t('symbolManager.capitalDistributed')}
                </Text>
              </FormControl>
              
              <Divider />
              
              <FormControl>
                <FormLabel>{t('symbolManager.selectPairs')}</FormLabel>
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
                              {t('symbolManager.alreadyExists')}
                            </Badge>
                          )}
                          <Checkbox value={symbol} size="md" isDisabled={alreadyExists}>
                            <VStack align="start" spacing={1}>
                              <HStack spacing={2}>
                                <Text fontWeight="600">{symbol}</Text>
                                {alreadyExists && (
                                  <Tooltip label={t('symbolManager.existsDeleteFirst')}>
                                    <InfoIcon color="orange.500" boxSize={3} />
                                  </Tooltip>
                                )}
                              </HStack>
                              {price ? (
                                <Text fontSize="xs" color="gray.500">
                                  {t('symbolManager.currentPrice')} {price.toFixed(2)} USDT
                                </Text>
                              ) : (
                                <Text fontSize="xs" color="gray.400">
                                  {t('symbolManager.priceLoading')}
                                </Text>
                              )}
                              {isSelected && !alreadyExists && (
                                <VStack align="start" spacing={0} mt={1}>
                                  <Text fontSize="xs" color="purple.600" fontWeight="500">
                                    {t('symbolManager.allocatedCapitalLabel', { value: capitalPerSymbol.toFixed(2) })}
                                  </Text>
                                  <Text fontSize="xs" color="blue.500">
                                    {t('symbolManager.suggestedParams', { interval: rec.price_interval.suggested.toFixed(2), order: rec.order_quantity.suggested, window: rec.buy_window_size.suggested })}
                                  </Text>
                                </VStack>
                              )}
                              {alreadyExists && (
                                <Text fontSize="xs" color="orange.600" fontWeight="500" mt={1}>
                                  {t('symbolManager.cannotAddDuplicate')}
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
                      {t('symbolManager.selectedContainExisting')}
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
                      <Text fontWeight="600">{t('symbolManager.calculationFormulas')}</Text>
                    </Box>
                    <AccordionIcon />
                  </AccordionButton>
                  <AccordionPanel pb={4}>
                    <VStack align="start" spacing={3} fontSize="sm">
                      <Box>
                        <Text fontWeight="600" mb={1}>{t('symbolManager.formula1Title')}</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50" whiteSpace="pre-wrap">
                          {t('symbolManager.formula1Code')}
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>{t('symbolManager.formula1Desc')}</Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>{t('symbolManager.formula2Title')}</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50" whiteSpace="pre-wrap">
                          {t('symbolManager.formula2Code')}
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>{t('symbolManager.formula2Desc')}</Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>{t('symbolManager.formula3Title')}</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50" whiteSpace="pre-wrap">
                          {t('symbolManager.formula3Code')}
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>{t('symbolManager.formula3Desc')}</Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>{t('symbolManager.formula4Title')}</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50" whiteSpace="pre-wrap">
                          {t('symbolManager.formula4Code')}
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>{t('symbolManager.formula4Desc')}</Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>{t('symbolManager.formula5Title')}</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50" whiteSpace="pre-wrap">
                          {t('symbolManager.formula5Code')}
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>{t('symbolManager.formula5Desc')}</Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>{t('symbolManager.formula6Title')}</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50" whiteSpace="pre-wrap">
                          {t('symbolManager.formula6Code')}
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>{t('symbolManager.formula6Desc')}</Text>
                      </Box>
                      
                      <Box>
                        <Text fontWeight="600" mb={1}>{t('symbolManager.formula7Title')}</Text>
                        <Code display="block" p={2} borderRadius="md" bg="gray.50" whiteSpace="pre-wrap">
                          {t('symbolManager.formula7Code')}
                        </Code>
                        <Text fontSize="xs" color="gray.600" mt={1}>{t('symbolManager.formula7Desc')}</Text>
                      </Box>
                    </VStack>
                  </AccordionPanel>
                </AccordionItem>
              </Accordion>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" me={3} onClick={onQuickSetupClose}>
              {t('common.cancel')}
            </Button>
            <Button
              colorScheme="purple"
              onClick={handleQuickSetup}
              isLoading={quickSetupLoading}
              isDisabled={quickSetupSelectedSymbols.length === 0}
            >
              {t('symbolManager.quickAddButton')} {quickSetupSelectedSymbols.length > 0 ? `(${quickSetupSelectedSymbols.length})` : ''}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </Box>
  )
}

export default SymbolManager
