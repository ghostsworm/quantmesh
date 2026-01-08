import React, { useState } from 'react'
import {
  Box,
  Button,
  FormControl,
  FormLabel,
  Input,
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
  useDisclosure,
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
  Code,
  useColorModeValue,
  Accordion,
  AccordionItem,
  AccordionButton,
  AccordionPanel,
  AccordionIcon,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import { generateAIConfig, applyAIConfig, AIGenerateConfigRequest, AIGenerateConfigResponse } from '../services/api'
import { StarIcon } from '@chakra-ui/icons'

interface AIConfigWizardProps {
  isOpen: boolean
  onClose: () => void
  onSuccess?: () => void
}

const AIConfigWizard: React.FC<AIConfigWizardProps> = ({ isOpen, onClose, onSuccess }) => {
  const { t } = useTranslation()
  const toast = useToast()
  const [step, setStep] = useState<'form' | 'preview' | 'success'>('form')
  const [loading, setLoading] = useState(false)
  const [formData, setFormData] = useState<AIGenerateConfigRequest>({
    exchange: 'binance',
    symbols: [],
    total_capital: 10000,
    risk_profile: 'balanced',
  })
  const [symbolInput, setSymbolInput] = useState('')
  const [aiConfig, setAiConfig] = useState<AIGenerateConfigResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  const bg = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.700')

  const handleAddSymbol = () => {
    const symbol = symbolInput.trim().toUpperCase()
    if (symbol && !formData.symbols.includes(symbol)) {
      setFormData(prev => ({
        ...prev,
        symbols: [...prev.symbols, symbol],
      }))
      setSymbolInput('')
    }
  }

  const handleRemoveSymbol = (symbol: string) => {
    setFormData(prev => ({
      ...prev,
      symbols: prev.symbols.filter(s => s !== symbol),
    }))
  }

  const handleGenerate = async () => {
    if (formData.symbols.length === 0) {
      setError('请至少添加一个交易币种')
      return
    }

    setLoading(true)
    setError(null)

    try {
      const config = await generateAIConfig(formData)
      setAiConfig(config)
      setStep('preview')
      toast({
        title: '配置生成成功',
        status: 'success',
        duration: 3000,
      })
    } catch (err: any) {
      const errorMsg = err.message || '生成配置失败，请检查 Gemini API Key 是否已配置'
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

              <FormControl isRequired>
                <FormLabel>交易所</FormLabel>
                <Select
                  value={formData.exchange}
                  onChange={(e) => setFormData(prev => ({ ...prev, exchange: e.target.value }))}
                >
                  <option value="binance">Binance</option>
                  <option value="gate">Gate.io</option>
                  <option value="bitget">Bitget</option>
                  <option value="bybit">Bybit</option>
                </Select>
              </FormControl>

              <FormControl isRequired>
                <FormLabel>交易币种</FormLabel>
                <HStack>
                  <Input
                    placeholder="例如: BTCUSDT"
                    value={symbolInput}
                    onChange={(e) => setSymbolInput(e.target.value)}
                    onKeyPress={(e) => e.key === 'Enter' && handleAddSymbol()}
                  />
                  <Button onClick={handleAddSymbol} size="md">添加</Button>
                </HStack>
                {formData.symbols.length > 0 && (
                  <HStack mt={2} flexWrap="wrap" spacing={2}>
                    {formData.symbols.map(symbol => (
                      <Badge
                        key={symbol}
                        colorScheme="blue"
                        px={2}
                        py={1}
                        borderRadius="md"
                        cursor="pointer"
                        onClick={() => handleRemoveSymbol(symbol)}
                      >
                        {symbol} ×
                      </Badge>
                    ))}
                  </HStack>
                )}
              </FormControl>

              <FormControl isRequired>
                <FormLabel>可用资金 (USDT)</FormLabel>
                <NumberInput
                  value={formData.total_capital}
                  onChange={(_, value) => setFormData(prev => ({ ...prev, total_capital: value }))}
                  min={100}
                  precision={2}
                >
                  <NumberInputField />
                </NumberInput>
              </FormControl>

              <FormControl isRequired>
                <FormLabel>风险偏好</FormLabel>
                <Select
                  value={formData.risk_profile}
                  onChange={(e) => setFormData(prev => ({ ...prev, risk_profile: e.target.value as any }))}
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
                  bg={useColorModeValue('gray.50', 'gray.700')}
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
                  isDisabled={formData.symbols.length === 0}
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

