import React, { useState, useCallback } from 'react'
import {
  Box,
  Button,
  FormControl,
  FormLabel,
  NumberInput,
  NumberInputField,
  HStack,
  VStack,
  Text,
  Badge,
  Alert,
  AlertIcon,
  AlertDescription,
  Spinner,
  SimpleGrid,
  Tooltip,
  IconButton,
  Divider,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  Flex,
  useToast,
  Collapse,
  useDisclosure,
} from '@chakra-ui/react'
import { RepeatIcon, InfoIcon, ChevronDownIcon, ChevronUpIcon, CheckIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import {
  getParamAdvisor,
  getExchangeFees,
  type ParamAdvisorResponse,
  type RangeAdvice,
} from '../services/api'

interface ParamAdvisorProps {
  exchange: string
  symbol: string
  currentPriceInterval?: number
  currentOrderQuantity?: number
  onApplyPriceInterval?: (value: number) => void
  onApplyOrderQuantity?: (value: number) => void
}

const ParamAdvisor: React.FC<ParamAdvisorProps> = ({
  exchange,
  symbol,
  currentPriceInterval,
  currentOrderQuantity,
  onApplyPriceInterval,
  onApplyOrderQuantity,
}) => {
  const { t } = useTranslation()
  const toast = useToast()
  const { isOpen, onToggle } = useDisclosure({ defaultIsOpen: false })

  const [loading, setLoading] = useState(false)
  const [fetchingFees, setFetchingFees] = useState(false)
  const [advisorData, setAdvisorData] = useState<ParamAdvisorResponse | null>(null)
  const [makerFee, setMakerFee] = useState<string>('')
  const [takerFee, setTakerFee] = useState<string>('')
  const [feeSource, setFeeSource] = useState<string>('')

  // 获取参数建议
  const fetchAdvisor = useCallback(async () => {
    if (!exchange || !symbol) {
      toast({
        title: t('paramAdvisor.missingSymbol'),
        status: 'warning',
        duration: 3000,
      })
      return
    }

    setLoading(true)
    try {
      const makerVal = makerFee ? parseFloat(makerFee) : undefined
      const takerVal = takerFee ? parseFloat(takerFee) : undefined
      const data = await getParamAdvisor(exchange, symbol, makerVal, takerVal)
      setAdvisorData(data)

      // 如果用户没有手动设置费率，用返回的值填充
      if (!makerFee && data.maker_fee > 0) {
        setMakerFee(String(data.maker_fee))
      }
      if (!takerFee && data.taker_fee > 0) {
        setTakerFee(String(data.taker_fee))
      }
      if (!feeSource) {
        setFeeSource(data.fee_source)
      }
    } catch (err) {
      toast({
        title: t('paramAdvisor.fetchFailed'),
        description: err instanceof Error ? err.message : String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setLoading(false)
    }
  }, [exchange, symbol, makerFee, takerFee, feeSource, toast, t])

  // 从交易所 API 获取费率
  const fetchFeesFromExchange = useCallback(async () => {
    if (!exchange || !symbol) return

    setFetchingFees(true)
    try {
      const data = await getExchangeFees(exchange, symbol)
      setMakerFee(String(data.maker_fee))
      setTakerFee(String(data.taker_fee))
      setFeeSource(data.fee_source)
      toast({
        title: t('paramAdvisor.feesFetched'),
        description: data.fee_source === 'exchange_api'
          ? t('paramAdvisor.feesFromExchangeApi')
          : t('paramAdvisor.feesFromConfig'),
        status: data.fee_source === 'exchange_api' ? 'success' : 'info',
        duration: 3000,
      })
    } catch (err) {
      toast({
        title: t('paramAdvisor.fetchFeesFailed'),
        description: err instanceof Error ? err.message : String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setFetchingFees(false)
    }
  }, [exchange, symbol, toast, t])

  // 渲染费率来源 Badge
  const renderFeeSourceBadge = () => {
    if (!feeSource) return null
    const colorMap: Record<string, string> = {
      exchange_api: 'green',
      config: 'blue',
      default: 'gray',
      user_input: 'purple',
    }
    const labelMap: Record<string, string> = {
      exchange_api: t('paramAdvisor.sourceExchangeApi'),
      config: t('paramAdvisor.sourceConfig'),
      default: t('paramAdvisor.sourceDefault'),
      user_input: t('paramAdvisor.sourceUserInput'),
    }
    return (
      <Badge colorScheme={colorMap[feeSource] || 'gray'} fontSize="xs">
        {labelMap[feeSource] || feeSource}
      </Badge>
    )
  }

  // 渲染范围建议卡片
  const renderRangeAdvice = (
    label: string,
    advice: RangeAdvice,
    currentValue: number | undefined,
    onApply?: (value: number) => void,
    precision: number = 2
  ) => {
    const isCurrentInRange = currentValue !== undefined && currentValue >= advice.min && currentValue <= advice.max
    const isCurrentBelowMin = currentValue !== undefined && currentValue < advice.min
    const isCurrentAboveMax = currentValue !== undefined && currentValue > advice.max

    return (
      <Box
        p={4}
        borderWidth="1px"
        borderRadius="lg"
        borderColor={isCurrentBelowMin ? 'orange.200' : isCurrentAboveMax ? 'red.200' : 'gray.200'}
        bg={isCurrentBelowMin ? 'orange.50' : isCurrentAboveMax ? 'red.50' : 'gray.50'}
      >
        <Flex justify="space-between" align="center" mb={2}>
          <Text fontSize="sm" fontWeight="600">{label}</Text>
          {currentValue !== undefined && (
            <Badge
              colorScheme={isCurrentInRange ? 'green' : isCurrentBelowMin ? 'orange' : 'red'}
              fontSize="xs"
            >
              {isCurrentInRange
                ? t('paramAdvisor.inRange')
                : isCurrentBelowMin
                  ? t('paramAdvisor.belowMin')
                  : t('paramAdvisor.aboveMax')}
            </Badge>
          )}
        </Flex>

        <SimpleGrid columns={3} spacing={3} mb={3}>
          <Stat size="sm">
            <StatLabel fontSize="xs" color="gray.500">{t('paramAdvisor.min')}</StatLabel>
            <StatNumber fontSize="sm">{advice.min.toFixed(precision)}</StatNumber>
          </Stat>
          <Stat size="sm">
            <StatLabel fontSize="xs" color="blue.500" fontWeight="bold">{t('paramAdvisor.recommended')}</StatLabel>
            <StatNumber fontSize="sm" color="blue.600">{advice.recommended.toFixed(precision)}</StatNumber>
          </Stat>
          <Stat size="sm">
            <StatLabel fontSize="xs" color="gray.500">{t('paramAdvisor.max')}</StatLabel>
            <StatNumber fontSize="sm">{advice.max.toFixed(precision)}</StatNumber>
          </Stat>
        </SimpleGrid>

        {currentValue !== undefined && (
          <Flex justify="space-between" align="center">
            <Text fontSize="xs" color="gray.500">
              {t('paramAdvisor.currentValue')}: <strong>{currentValue}</strong>
            </Text>
            {onApply && (
              <Button
                size="xs"
                colorScheme="blue"
                variant="outline"
                leftIcon={<CheckIcon />}
                onClick={() => onApply(advice.recommended)}
              >
                {t('paramAdvisor.applyRecommended')}
              </Button>
            )}
          </Flex>
        )}
        {currentValue === undefined && onApply && (
          <Flex justify="flex-end">
            <Button
              size="xs"
              colorScheme="blue"
              variant="outline"
              leftIcon={<CheckIcon />}
              onClick={() => onApply(advice.recommended)}
            >
              {t('paramAdvisor.applyRecommended')}
            </Button>
          </Flex>
        )}
      </Box>
    )
  }

  // 格式化费率为百分比
  const formatFeePercent = (fee: number) => {
    return `${(fee * 100).toFixed(4)}%`
  }

  return (
    <Box
      borderWidth="1px"
      borderRadius="xl"
      borderColor="blue.100"
      bg="blue.50"
      overflow="hidden"
    >
      {/* 折叠头部 */}
      <Flex
        px={4}
        py={3}
        cursor="pointer"
        onClick={onToggle}
        align="center"
        justify="space-between"
        _hover={{ bg: 'blue.100' }}
        transition="background 0.2s"
      >
        <HStack spacing={2}>
          <InfoIcon color="blue.500" boxSize={4} />
          <Text fontSize="sm" fontWeight="600" color="blue.700">
            {t('paramAdvisor.title')}
          </Text>
          <Text fontSize="xs" color="blue.500">
            {t('paramAdvisor.subtitle')}
          </Text>
        </HStack>
        {isOpen ? <ChevronUpIcon color="blue.500" /> : <ChevronDownIcon color="blue.500" />}
      </Flex>

      <Collapse in={isOpen} animateOpacity>
        <Box px={4} pb={4}>
          <VStack spacing={4} align="stretch">
            {/* 费率设置区域 */}
            <Box>
              <Flex justify="space-between" align="center" mb={2}>
                <HStack spacing={2}>
                  <Text fontSize="xs" fontWeight="600" color="gray.600">
                    {t('paramAdvisor.feeRateSettings')}
                  </Text>
                  {renderFeeSourceBadge()}
                </HStack>
                <Tooltip label={t('paramAdvisor.fetchFromExchangeTooltip')}>
                  <Button
                    size="xs"
                    colorScheme="blue"
                    variant="ghost"
                    leftIcon={fetchingFees ? <Spinner size="xs" /> : <RepeatIcon />}
                    onClick={fetchFeesFromExchange}
                    isLoading={fetchingFees}
                    loadingText={t('paramAdvisor.fetching')}
                  >
                    {t('paramAdvisor.autoFillFromExchange')}
                  </Button>
                </Tooltip>
              </Flex>

              <SimpleGrid columns={2} spacing={3}>
                <FormControl size="sm">
                  <FormLabel fontSize="xs" color="gray.500" mb={1}>
                    {t('paramAdvisor.makerFee')}
                  </FormLabel>
                  <NumberInput
                    size="sm"
                    value={makerFee}
                    onChange={(v) => {
                      setMakerFee(v)
                      setFeeSource('user_input')
                    }}
                    precision={6}
                    step={0.0001}
                  >
                    <NumberInputField
                      borderRadius="lg"
                      placeholder="0.0002"
                      fontSize="sm"
                    />
                  </NumberInput>
                  {makerFee && (
                    <Text fontSize="xs" color="gray.400" mt={0.5}>
                      = {formatFeePercent(parseFloat(makerFee) || 0)}
                    </Text>
                  )}
                </FormControl>

                <FormControl size="sm">
                  <FormLabel fontSize="xs" color="gray.500" mb={1}>
                    {t('paramAdvisor.takerFee')}
                  </FormLabel>
                  <NumberInput
                    size="sm"
                    value={takerFee}
                    onChange={(v) => {
                      setTakerFee(v)
                      setFeeSource('user_input')
                    }}
                    precision={6}
                    step={0.0001}
                  >
                    <NumberInputField
                      borderRadius="lg"
                      placeholder="0.0005"
                      fontSize="sm"
                    />
                  </NumberInput>
                  {takerFee && (
                    <Text fontSize="xs" color="gray.400" mt={0.5}>
                      = {formatFeePercent(parseFloat(takerFee) || 0)}
                    </Text>
                  )}
                </FormControl>
              </SimpleGrid>
            </Box>

            {/* 获取建议按钮 */}
            <Button
              colorScheme="blue"
              size="sm"
              onClick={fetchAdvisor}
              isLoading={loading}
              loadingText={t('paramAdvisor.calculating')}
              borderRadius="lg"
            >
              {t('paramAdvisor.getSuggestions')}
            </Button>

            {/* 建议结果 */}
            {advisorData && (
              <VStack spacing={3} align="stretch">
                {/* 当前价格 */}
                <Alert status="info" borderRadius="lg" size="sm" py={2}>
                  <AlertIcon />
                  <AlertDescription fontSize="xs">
                    <HStack spacing={4} flexWrap="wrap">
                      <Text>
                        {t('paramAdvisor.currentPrice')}: <strong>${advisorData.current_price.toLocaleString()}</strong>
                      </Text>
                      <Text>
                        {t('paramAdvisor.breakEvenInterval')}: <strong>{advisorData.suggestions.min_profitable_interval}</strong>
                      </Text>
                      <Text>
                        {t('paramAdvisor.totalFeeRate')}: <strong>{formatFeePercent(advisorData.suggestions.breakeven_fee_rate)}</strong>
                      </Text>
                    </HStack>
                  </AlertDescription>
                </Alert>

                <Divider />

                {/* Price Interval 建议 */}
                {renderRangeAdvice(
                  t('paramAdvisor.priceIntervalSuggestion'),
                  advisorData.suggestions.price_interval,
                  currentPriceInterval,
                  onApplyPriceInterval,
                  advisorData.current_price >= 1000 ? 0 : advisorData.current_price >= 1 ? 2 : 4
                )}

                {/* Order Quantity 建议 */}
                {renderRangeAdvice(
                  t('paramAdvisor.orderQuantitySuggestion'),
                  advisorData.suggestions.order_quantity,
                  currentOrderQuantity,
                  onApplyOrderQuantity,
                  0
                )}

                {/* 说明 */}
                <Alert status="warning" borderRadius="lg" size="sm" py={2}>
                  <AlertIcon />
                  <AlertDescription fontSize="xs">
                    {t('paramAdvisor.disclaimer')}
                  </AlertDescription>
                </Alert>
              </VStack>
            )}
          </VStack>
        </Box>
      </Collapse>
    </Box>
  )
}

export default ParamAdvisor
