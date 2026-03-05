import React, { useState, useEffect } from 'react'
import {
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalFooter,
  ModalBody,
  ModalCloseButton,
  Button,
  FormControl,
  FormLabel,
  Input,
  Box,
  Text,
  Progress,
  useToast,
  Badge,
  Tabs,
  TabList,
  TabPanels,
  Tab,
  TabPanel,
  SimpleGrid,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Flex,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import {
  createBotBacktest,
  getBotBacktestTask,
  getBotBacktestResult,
  type BotBacktestRequest,
  type BotBacktestTask,
  type BotBacktestResult,
} from '../services/api'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'

interface BotBacktestDialogProps {
  open: boolean
  onClose: () => void
  botId: string
  botName: string
  botConfig?: any
}

const BotBacktestDialog: React.FC<BotBacktestDialogProps> = ({
  open,
  onClose,
  botId,
  botName,
  botConfig,
}) => {
  const { t } = useTranslation()
  const toast = useToast()
  const [tabIndex, setTabIndex] = useState(0)
  const [loading, setLoading] = useState(false)
  const [task, setTask] = useState<BotBacktestTask | null>(null)
  const [result, setResult] = useState<BotBacktestResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pollInterval, setPollInterval] = useState<NodeJS.Timeout | null>(null)

  // 表單狀態
  const [formData, setFormData] = useState<BotBacktestRequest>({
    bot_id: botId,
    start_date: new Date(Date.now() - 90 * 24 * 60 * 60 * 1000).toISOString().split('T')[0],
    end_date: new Date().toISOString().split('T')[0],
    data_dir: './data',
    commission: 0.0004,
    leverage: 1,
  })

  useEffect(() => {
    return () => {
      if (pollInterval) {
        clearInterval(pollInterval)
      }
    }
  }, [pollInterval])

  const handleCreateBacktest = async () => {
    setLoading(true)
    setError(null)
    setTask(null)
    setResult(null)

    try {
      const response = await createBotBacktest(botId, formData)
      setTask({
        task_id: response.task_id,
        bot_id: botId,
        status: 'pending',
        created_at: new Date().toISOString(),
        progress: 0,
      })

      startPolling(response.task_id)
      setTabIndex(1)
    } catch (err: any) {
      setError(err.message || t('backtest.createFailed'))
      setLoading(false)
      toast({
        title: t('backtest.createFailed'),
        description: err.message,
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    }
  }

  const startPolling = (taskId: string) => {
    let currentInterval = 2000
    const maxInterval = 10000

    const poll = async () => {
      try {
        const taskStatus = await getBotBacktestTask(taskId)
        setTask(taskStatus)

        if (taskStatus.status === 'completed') {
          clearInterval(pollInterval!)
          setLoading(false)
          const backtestResult = await getBotBacktestResult(taskId)
          setResult(backtestResult)
          toast({
            title: t('backtest.executionCompleted'),
            status: 'success',
            duration: 3000,
            isClosable: true,
          })
        } else if (taskStatus.status === 'failed') {
          clearInterval(pollInterval!)
          setLoading(false)
          setError(taskStatus.error || t('backtest.executionFailed'))
          toast({
            title: t('backtest.executionFailed'),
            description: taskStatus.error,
            status: 'error',
            duration: 5000,
            isClosable: true,
          })
        } else {
          // 指數退避
          currentInterval = Math.min(currentInterval * 1.5, maxInterval)
        }
      } catch (err: any) {
        console.error('Failed to poll backtest status:', err)
      }
    }

    const interval = setInterval(poll, currentInterval)
    setPollInterval(interval)
  }

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'completed':
        return 'green'
      case 'failed':
        return 'red'
      case 'running':
        return 'blue'
      default:
        return 'gray'
    }
  }

  const renderConfigForm = () => (
    <Box display="flex" flexDirection="column" gap={4}>
      <Text fontSize="lg" fontWeight="semibold">
        {t('backtest.configureParameters')}
      </Text>

      <SimpleGrid columns={2} spacing={4}>
        <FormControl>
          <FormLabel>{t('backtest.startDate')}</FormLabel>
          <Input
            type="date"
            value={formData.start_date?.split('T')[0] || ''}
            onChange={(e) => setFormData({ ...formData, start_date: e.target.value })}
          />
        </FormControl>

        <FormControl>
          <FormLabel>{t('backtest.endDate')}</FormLabel>
          <Input
            type="date"
            value={formData.end_date?.split('T')[0] || ''}
            onChange={(e) => setFormData({ ...formData, end_date: e.target.value })}
          />
        </FormControl>

        <FormControl>
          <FormLabel>{t('backtest.commissionRate')}</FormLabel>
          <Input
            type="number"
            step={0.0001}
            min={0}
            value={formData.commission}
            onChange={(e) => setFormData({ ...formData, commission: parseFloat(e.target.value) })}
          />
        </FormControl>

        <FormControl>
          <FormLabel>{t('backtest.leverage')}</FormLabel>
          <Input
            type="number"
            step={0.1}
            min={1}
            value={formData.leverage}
            onChange={(e) => setFormData({ ...formData, leverage: parseFloat(e.target.value) })}
          />
        </FormControl>

        <FormControl gridColumn="span 2">
          <FormLabel>{t('backtest.dataDirectory')}</FormLabel>
          <Input
            value={formData.data_dir}
            onChange={(e) => setFormData({ ...formData, data_dir: e.target.value })}
          />
          <Text fontSize="sm" color="gray.500" mt={1}>
            {t('backtest.dataDirectoryHelp')}
          </Text>
        </FormControl>
      </SimpleGrid>

      {error && (
        <Alert status="error">
          <AlertIcon />
          <AlertTitle mr={2}>{t('common.error')}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
    </Box>
  )

  const renderResults = () => {
    if (!task) {
      return (
        <Flex justify="center" py={8}>
          <Text color="gray.500">{t('backtest.noTask')}</Text>
        </Flex>
      )
    }

    return (
      <Box display="flex" flexDirection="column" gap={4}>
        {/* 任務狀態 */}
        <Flex align="center" gap={3}>
          <Badge colorScheme={getStatusColor(task.status)} fontSize="sm" px={2} py={1}>
            {task.status.toUpperCase()}
          </Badge>
          <Text fontSize="sm" color="gray.600">
            {t('backtest.taskId')}: {task.task_id}
          </Text>
        </Flex>

        {/* 進度條 */}
        {task.status === 'running' && (
          <Box>
            <Text fontSize="sm" mb={2}>
              {t('backtest.progress')}: {task.progress.toFixed(1)}%
            </Text>
            <Progress value={task.progress} colorScheme="blue" size="sm" borderRadius="md" />
          </Box>
        )}

        {/* 錯誤信息 */}
        {task.status === 'failed' && task.error && (
          <Alert status="error">
            <AlertIcon />
            <AlertDescription>{task.error}</AlertDescription>
          </Alert>
        )}

        {/* 回測結果 */}
        {result && (
          <>
            {/* 關鍵指標 */}
            <Box p={4} bg="gray.50" borderRadius="lg">
              <Text fontSize="lg" fontWeight="semibold" mb={4}>
                {t('backtest.summary')}
              </Text>
              <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
                <Stat>
                  <StatLabel>{t('backtest.totalReturn')}</StatLabel>
                  <StatNumber color={result.total_return_pct >= 0 ? 'green.500' : 'red.500'}>
                    {result.total_return_pct.toFixed(2)}%
                  </StatNumber>
                </Stat>

                <Stat>
                  <StatLabel>{t('backtest.totalTrades')}</StatLabel>
                  <StatNumber>{result.total_trades}</StatNumber>
                </Stat>

                <Stat>
                  <StatLabel>{t('backtest.maxDrawdown')}</StatLabel>
                  <StatNumber color="red.500">
                    {result.risk_metrics.max_drawdown_pct.toFixed(2)}%
                  </StatNumber>
                </Stat>

                <Stat>
                  <StatLabel>{t('backtest.winRate')}</StatLabel>
                  <StatNumber>{result.risk_metrics.win_rate.toFixed(1)}%</StatNumber>
                </Stat>

                <Stat>
                  <StatLabel>{t('backtest.sharpeRatio')}</StatLabel>
                  <StatNumber>{result.risk_metrics.sharpe_ratio.toFixed(2)}</StatNumber>
                </Stat>

                <Stat>
                  <StatLabel>{t('backtest.profitFactor')}</StatLabel>
                  <StatNumber>{result.risk_metrics.profit_factor.toFixed(2)}</StatNumber>
                </Stat>

                <Stat>
                  <StatLabel>{t('backtest.totalFees')}</StatLabel>
                  <StatNumber>${result.total_fees.toFixed(2)}</StatNumber>
                </Stat>

                <Stat>
                  <StatLabel>{t('backtest.totalSlippage')}</StatLabel>
                  <StatNumber>${result.total_slippage.toFixed(2)}</StatNumber>
                </Stat>
              </SimpleGrid>
            </Box>

            {/* 權益曲線 */}
            <Box p={4} bg="gray.50" borderRadius="lg">
              <Text fontSize="lg" fontWeight="semibold" mb={4}>
                {t('backtest.equityCurve')}
              </Text>
              <Box height="300px">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={result.equity_curve}>
                    <CartesianGrid strokeDasharray="3 3" />
                    <XAxis
                      dataKey="timestamp"
                      tickFormatter={(v) => new Date(v).toLocaleDateString()}
                      label={{ value: t('backtest.date'), position: 'insideBottom', offset: -5 }}
                    />
                    <YAxis
                      label={{ value: t('backtest.equity'), angle: -90, position: 'insideLeft' }}
                    />
                    <Tooltip
                      labelFormatter={(v) => new Date(v).toLocaleString()}
                      formatter={(value: number) => [`$${value.toFixed(2)}`, t('backtest.equity')]}
                    />
                    <Line
                      type="monotone"
                      dataKey="equity"
                      stroke="#3182ce"
                      strokeWidth={2}
                      dot={false}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </Box>
            </Box>

            {/* 策略統計 */}
            {Object.keys(result.stats_by_strategy).length > 0 && (
              <Box p={4} bg="gray.50" borderRadius="lg">
                <Text fontSize="lg" fontWeight="semibold" mb={4}>
                  {t('backtest.strategyStats')}
                </Text>
                <Table size="sm">
                  <Thead>
                    <Tr>
                      <Th>{t('backtest.strategy')}</Th>
                      <Th isNumeric>{t('backtest.trades')}</Th>
                      <Th isNumeric>{t('backtest.pnl')}</Th>
                      <Th isNumeric>{t('backtest.winRate')}</Th>
                      <Th isNumeric>{t('backtest.drawdown')}</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {Object.entries(result.stats_by_strategy).map(([key, stats]) => (
                      <Tr key={key}>
                        <Td>{stats.name}</Td>
                        <Td isNumeric>{stats.total_trades}</Td>
                        <Td isNumeric>
                          <Text color={stats.realized_pnl >= 0 ? 'green.500' : 'red.500'}>
                            ${stats.realized_pnl.toFixed(2)}
                          </Text>
                        </Td>
                        <Td isNumeric>{stats.win_rate.toFixed(1)}%</Td>
                        <Td isNumeric>{stats.max_drawdown.toFixed(2)}%</Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </Box>
            )}
          </>
        )}
      </Box>
    )
  }

  return (
    <Modal isOpen={open} onClose={onClose} size="xl" scrollBehavior="inside">
      <ModalOverlay />
      <ModalContent>
        <ModalHeader>
          {t('backtest.title')}: {botName}
        </ModalHeader>
        <ModalCloseButton />

        <ModalBody pb={6}>
          <Tabs index={tabIndex} onChange={(index) => setTabIndex(index)}>
            <TabList>
              <Tab>{t('backtest.config')}</Tab>
              <Tab>{t('backtest.results')}</Tab>
            </TabList>

            <TabPanels>
              <TabPanel p={0}>{renderConfigForm()}</TabPanel>
              <TabPanel p={0}>{renderResults()}</TabPanel>
            </TabPanels>
          </Tabs>
        </ModalBody>

        <ModalFooter>
          <Button onClick={onClose} mr={3}>
            {t('common.close')}
          </Button>
          {tabIndex === 0 && (
            <Button
              colorScheme="blue"
              onClick={handleCreateBacktest}
              isDisabled={loading}
              isLoading={loading}
            >
              {loading ? t('backtest.running') : t('backtest.start')}
            </Button>
          )}
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}

export default BotBacktestDialog
