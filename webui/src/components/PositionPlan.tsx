import React, { useState, useEffect, useCallback } from 'react'
import {
  Box,
  VStack,
  HStack,
  Heading,
  Text,
  Button,
  FormControl,
  FormLabel,
  Input,
  Select,
  Switch,
  Badge,
  Alert,
  AlertIcon,
  useToast,
  Spinner,
  Center,
  Divider,
  Progress,
  useColorModeValue,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  useDisclosure,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Tooltip,
  IconButton,
} from '@chakra-ui/react'
import { AddIcon, CloseIcon, RepeatIcon, InfoIcon } from '@chakra-ui/icons'
import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import {
  getPositionPlans,
  createPositionPlan,
  cancelPositionPlan,
  checkPositionPlan,
} from '../services/positionPlan'
import { getSymbols } from '../services/api'
import type { PositionPlan, PlanStatus, CreatePositionPlanRequest } from '../types/positionPlan'

const MotionBox = motion(Box)

const statusColors: Record<PlanStatus, string> = {
  pending: 'yellow',
  in_progress: 'blue',
  completed: 'green',
  cancelled: 'gray',
}

const statusLabels: Record<PlanStatus, string> = {
  pending: 'positionPlan.status.pending',
  in_progress: 'positionPlan.status.inProgress',
  completed: 'positionPlan.status.completed',
  cancelled: 'positionPlan.status.cancelled',
}

const PositionPlanPage: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()

  const [plans, setPlans] = useState<PositionPlan[]>([])
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [symbols, setSymbols] = useState<{ exchange: string; symbol: string }[]>([])
  const [currentAmount, setCurrentAmount] = useState<number | null>(null)
  const [checkingAmount, setCheckingAmount] = useState(false)

  // 創建表單状態
  const [formData, setFormData] = useState<CreatePositionPlanRequest>({
    exchange: '',
    symbol: '',
    strategyId: '',
    targetAmountUsdt: 0,
    notifyOnComplete: true,
    autoAdjustLimit: true,
  })

  // 取消确认弹窗
  const { isOpen: isCancelOpen, onOpen: onCancelOpen, onClose: onCancelClose } = useDisclosure()
  const [cancelPlanId, setCancelPlanId] = useState<number | null>(null)
  const [restoreLimit, setRestoreLimit] = useState(true)

  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  // 加載數據
  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const [plansRes, symbolsRes] = await Promise.all([
        getPositionPlans(),
        getSymbols(),
      ])
      if (plansRes.success) {
        setPlans(plansRes.plans || [])
      }
      if (symbolsRes && symbolsRes.symbols) {
        setSymbols(symbolsRes.symbols)
      }
    } catch (err) {
      console.error('Failed to fetch data:', err)
      toast({
        title: t('positionPlan.fetchFailed'),
        status: 'error',
        duration: 3000,
      })
    } finally {
      setLoading(false)
    }
  }, [t, toast])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  // 當選擇交易對变化時，检查當前倉位
  const handleSymbolChange = async (exchange: string, symbol: string) => {
    setFormData((prev) => ({ ...prev, exchange, symbol }))
    if (exchange && symbol) {
      setCheckingAmount(true)
      try {
        const res = await checkPositionPlan(exchange, symbol)
        if (res.success) {
          setCurrentAmount(res.currentAmount)
          if (res.hasActivePlan) {
            toast({
              title: t('positionPlan.hasActivePlan'),
              description: t('positionPlan.cancelFirst'),
              status: 'warning',
              duration: 5000,
            })
          }
        }
      } catch {
        setCurrentAmount(null)
      } finally {
        setCheckingAmount(false)
      }
    } else {
      setCurrentAmount(null)
    }
  }

  // 提交創建计划
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!formData.exchange || !formData.symbol) {
      toast({ title: t('positionPlan.selectSymbol'), status: 'warning', duration: 3000 })
      return
    }
    if (formData.targetAmountUsdt < 0) {
      toast({ title: t('positionPlan.invalidTarget'), status: 'warning', duration: 3000 })
      return
    }

    setSubmitting(true)
    try {
      const res = await createPositionPlan(formData)
      if (res.success) {
        toast({
          title: res.message,
          status: 'success',
          duration: 3000,
        })
        // 重置表單並刷新列表
        setFormData({
          exchange: '',
          symbol: '',
          strategyId: '',
          targetAmountUsdt: 0,
          notifyOnComplete: true,
          autoAdjustLimit: true,
        })
        setCurrentAmount(null)
        fetchData()
      } else {
        toast({ title: res.error || res.message, status: 'error', duration: 5000 })
      }
    } catch (err) {
      toast({ title: t('positionPlan.createFailed'), status: 'error', duration: 5000 })
    } finally {
      setSubmitting(false)
    }
  }

  // 取消计划
  const handleCancelPlan = async () => {
    if (cancelPlanId === null) return
    try {
      const res = await cancelPositionPlan(cancelPlanId, restoreLimit)
      if (res.success) {
        toast({ title: res.message, status: 'success', duration: 3000 })
        fetchData()
      } else {
        toast({ title: res.error || res.message, status: 'error', duration: 5000 })
      }
    } catch {
      toast({ title: t('positionPlan.cancelFailed'), status: 'error', duration: 5000 })
    } finally {
      onCancelClose()
      setCancelPlanId(null)
    }
  }

  // 计算進度百分比
  const getProgress = (plan: PositionPlan): number => {
    if (plan.status === 'completed') return 100
    if (plan.status === 'cancelled') return 0
    const initial = plan.initialAmount
    const target = plan.targetAmountUsdt
    const current = plan.currentAmount
    if (initial === target) return 100
    const progress = ((initial - current) / (initial - target)) * 100
    return Math.max(0, Math.min(100, progress))
  }

  // 獲取唯一的交易所列表
  const exchanges = [...new Set(symbols.map((s) => s.exchange))]

  // 根據选擇的交易所過滤交易對
  const filteredSymbols = formData.exchange
    ? symbols.filter((s) => s.exchange === formData.exchange)
    : []

  if (loading) {
    return (
      <Center h="400px">
        <Spinner size="xl" />
      </Center>
    )
  }

  return (
    <Box p={6}>
      <MotionBox
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.5 }}
      >
        <HStack justify="space-between" mb={6}>
          <Heading size="lg">{t('positionPlan.title')}</Heading>
          <Button leftIcon={<RepeatIcon />} onClick={fetchData} size="sm">
            {t('common.refresh')}
          </Button>
        </HStack>

        {/* 創建新计划表單 */}
        <Box bg={bgColor} p={6} borderRadius="lg" borderWidth="1px" borderColor={borderColor} mb={6}>
          <Heading size="md" mb={4}>
            {t('positionPlan.createPlan')}
          </Heading>
          <form onSubmit={handleSubmit}>
            <VStack spacing={4} align="stretch">
              <HStack spacing={4}>
                <FormControl isRequired flex={1}>
                  <FormLabel>{t('positionPlan.exchange')}</FormLabel>
                  <Select
                    placeholder={t('positionPlan.selectExchange')}
                    value={formData.exchange}
                    onChange={(e) => handleSymbolChange(e.target.value, '')}
                  >
                    {exchanges.map((ex) => (
                      <option key={ex} value={ex}>
                        {ex}
                      </option>
                    ))}
                  </Select>
                </FormControl>
                <FormControl isRequired flex={1}>
                  <FormLabel>{t('positionPlan.symbol')}</FormLabel>
                  <Select
                    placeholder={t('positionPlan.selectSymbol')}
                    value={formData.symbol}
                    onChange={(e) => handleSymbolChange(formData.exchange, e.target.value)}
                    isDisabled={!formData.exchange}
                  >
                    {filteredSymbols.map((s) => (
                      <option key={s.symbol} value={s.symbol}>
                        {s.symbol}
                      </option>
                    ))}
                  </Select>
                </FormControl>
              </HStack>

              <HStack spacing={4}>
                <FormControl flex={1}>
                  <FormLabel>
                    {t('positionPlan.currentAmount')}
                    {checkingAmount && <Spinner size="xs" ml={2} />}
                  </FormLabel>
                  <Input
                    value={currentAmount !== null ? currentAmount.toFixed(2) : '-'}
                    isReadOnly
                    bg={useColorModeValue('gray.100', 'gray.700')}
                  />
                </FormControl>
                <FormControl isRequired flex={1}>
                  <FormLabel>{t('positionPlan.targetAmount')}</FormLabel>
                  <Input
                    type="number"
                    step="0.01"
                    min="0"
                    value={formData.targetAmountUsdt}
                    onChange={(e) =>
                      setFormData((prev) => ({ ...prev, targetAmountUsdt: parseFloat(e.target.value) || 0 }))
                    }
                    placeholder="USDT"
                  />
                </FormControl>
              </HStack>

              <HStack spacing={8}>
                <FormControl display="flex" alignItems="center">
                  <FormLabel mb="0">{t('positionPlan.notifyOnComplete')}</FormLabel>
                  <Switch
                    isChecked={formData.notifyOnComplete}
                    onChange={(e) => setFormData((prev) => ({ ...prev, notifyOnComplete: e.target.checked }))}
                  />
                </FormControl>
                <FormControl display="flex" alignItems="center">
                  <HStack>
                    <FormLabel mb="0">{t('positionPlan.autoAdjustLimit')}</FormLabel>
                    <Tooltip label={t('positionPlan.autoAdjustLimitHint')}>
                      <InfoIcon color="gray.400" />
                    </Tooltip>
                  </HStack>
                  <Switch
                    isChecked={formData.autoAdjustLimit}
                    onChange={(e) => setFormData((prev) => ({ ...prev, autoAdjustLimit: e.target.checked }))}
                  />
                </FormControl>
              </HStack>

              {formData.autoAdjustLimit && (
                <Alert status="info" borderRadius="md">
                  <AlertIcon />
                  {t('positionPlan.autoAdjustLimitWarning')}
                </Alert>
              )}

              <Button
                type="submit"
                colorScheme="blue"
                leftIcon={<AddIcon />}
                isLoading={submitting}
                isDisabled={!formData.exchange || !formData.symbol}
              >
                {t('positionPlan.createPlan')}
              </Button>
            </VStack>
          </form>
        </Box>

        <Divider my={6} />

        {/* 计划列表 */}
        <Box bg={bgColor} p={6} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
          <Heading size="md" mb={4}>
            {t('positionPlan.planList')}
          </Heading>
          {plans.length === 0 ? (
            <Text color="gray.500">{t('positionPlan.noPlans')}</Text>
          ) : (
            <Table variant="simple">
              <Thead>
                <Tr>
                  <Th>{t('positionPlan.exchange')}</Th>
                  <Th>{t('positionPlan.symbol')}</Th>
                  <Th>{t('positionPlan.target')}</Th>
                  <Th>{t('positionPlan.progress')}</Th>
                  <Th>{t('positionPlan.statusLabel')}</Th>
                  <Th>{t('positionPlan.createdAt')}</Th>
                  <Th>{t('common.actions')}</Th>
                </Tr>
              </Thead>
              <Tbody>
                {plans.map((plan) => (
                  <Tr key={plan.id}>
                    <Td>{plan.exchange}</Td>
                    <Td>{plan.symbol}</Td>
                    <Td>
                      <Text>
                        {plan.currentAmount.toFixed(2)} / {plan.targetAmountUsdt.toFixed(2)} USDT
                      </Text>
                      <Text fontSize="xs" color="gray.500">
                        {plan.direction === 'reduce' ? t('positionPlan.reduce') : t('positionPlan.increase')}
                      </Text>
                    </Td>
                    <Td minW="150px">
                      <Progress
                        value={getProgress(plan)}
                        colorScheme={statusColors[plan.status]}
                        size="sm"
                        borderRadius="full"
                      />
                      <Text fontSize="xs" mt={1}>
                        {getProgress(plan).toFixed(1)}%
                      </Text>
                    </Td>
                    <Td>
                      <Badge colorScheme={statusColors[plan.status]}>
                        {t(statusLabels[plan.status])}
                      </Badge>
                    </Td>
                    <Td>
                      <Text fontSize="sm">
                        {new Date(plan.createdAt).toLocaleString()}
                      </Text>
                    </Td>
                    <Td>
                      {(plan.status === 'pending' || plan.status === 'in_progress') && (
                        <Tooltip label={t('positionPlan.cancel')}>
                          <IconButton
                            aria-label="Cancel plan"
                            icon={<CloseIcon />}
                            size="sm"
                            colorScheme="red"
                            variant="ghost"
                            onClick={() => {
                              setCancelPlanId(plan.id)
                              setRestoreLimit(plan.autoAdjustLimit)
                              onCancelOpen()
                            }}
                          />
                        </Tooltip>
                      )}
                    </Td>
                  </Tr>
                ))}
              </Tbody>
            </Table>
          )}
        </Box>
      </MotionBox>

      {/* 取消确认弹窗 */}
      <Modal isOpen={isCancelOpen} onClose={onCancelClose}>
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{t('positionPlan.cancelConfirm')}</ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <Text mb={4}>{t('positionPlan.cancelConfirmMessage')}</Text>
            <FormControl display="flex" alignItems="center">
              <FormLabel mb="0">{t('positionPlan.restoreLimit')}</FormLabel>
              <Switch isChecked={restoreLimit} onChange={(e) => setRestoreLimit(e.target.checked)} />
            </FormControl>
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" mr={3} onClick={onCancelClose}>
              {t('common.cancel')}
            </Button>
            <Button colorScheme="red" onClick={handleCancelPlan}>
              {t('positionPlan.confirmCancel')}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </Box>
  )
}

export default PositionPlanPage
