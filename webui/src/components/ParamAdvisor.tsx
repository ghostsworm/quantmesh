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

/** 维持保证金比率（常见合约约 0.5%） */
const DEFAULT_MMR = 0.005

export interface LiquidationEstimate {
  liquidationPrice: number
  avgEntryPrice: number
  positionBtc: number
  positionNotional: number
  /** 当前价格距强平价的安全比例 (current - liq) / current */
  safetyPercent: number
  /** 是否可计算（参数不足或做多时保证金充足不会强平则可能为 null） */
  valid: boolean
}

/**
 * 根据当前仓位（满买窗）、总资金、杠杆等估算强平价格（做多）。
 * 假设：买窗全部成交，仓位 = sum(orderQuantity/price_i)，加权平均开仓价 = avg_entry。
 * 强平条件：保证金余额 <= 持仓价值 × 维持保证金比率。
 *
 * @param maxCapitalRatio 最大资金占用比例 (0.1-1.0)，例如 0.2 表示只用 20% 的资金
 */
export function computeLiquidationPrice(params: {
  currentPrice: number
  buyWindowSize: number
  orderQuantity: number
  priceInterval: number
  totalCapital: number
  leverage: number
  maintenanceMarginRate?: number
  maxCapitalRatio?: number // 新增：最大资金占用比例
}): LiquidationEstimate | null {
  const {
    currentPrice,
    buyWindowSize: n,
    orderQuantity: Q,
    priceInterval: I,
    totalCapital,
    leverage,
    maintenanceMarginRate: MMR = DEFAULT_MMR,
    maxCapitalRatio = 1.0, // 默认不限制
  } = params

  if (currentPrice <= 0 || n <= 0 || Q <= 0 || totalCapital <= 0 || leverage <= 0) {
    return null
  }

  // 计算实际可用资金（考虑最大资金占用限制）
  const actualCapital = totalCapital * Math.min(maxCapitalRatio, 1.0)

  let sumInvP = 0
  for (let i = 0; i < n; i++) {
    const p = currentPrice - i * I
    if (p <= 0) return null
    sumInvP += 1 / p
  }

  const positionBtc = Q * sumInvP
  const positionNotional = n * Q
  const avgEntryPrice = positionNotional / positionBtc

  // 做多强平：margin_balance = actualCapital + (liq_price - avg_entry) * position_btc
  // 强平时 margin_balance = position_btc * liq_price * MMR
  // => actualCapital + (liq_price - avg_entry)*position_btc = position_btc * liq_price * MMR
  // => actualCapital - avg_entry*position_btc = liq_price * position_btc * (MMR - 1)
  const denom = positionBtc * (MMR - 1)
  if (denom >= 0) return null
  const numerator = actualCapital - avgEntryPrice * positionBtc
  const liquidationPrice = numerator / denom
  if (liquidationPrice <= 0 || liquidationPrice >= currentPrice) {
    return null
  }

  const safetyPercent = ((currentPrice - liquidationPrice) / currentPrice) * 100

  return {
    liquidationPrice,
    avgEntryPrice,
    positionBtc,
    positionNotional,
    safetyPercent,
    valid: true,
  }
}

interface ParamAdvisorProps {
  exchange: string
  symbol: string
  currentPriceInterval?: number
  currentOrderQuantity?: number
  onApplyPriceInterval?: (value: number) => void
  onApplyOrderQuantity?: (value: number) => void
  /** 买窗数量（用于强平价估算） */
  buyWindowSize?: number
  /** 杠杆倍数（用于强平价估算） */
  leverage?: number
  /** 总资金 USDT（用于强平价估算，可在此组件内输入覆盖） */
  totalCapital?: number
}

const ParamAdvisor: React.FC<ParamAdvisorProps> = ({
  exchange,
  symbol,
  currentPriceInterval,
  currentOrderQuantity,
  onApplyPriceInterval,
  onApplyOrderQuantity,
  buyWindowSize,
  leverage: leverageProp,
  totalCapital: totalCapitalProp,
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
  const [totalCapitalInput, setTotalCapitalInput] = useState<string>(totalCapitalProp != null ? String(totalCapitalProp) : '')
  const [leverageInput, setLeverageInput] = useState<string>(leverageProp != null ? String(leverageProp) : '')

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

                {/* 强平价格估算（交易概念） */}
                <Box
                  p={4}
                  borderWidth="1px"
                  borderRadius="lg"
                  borderColor="orange.200"
                  bg="orange.50"
                >
                  <Flex justify="space-between" align="center" mb={2}>
                    <Text fontSize="sm" fontWeight="600" color="orange.800">
                      {t('paramAdvisor.liquidationPriceTitle')}
                    </Text>
                    <Tooltip label={t('paramAdvisor.liquidationPriceTooltip')}>
                      <InfoIcon color="orange.500" boxSize={4} />
                    </Tooltip>
                  </Flex>
                  <Text fontSize="xs" color="gray.600" mb={3}>
                    {t('paramAdvisor.liquidationPriceDesc')}
                  </Text>
                  <SimpleGrid columns={2} spacing={3} mb={3}>
                    <FormControl size="sm">
                      <FormLabel fontSize="xs" color="gray.600">
                        {t('paramAdvisor.totalCapital')} (USDT)
                      </FormLabel>
                      <NumberInput
                        size="sm"
                        value={totalCapitalInput}
                        onChange={setTotalCapitalInput}
                        min={0}
                        precision={2}
                      >
                        <NumberInputField borderRadius="lg" placeholder="20000" />
                      </NumberInput>
                    </FormControl>
                    <FormControl size="sm">
                      <FormLabel fontSize="xs" color="gray.600">
                        {t('paramAdvisor.leverage')}
                      </FormLabel>
                      <NumberInput
                        size="sm"
                        value={leverageInput}
                        onChange={setLeverageInput}
                        min={1}
                        max={125}
                        precision={0}
                      >
                        <NumberInputField borderRadius="lg" placeholder="10" />
                      </NumberInput>
                    </FormControl>
                  </SimpleGrid>
                  {(() => {
                    const totalCap = totalCapitalInput ? parseFloat(totalCapitalInput) : totalCapitalProp
                    const lev = leverageInput ? parseFloat(leverageInput) : leverageProp
                    const n = buyWindowSize ?? 0
                    const Q = currentOrderQuantity ?? 0
                    const I = currentPriceInterval ?? 0
                    const price = advisorData.current_price
                    const liq = totalCap != null && totalCap > 0 && lev != null && lev > 0 && n > 0 && Q > 0 && I > 0 && price > 0
                      ? computeLiquidationPrice({
                          currentPrice: price,
                          buyWindowSize: n,
                          orderQuantity: Q,
                          priceInterval: I,
                          totalCapital: totalCap,
                          leverage: lev,
                        })
                      : null
                    if (!liq?.valid) {
                      return (
                        <Text fontSize="xs" color="gray.500">
                          {t('paramAdvisor.liquidationPriceHint')}
                        </Text>
                      )
                    }
                    return (
                      <VStack align="stretch" spacing={2}>
                        <HStack justify="space-between">
                          <Text fontSize="xs" color="gray.600">{t('paramAdvisor.estimatedLiquidationPrice')}</Text>
                          <Text fontSize="sm" fontWeight="bold" color="orange.700">
                            {liq.liquidationPrice.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                          </Text>
                        </HStack>
                        <HStack justify="space-between">
                          <Text fontSize="xs" color="gray.600">{t('paramAdvisor.avgEntryPrice')}</Text>
                          <Text fontSize="xs">{liq.avgEntryPrice.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}</Text>
                        </HStack>
                        <HStack justify="space-between">
                          <Text fontSize="xs" color="gray.600">{t('paramAdvisor.safetyDistance')}</Text>
                          <Text fontSize="xs" color={liq.safetyPercent > 10 ? 'green.600' : liq.safetyPercent > 5 ? 'orange.600' : 'red.600'}>
                            {liq.safetyPercent.toFixed(2)}%
                          </Text>
                        </HStack>
                        <Text fontSize="2xs" color="gray.500" mt={1}>
                          {t('paramAdvisor.liquidationDisclaimer')}
                        </Text>
                      </VStack>
                    )
                  })()}
                </Box>

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
