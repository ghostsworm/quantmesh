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
  AlertDescription,
  Divider,
} from '@chakra-ui/react'
import { AddIcon, DeleteIcon, EditIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { Config, SymbolConfig } from '../services/config'
import { getExchangeSymbols } from '../services/setup'

interface SymbolManagerProps {
  config: Config
  onUpdate: (symbols: SymbolConfig[]) => void
}

const SymbolManager: React.FC<SymbolManagerProps> = ({ config, onUpdate }) => {
  const { t } = useTranslation()
  const toast = useToast()
  const { isOpen: isAddOpen, onOpen: onAddOpen, onClose: onAddClose } = useDisclosure()
  const { isOpen: isEditOpen, onOpen: onEditOpen, onClose: onEditClose } = useDisclosure()
  
  const [symbols, setSymbols] = useState<SymbolConfig[]>(config.trading?.symbols || [])
  const [editingIndex, setEditingIndex] = useState<number>(-1)
  const [availableSymbols, setAvailableSymbols] = useState<string[]>([])
  const [loadingSymbols, setLoadingSymbols] = useState(false)
  
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

  useEffect(() => {
    setSymbols(config.trading?.symbols || [])
  }, [config])

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
    onAddOpen()
  }

  const handleEdit = (index: number) => {
    setFormData(symbols[index])
    setEditingIndex(index)
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

    const newSymbols = [...symbols]
    if (editingIndex >= 0) {
      newSymbols[editingIndex] = { ...formData }
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
      newSymbols.push({ ...formData })
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

  return (
    <Box>
      <VStack spacing={4} align="stretch">
        <HStack justify="space-between">
          <Text fontSize="sm" fontWeight="600" color="gray.600">
            当前配置了 {symbols.length} 个交易对
          </Text>
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
                    onChange={(e) => setFormData({ ...formData, symbol: e.target.value })}
                    placeholder="选择交易对"
                  >
                    {availableSymbols.map((sym) => (
                      <option key={sym} value={sym}>{sym}</option>
                    ))}
                  </Select>
                ) : (
                  <Input
                    value={formData.symbol}
                    onChange={(e) => setFormData({ ...formData, symbol: e.target.value.toUpperCase() })}
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
              </FormControl>

              <Divider />

              <FormControl>
                <FormLabel>价格间隔 (USDT)</FormLabel>
                <NumberInput
                  value={formData.price_interval}
                  onChange={(_, v) => setFormData({ ...formData, price_interval: v })}
                  min={0.0001}
                  precision={6}
                >
                  <NumberInputField />
                </NumberInput>
              </FormControl>

              <FormControl>
                <FormLabel>订单金额 (USDT)</FormLabel>
                <NumberInput
                  value={formData.order_quantity}
                  onChange={(_, v) => setFormData({ ...formData, order_quantity: v })}
                  min={1}
                  precision={2}
                >
                  <NumberInputField />
                </NumberInput>
              </FormControl>

              <HStack spacing={4} width="100%">
                <FormControl>
                  <FormLabel>买单窗口大小</FormLabel>
                  <NumberInput
                    value={formData.buy_window_size}
                    onChange={(_, v) => setFormData({ ...formData, buy_window_size: v })}
                    min={1}
                  >
                    <NumberInputField />
                  </NumberInput>
                </FormControl>

                <FormControl>
                  <FormLabel>卖单窗口大小</FormLabel>
                  <NumberInput
                    value={formData.sell_window_size}
                    onChange={(_, v) => setFormData({ ...formData, sell_window_size: v })}
                    min={1}
                  >
                    <NumberInputField />
                  </NumberInput>
                </FormControl>
              </HStack>

              <FormControl>
                <FormLabel>最小订单价值 (USDT)</FormLabel>
                <NumberInput
                  value={formData.min_order_value || 20}
                  onChange={(_, v) => setFormData({ ...formData, min_order_value: v })}
                  min={1}
                  precision={2}
                >
                  <NumberInputField />
                </NumberInput>
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
    </Box>
  )
}

export default SymbolManager
