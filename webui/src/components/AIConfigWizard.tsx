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

interface AIConfigWizardProps {
  isOpen: boolean
  onClose: () => void
  onSuccess?: () => void
  // 从父组件传入的已选交易所和币种
  exchange?: string
  symbols?: string[]
}

const AIConfigWizard: React.FC<AIConfigWizardProps> = ({ 
  isOpen, 
  onClose, 
  onSuccess,
  exchange: propsExchange,
  symbols: propsSymbols 
}) => {
  const { t } = useTranslation()
  const toast = useToast()
  const [step, setStep] = useState<'form' | 'preview' | 'success'>('form')
  const [loading, setLoading] = useState(false)
  
  // Gemini API Key
  const [geminiApiKey, setGeminiApiKey] = useState('')
  const [showApiKey, setShowApiKey] = useState(false)
  
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

  // 使用传入的交易所和币种，如果没有传入则使用默认值
  const exchange = propsExchange || 'binance'
  const symbols = propsSymbols || []

  // 当币种列表变化时，初始化按币种分配的资金
  useEffect(() => {
    if (symbols.length > 0) {
      const defaultCapitalPerSymbol = Math.floor(10000 / symbols.length)
      setSymbolCapitals(symbols.map(symbol => ({
        symbol,
        capital: defaultCapitalPerSymbol
      })))
    }
  }, [symbols])

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
    if (symbols.length === 0) {
      setError('请先在向导中选择交易币种')
      return
    }

    // 验证资金
    if (capitalMode === 'total' && totalCapital <= 0) {
      setError('请输入有效的总资金金额')
      return
    }
    if (capitalMode === 'per_symbol' && totalSymbolCapitals <= 0) {
      setError('请为至少一个币种设置资金')
      return
    }

    setLoading(true)
    setError(null)

    try {
      // 传递 API Key 给后端（后端会临时使用，或者如果配置中有就用配置中的）
      const formData: AIGenerateConfigRequest = {
        exchange,
        symbols,
        capital_mode: capitalMode,
        risk_profile: riskProfile,
        gemini_api_key: geminiApiKey,  // 传递 API Key
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

  return (
    <Modal isOpen={isOpen} onClose={handleClose} size="xl" scrollBehavior="inside">
      <ModalOverlay />
      <ModalContent bg={bg}>
        <ModalHeader>AI 智能配置助手</ModalHeader>
        <ModalCloseButton />
        <ModalBody>
          {step === 'form' && (
            <VStack spacing={4} align="stretch">
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>AI 配置助手</AlertTitle>
                  <AlertDescription fontSize="sm">
                    根据您的资金和风险偏好，AI 将为您生成最优的网格交易参数和资金分配方案
                  </AlertDescription>
                </Box>
              </Alert>

              {error && (
                <Alert status="error" borderRadius="md">
                  <AlertIcon />
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}

              {/* Gemini API Key 输入 */}
              <FormControl isRequired>
                <FormLabel>Gemini API Key</FormLabel>
                <InputGroup>
                  <Input
                    type={showApiKey ? 'text' : 'password'}
                    placeholder="输入您的 Gemini API Key"
                    value={geminiApiKey}
                    onChange={(e) => setGeminiApiKey(e.target.value)}
                  />
                  <InputRightElement>
                    <IconButton
                      aria-label={showApiKey ? '隐藏' : '显示'}
                      icon={showApiKey ? <ViewOffIcon /> : <ViewIcon />}
                      size="sm"
                      variant="ghost"
                      onClick={() => setShowApiKey(!showApiKey)}
                    />
                  </InputRightElement>
                </InputGroup>
                <Text fontSize="xs" color="gray.500" mt={1}>
                  获取 API Key: <a href="https://aistudio.google.com/app/apikey" target="_blank" rel="noopener noreferrer" style={{ color: '#3182ce', textDecoration: 'underline' }}>Google AI Studio</a>
                </Text>
              </FormControl>

              <Divider />

              {/* 显示已选择的交易所和币种（只读） */}
              <Box p={4} bg={infoBg} borderRadius="md">
                <Text fontWeight="bold" mb={2}>已选择的交易配置</Text>
                <HStack mb={2}>
                  <Text fontSize="sm" color="gray.600">交易所:</Text>
                  <Badge colorScheme="blue">{exchangeNames[exchange] || exchange}</Badge>
                </HStack>
                <HStack alignItems="flex-start">
                  <Text fontSize="sm" color="gray.600" flexShrink={0}>交易币种:</Text>
                  <Wrap>
                    {symbols.length > 0 ? (
                      symbols.map(symbol => (
                        <WrapItem key={symbol}>
                          <Badge colorScheme="green">{symbol}</Badge>
                        </WrapItem>
                      ))
                    ) : (
                      <Text fontSize="sm" color="orange.500">未选择币种，请先在向导中选择交易币种</Text>
                    )}
                  </Wrap>
                  </HStack>
              </Box>

              <Divider />

              {/* 资金配置模式选择 */}
              <FormControl>
                <FormLabel>资金配置方式</FormLabel>
                <RadioGroup value={capitalMode} onChange={(value) => setCapitalMode(value as 'total' | 'per_symbol')}>
                  <Stack direction="row" spacing={4}>
                    <Radio value="total">总金额分配</Radio>
                    <Radio value="per_symbol">按币种分配</Radio>
                  </Stack>
                </RadioGroup>
                <Text fontSize="xs" color="gray.500" mt={1}>
                  {capitalMode === 'total' 
                    ? '输入总资金，AI 将自动分配到各个币种' 
                    : '分别设置每个币种的资金量，AI 根据资金决定网格参数'}
                </Text>
              </FormControl>

              {/* 总金额模式 */}
              {capitalMode === 'total' && (
              <FormControl isRequired>
                <FormLabel>可用资金 (USDT)</FormLabel>
                <NumberInput
                    value={totalCapital}
                    onChange={(_, value) => setTotalCapital(value || 0)}
                  min={100}
                  precision={2}
                >
                  <NumberInputField />
                </NumberInput>
              </FormControl>
              )}

              {/* 按币种分配模式 */}
              {capitalMode === 'per_symbol' && (
                <FormControl isRequired>
                  <FormLabel>各币种资金分配 (USDT)</FormLabel>
                  <VStack spacing={2} align="stretch">
                    {symbolCapitals.map(({ symbol, capital }) => (
                      <HStack key={symbol}>
                        <Badge colorScheme="green" minW="100px" textAlign="center">{symbol}</Badge>
                        <NumberInput
                          value={capital}
                          onChange={(_, value) => handleSymbolCapitalChange(symbol, value || 0)}
                          min={0}
                          precision={2}
                          flex={1}
                        >
                          <NumberInputField placeholder="输入资金量" />
                        </NumberInput>
                        <Text fontSize="sm" color="gray.500">USDT</Text>
                      </HStack>
                    ))}
                    {symbolCapitals.length > 0 && (
                      <HStack justify="flex-end" pt={2} borderTop="1px" borderColor={borderColor}>
                        <Text fontSize="sm" fontWeight="bold">总计:</Text>
                        <Text fontSize="sm" fontWeight="bold" color="blue.500">
                          {totalSymbolCapitals.toFixed(2)} USDT
                        </Text>
                      </HStack>
                    )}
                  </VStack>
                </FormControl>
              )}

              <Divider />

              {/* 风险偏好 */}
              <FormControl isRequired>
                <FormLabel>风险偏好</FormLabel>
                <Select
                  value={riskProfile}
                  onChange={(e) => setRiskProfile(e.target.value as any)}
                >
                  <option value="conservative">保守型（低风险，稳健收益）</option>
                  <option value="balanced">平衡型（中等风险，适中收益）</option>
                  <option value="aggressive">激进型（高风险，追求高收益）</option>
                </Select>
              </FormControl>
            </VStack>
          )}

          {step === 'preview' && aiConfig && (
            <VStack spacing={4} align="stretch">
              <Alert status="success" borderRadius="md">
                <AlertIcon />
                <Box>
                  <AlertTitle>配置生成成功</AlertTitle>
                  <AlertDescription fontSize="sm">
                    请仔细查看 AI 生成的配置方案，确认无误后点击"应用配置"
                  </AlertDescription>
                </Box>
              </Alert>

              <Box>
                <Text fontWeight="bold" mb={2}>AI 配置说明</Text>
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

              {aiConfig.grid_config.some(g => g.grid_risk_control?.enabled) && (
                <>
                  <Divider />
                  <Box>
                    <Text fontWeight="bold" mb={2}>网格风控配置</Text>
                    <TableContainer>
                      <Table size="sm" variant="simple">
                        <Thead>
                          <Tr>
                            <Th>币种</Th>
                            <Th>最大层数</Th>
                            <Th>止损比例</Th>
                            <Th>盈利触发</Th>
                            <Th>回撤止盈</Th>
                            <Th>趋势过滤</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {aiConfig.grid_config
                            .filter(g => g.grid_risk_control?.enabled)
                            .map((grid, idx) => (
                              <Tr key={idx}>
                                <Td><Badge>{grid.symbol}</Badge></Td>
                                <Td>{grid.grid_risk_control?.max_grid_layers || '-'}</Td>
                                <Td>{(grid.grid_risk_control?.stop_loss_ratio || 0) * 100}%</Td>
                                <Td>{(grid.grid_risk_control?.take_profit_trigger_ratio || 0) * 100}%</Td>
                                <Td>{(grid.grid_risk_control?.trailing_take_profit_ratio || 0) * 100}%</Td>
                                <Td>{grid.grid_risk_control?.trend_filter_enabled ? '✓' : '✗'}</Td>
                              </Tr>
                            ))}
                        </Tbody>
                      </Table>
                    </TableContainer>
                  </Box>
                </>
              )}

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
          <HStack spacing={2}>
            {step === 'form' && (
              <>
                <Button variant="ghost" onClick={handleClose}>
                  取消
                </Button>
                <Button
                  colorScheme="blue"
                  onClick={handleGenerate}
                  isLoading={loading}
                  isDisabled={symbols.length === 0 || !geminiApiKey.trim()}
                >
                  生成配置
                </Button>
              </>
            )}
            {step === 'preview' && (
              <>
                <Button variant="ghost" onClick={handleReset}>
                  重新生成
                </Button>
                <Button variant="ghost" onClick={handleClose}>
                  取消
                </Button>
                <Button
                  colorScheme="green"
                  onClick={handleApply}
                  isLoading={loading}
                >
                  应用配置
                </Button>
              </>
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
