import React, { useState, useEffect, useCallback, useRef } from 'react'
import {
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  Button,
  VStack,
  HStack,
  Box,
  Text,
  Input,
  Select,
  IconButton,
  Badge,
  Divider,
  FormControl,
  FormLabel,
  useToast,
  Alert,
  AlertIcon,
  Flex,
  NumberInput,
  NumberInputField,
  NumberInputStepper,
  NumberIncrementStepper,
  NumberDecrementStepper,
  Tooltip,
  Tag,
} from '@chakra-ui/react'
import { AddIcon, DeleteIcon, WarningIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { closePositionsV2 } from '../services/api'
import { loadRules, saveRules } from '../utils/tpSlStorage'

// ─── Types ───────────────────────────────────────────────────────────────────

export type TpSlDirection = 'take_profit' | 'stop_loss'
export type TpSlQuantityRatio = 'full' | 'half' | 'third' | 'quarter'
export type TpSlCloseMethod = 'market' | 'limit'

export interface TpSlRule {
  id: string
  direction: TpSlDirection
  triggerPrice: number
  orderPrice: number | null  // null = 市价
  quantityRatio: TpSlQuantityRatio
  method: TpSlCloseMethod
  triggered: boolean
  createdAt: number
}

interface TpSlRulesModalProps {
  isOpen: boolean
  onClose: () => void
  exchange: string
  symbol: string
  marketType?: string
  botId?: string
  currentPrice: number
  totalQuantity: number
}

// ─── Ratio helpers ─────────────────────────────────────────────────────────

function ratioToFloat(ratio: TpSlQuantityRatio): number {
  switch (ratio) {
    case 'full': return 1.0
    case 'half': return 0.5
    case 'third': return 1 / 3
    case 'quarter': return 0.25
  }
}

function ratioLabel(ratio: TpSlQuantityRatio, t: (k: string) => string): string {
  switch (ratio) {
    case 'full': return t('tpSl.fullPosition')
    case 'half': return t('tpSl.halfPosition')
    case 'third': return t('tpSl.thirdPosition')
    case 'quarter': return t('tpSl.quarterPosition')
  }
}

function newRule(direction: TpSlDirection, currentPrice: number): TpSlRule {
  return {
    id: `${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
    direction,
    triggerPrice: direction === 'take_profit' ? currentPrice * 1.05 : currentPrice * 0.95,
    orderPrice: null,
    quantityRatio: 'full',
    method: 'limit',
    triggered: false,
    createdAt: Date.now(),
  }
}

// ─── RuleRow ───────────────────────────────────────────────────────────────

interface RuleRowProps {
  rule: TpSlRule
  onChange: (updated: TpSlRule) => void
  onDelete: () => void
  currentPrice: number
}

const RuleRow: React.FC<RuleRowProps> = ({ rule, onChange, onDelete, currentPrice }) => {
  const { t } = useTranslation()

  const isTriggered = rule.direction === 'take_profit'
    ? currentPrice >= rule.triggerPrice
    : currentPrice <= rule.triggerPrice

  return (
    <Box
      borderWidth="1px"
      borderRadius="lg"
      p={3}
      bg={isTriggered ? 'orange.50' : rule.direction === 'take_profit' ? 'green.50' : 'red.50'}
      borderColor={isTriggered ? 'orange.300' : rule.direction === 'take_profit' ? 'green.200' : 'red.200'}
      position="relative"
    >
      <HStack justify="space-between" mb={2}>
        <HStack spacing={2}>
          <Badge colorScheme={rule.direction === 'take_profit' ? 'green' : 'red'} borderRadius="full">
            {rule.direction === 'take_profit' ? t('tpSl.takeProfit') : t('tpSl.stopLoss')}
          </Badge>
          {isTriggered && (
            <Tag size="sm" colorScheme="orange" borderRadius="full">
              <WarningIcon mr={1} boxSize="10px" />
              {t('tpSl.ruleActive')}
            </Tag>
          )}
        </HStack>
        <IconButton
          aria-label={t('tpSl.deleteRule')}
          icon={<DeleteIcon />}
          size="xs"
          variant="ghost"
          colorScheme="red"
          onClick={onDelete}
        />
      </HStack>

      <VStack spacing={2} align="stretch">
        <HStack spacing={3} flexWrap="wrap">
          {/* 方向 */}
          <FormControl w="auto" minW="100px">
            <FormLabel fontSize="xs" mb={1}>{t('tpSl.direction')}</FormLabel>
            <Select
              size="sm"
              value={rule.direction}
              onChange={e => onChange({ ...rule, direction: e.target.value as TpSlDirection })}
            >
              <option value="take_profit">{t('tpSl.takeProfit')}</option>
              <option value="stop_loss">{t('tpSl.stopLoss')}</option>
            </Select>
          </FormControl>

          {/* 触发价格 */}
          <FormControl w="auto" minW="120px">
            <FormLabel fontSize="xs" mb={1}>{t('tpSl.triggerPrice')}</FormLabel>
            <NumberInput
              size="sm"
              value={rule.triggerPrice}
              min={0}
              precision={4}
              onChange={(_, val) => !isNaN(val) && onChange({ ...rule, triggerPrice: val })}
            >
              <NumberInputField />
              <NumberInputStepper>
                <NumberIncrementStepper />
                <NumberDecrementStepper />
              </NumberInputStepper>
            </NumberInput>
          </FormControl>

          {/* 委托价格 */}
          <FormControl w="auto" minW="120px">
            <FormLabel fontSize="xs" mb={1}>{t('tpSl.orderPrice')}</FormLabel>
            <NumberInput
              size="sm"
              value={rule.orderPrice ?? ''}
              min={0}
              precision={4}
              onChange={(strVal, numVal) => {
                onChange({ ...rule, orderPrice: strVal === '' ? null : numVal })
              }}
            >
              <NumberInputField placeholder={t('tpSl.orderPricePlaceholder')} />
              <NumberInputStepper>
                <NumberIncrementStepper />
                <NumberDecrementStepper />
              </NumberInputStepper>
            </NumberInput>
          </FormControl>

          {/* 卖出比例 */}
          <FormControl w="auto" minW="100px">
            <FormLabel fontSize="xs" mb={1}>{t('tpSl.quantityRatio')}</FormLabel>
            <Select
              size="sm"
              value={rule.quantityRatio}
              onChange={e => onChange({ ...rule, quantityRatio: e.target.value as TpSlQuantityRatio })}
            >
              <option value="full">{t('tpSl.fullPosition')}</option>
              <option value="half">{t('tpSl.halfPosition')}</option>
              <option value="third">{t('tpSl.thirdPosition')}</option>
              <option value="quarter">{t('tpSl.quarterPosition')}</option>
            </Select>
          </FormControl>

          {/* 平仓方式 */}
          <FormControl w="auto" minW="90px">
            <FormLabel fontSize="xs" mb={1}>{t('tpSl.closeMethod')}</FormLabel>
            <Select
              size="sm"
              value={rule.method}
              onChange={e => onChange({ ...rule, method: e.target.value as TpSlCloseMethod })}
            >
              <option value="limit">{t('tpSl.limit')}</option>
              <option value="market">{t('tpSl.market')}</option>
            </Select>
          </FormControl>
        </HStack>

        {/* 条件说明 */}
        <Text fontSize="xs" color="gray.500">
          {rule.direction === 'take_profit' ? t('tpSl.priceAbove') : t('tpSl.priceBelow')}
          {' '}<strong>{rule.triggerPrice}</strong>
          {' → '}{ratioLabel(rule.quantityRatio, t)}
          {rule.orderPrice ? ` @ ${rule.orderPrice} (${rule.method})` : ` (${t('tpSl.market')})`}
        </Text>
      </VStack>
    </Box>
  )
}

// ─── Main Modal ────────────────────────────────────────────────────────────

const TpSlRulesModal: React.FC<TpSlRulesModalProps> = ({
  isOpen,
  onClose,
  exchange,
  symbol,
  botId,
  currentPrice,
}) => {
  const { t } = useTranslation()
  const toast = useToast()
  const [rules, setRules] = useState<TpSlRule[]>([])
  const [executing, setExecuting] = useState<Set<string>>(new Set())
  const priceRef = useRef(currentPrice)
  const rulesRef = useRef(rules)

  // 同步最新价格和规则到 ref（轮询闭包用）
  useEffect(() => { priceRef.current = currentPrice }, [currentPrice])
  useEffect(() => { rulesRef.current = rules }, [rules])

  // 打开时从 localStorage 加载
  useEffect(() => {
    if (isOpen) {
      setRules(loadRules(exchange, symbol))
    }
  }, [isOpen, exchange, symbol])

  // 自动触发轮询：每 5s 检查一次
  useEffect(() => {
    if (!isOpen) return

    const timer = setInterval(async () => {
      const price = priceRef.current
      const current = rulesRef.current
      if (!price || !current.length) return

      for (const rule of current) {
        if (rule.triggered) continue

        const shouldTrigger = rule.direction === 'take_profit'
          ? price >= rule.triggerPrice
          : price <= rule.triggerPrice

        if (!shouldTrigger) continue
        if (!botId) {
          toast({
            title: t('tpSl.botNotFound'),
            status: 'warning',
            duration: 4000,
          })
          continue
        }

        // 标记已触发，避免重复执行
        setRules(prev => {
          const updated = prev.map(r => r.id === rule.id ? { ...r, triggered: true } : r)
          saveRules(exchange, symbol, updated)
          return updated
        })

        setExecuting(prev => new Set([...prev, rule.id]))
        try {
          const res = await closePositionsV2(botId, {
            method: rule.orderPrice ? rule.method : 'market',
            price_offset: rule.orderPrice ? undefined : undefined,
            quantity_ratio: ratioToFloat(rule.quantityRatio),
            auto_retry: true,
          })
          if (res.success) {
            toast({
              title: t('tpSl.executedSuccess'),
              description: t('tpSl.ruleTriggered', { symbol, price: price.toFixed(2) }),
              status: 'success',
              duration: 5000,
            })
          } else {
            toast({
              title: t('tpSl.executedFail'),
              description: res.error_message || '',
              status: 'error',
              duration: 6000,
            })
          }
        } catch (err) {
          toast({
            title: t('tpSl.executedFail'),
            description: err instanceof Error ? err.message : String(err),
            status: 'error',
            duration: 6000,
          })
        } finally {
          setExecuting(prev => { const s = new Set(prev); s.delete(rule.id); return s })
        }
      }
    }, 5000)

    return () => clearInterval(timer)
  }, [isOpen, botId, exchange, symbol, t, toast])

  const handleAddRule = useCallback((direction: TpSlDirection) => {
    setRules(prev => [...prev, newRule(direction, currentPrice)])
  }, [currentPrice])

  const handleChange = useCallback((id: string, updated: TpSlRule) => {
    setRules(prev => prev.map(r => r.id === id ? updated : r))
  }, [])

  const handleDelete = useCallback((id: string) => {
    setRules(prev => {
      const next = prev.filter(r => r.id !== id)
      saveRules(exchange, symbol, next)
      return next
    })
  }, [exchange, symbol])

  const handleSave = () => {
    saveRules(exchange, symbol, rules)
    toast({
      title: t('tpSl.saveRules'),
      status: 'success',
      duration: 2000,
      isClosable: true,
    })
    onClose()
  }

  const activeCount = rules.filter(r => !r.triggered).length

  return (
    <Modal isOpen={isOpen} onClose={onClose} size="2xl" scrollBehavior="inside">
      <ModalOverlay />
      <ModalContent>
        <ModalHeader>
          <HStack spacing={3}>
            <Text>{t('tpSl.title')}</Text>
            <Badge colorScheme="blue" borderRadius="full" fontSize="xs">
              {symbol}
            </Badge>
            {activeCount > 0 && (
              <Badge colorScheme="green" borderRadius="full" fontSize="xs">
                {t('tpSl.rulesMonitoring', { count: activeCount })}
              </Badge>
            )}
          </HStack>
        </ModalHeader>
        <ModalCloseButton />

        <ModalBody>
          <VStack spacing={3} align="stretch">
            {/* 当前价格提示 */}
            <Alert status="info" borderRadius="md" py={2}>
              <AlertIcon />
              <Text fontSize="sm">
                {t('tpSl.currentPrice')}: <strong>{currentPrice > 0 ? currentPrice.toFixed(4) : '—'}</strong>
              </Text>
            </Alert>

            {!botId && (
              <Alert status="warning" borderRadius="md" py={2}>
                <AlertIcon />
                <Text fontSize="sm">{t('tpSl.botNotFound')}</Text>
              </Alert>
            )}

            {/* 规则列表 */}
            {rules.length === 0 ? (
              <Box textAlign="center" py={8} color="gray.500">
                <Text>{t('tpSl.noRules')}</Text>
              </Box>
            ) : (
              rules.map(rule => (
                <RuleRow
                  key={rule.id}
                  rule={rule}
                  onChange={updated => handleChange(rule.id, updated)}
                  onDelete={() => handleDelete(rule.id)}
                  currentPrice={currentPrice}
                />
              ))
            )}

            <Divider />

            {/* 添加规则按钮 */}
            <Flex gap={2} flexWrap="wrap">
              <Tooltip label={t('tpSl.takeProfit')}>
                <Button
                  size="sm"
                  leftIcon={<AddIcon />}
                  colorScheme="green"
                  variant="outline"
                  onClick={() => handleAddRule('take_profit')}
                >
                  {t('tpSl.takeProfit')} {t('tpSl.addRule')}
                </Button>
              </Tooltip>
              <Tooltip label={t('tpSl.stopLoss')}>
                <Button
                  size="sm"
                  leftIcon={<AddIcon />}
                  colorScheme="red"
                  variant="outline"
                  onClick={() => handleAddRule('stop_loss')}
                >
                  {t('tpSl.stopLoss')} {t('tpSl.addRule')}
                </Button>
              </Tooltip>
            </Flex>
          </VStack>
        </ModalBody>

        <ModalFooter gap={2}>
          <Button variant="ghost" onClick={onClose}>
            {t('tpSl.cancelClose')}
          </Button>
          <Button
            colorScheme="blue"
            onClick={handleSave}
            isLoading={executing.size > 0}
          >
            {t('tpSl.saveRules')}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}

export default TpSlRulesModal
