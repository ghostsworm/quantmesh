import React, { useState, useEffect } from 'react'
import {
  Box,
  Button,
  Card,
  CardBody,
  CardHeader,
  VStack,
  HStack,
  Text,
  Heading,
  Badge,
  IconButton,
  useToast,
  Divider,
  SimpleGrid,
  FormControl,
  FormLabel,
  Input,
  NumberInput,
  NumberInputField,
  Select,
  Switch,
  Alert,
  AlertIcon,
  Collapse,
  Flex,
  Spacer,
  Tooltip,
} from '@chakra-ui/react'
import { AddIcon, DeleteIcon, EditIcon, CheckIcon, CloseIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import {
  BotConfigFile,
  BotStrategyConfig,
  getBotConfigFile,
  updateBotStrategyConfig,
  addBotStrategy,
  removeBotStrategy,
} from '../services/api'

interface MultiStrategyConfigProps {
  botId: string
  botRunning: boolean
  onConfigUpdate?: () => void
}

// 策略类型定义
const STRATEGY_TYPES = {
  grid: {
    label: '网格策略',
    category: 'grid',
    description: '在价格区间内挂单赚取差价',
    params: {
      grid_spacing: { label: '网格间距', type: 'number', default: 100 },
      grid_levels: { label: '网格层数', type: 'number', default: 10 },
    },
  },
  trend_following: {
    label: '趋势跟踪',
    category: 'grid',
    description: '跟踪市场趋势动态调整',
    params: {
      trend_period: { label: '趋势周期', type: 'number', default: 60 },
      trend_threshold: { label: '趋势阈值', type: 'number', default: 0.5 },
    },
  },
  momentum: {
    label: '动量策略',
    category: 'grid',
    description: '基于价格动量进行交易',
    params: {
      momentum_period: { label: '动量周期', type: 'number', default: 14 },
      momentum_threshold: { label: '动量阈值', type: 'number', default: 0.02 },
    },
  },
  mean_reversion: {
    label: '均值回归',
    category: 'grid',
    description: '价格偏离均值时进行交易',
    params: {
      mean_period: { label: '均值周期', type: 'number', default: 20 },
      std_dev_threshold: { label: '标准差阈值', type: 'number', default: 2 },
    },
  },
  dca: {
    label: '定投策略',
    category: 'dca',
    description: '定期定额买入',
    params: {
      dca_amount: { label: '定投金额', type: 'number', default: 100 },
      dca_interval: { label: '定投间隔(分钟)', type: 'number', default: 60 },
    },
  },
  martingale: {
    label: '马丁格尔',
    category: 'dca',
    description: '亏损后加倍投入',
    params: {
      base_amount: { label: '基础金额', type: 'number', default: 50 },
      multiplier: { label: '倍增系数', type: 'number', default: 2 },
      max_levels: { label: '最大层数', type: 'number', default: 5 },
    },
  },
}

const MultiStrategyConfig: React.FC<MultiStrategyConfigProps> = ({ botId, botRunning, onConfigUpdate }) => {
  const { t } = useTranslation()
  const toast = useToast()
  const [config, setConfig] = useState<BotConfigFile | null>(null)
  const [loading, setLoading] = useState(true)
  const [editingIndex, setEditingIndex] = useState<number | null>(null)
  const [addingStrategy, setAddingStrategy] = useState(false)

  // 加载配置
  const loadConfig = async () => {
    try {
      setLoading(true)
      const response = await getBotConfigFile(botId)
      setConfig(response.config)
    } catch (error) {
      console.error('Failed to load bot config:', error)
      toast({
        title: t('bot.failed_to_load_config'),
        description: error instanceof Error ? error.message : String(error),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadConfig()
  }, [botId])

  // 添加策略
  const handleAddStrategy = async (strategyType: string) => {
    if (!config || botRunning) {
      if (botRunning) {
        toast({
          title: t('bot.cannot_modify_while_running'),
          status: 'warning',
          duration: 3000,
          isClosable: true,
        })
      }
      return
    }

    try {
      const newStrategy: BotStrategyConfig = {
        type: strategyType,
        enabled: true,
        weight: 1.0,
        params: {},
        settings: {},
      }

      // 设置默认参数
      const strategyDef = STRATEGY_TYPES[strategyType as keyof typeof STRATEGY_TYPES]
      if (strategyDef?.params) {
        Object.entries(strategyDef.params).forEach(([key, param]) => {
          newStrategy.params![key] = param.default
        })
      }

      const result = await addBotStrategy(botId, newStrategy)
      toast({
        title: t('bot.strategy_added'),
        description: t('bot.strategy_added_desc', { type: strategyDef?.label }),
        status: 'success',
        duration: 3000,
        isClosable: true,
      })

      await loadConfig()
      onConfigUpdate?.()
    } catch (error) {
      console.error('Failed to add strategy:', error)
      toast({
        title: t('bot.failed_to_add_strategy'),
        description: error instanceof Error ? error.message : String(error),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    }
  }

  // 移除策略
  const handleRemoveStrategy = async (index: number) => {
    if (!config || botRunning) {
      return
    }

    if (config.strategies.length <= 1) {
      toast({
        title: t('bot.cannot_remove_last_strategy'),
        status: 'warning',
        duration: 3000,
        isClosable: true,
      })
      return
    }

    try {
      await removeBotStrategy(botId, index)
      toast({
        title: t('bot.strategy_removed'),
        status: 'success',
        duration: 3000,
        isClosable: true,
      })

      await loadConfig()
      onConfigUpdate?.()
    } catch (error) {
      console.error('Failed to remove strategy:', error)
      toast({
        title: t('bot.failed_to_remove_strategy'),
        description: error instanceof Error ? error.message : String(error),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    }
  }

  // 更新策略权重
  const handleUpdateWeight = async (index: number, weight: number) => {
    if (!config || botRunning) {
      return
    }

    try {
      const strategy = { ...config.strategies[index], weight }
      await updateBotStrategyConfig(botId, index, strategy)

      // 更新本地状态
      const newStrategies = [...config.strategies]
      newStrategies[index] = strategy
      setConfig({ ...config, strategies: newStrategies })

      onConfigUpdate?.()
    } catch (error) {
      console.error('Failed to update strategy weight:', error)
      toast({
        title: t('bot.failed_to_update_weight'),
        description: error instanceof Error ? error.message : String(error),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    }
  }

  // 切换策略启用状态
  const handleToggleEnabled = async (index: number) => {
    if (!config || botRunning) {
      return
    }

    try {
      const strategy = { ...config.strategies[index], enabled: !config.strategies[index].enabled }
      await updateBotStrategyConfig(botId, index, strategy)

      // 更新本地状态
      const newStrategies = [...config.strategies]
      newStrategies[index] = strategy
      setConfig({ ...config, strategies: newStrategies })

      onConfigUpdate?.()
    } catch (error) {
      console.error('Failed to toggle strategy:', error)
      toast({
        title: t('bot.failed_to_toggle_strategy'),
        description: error instanceof Error ? error.message : String(error),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    }
  }

  if (loading) {
    return (
      <Card>
        <CardBody>
          <Flex justify="center" py={8}>
            <Text>{t('common.loading')}</Text>
          </Flex>
        </CardBody>
      </Card>
    )
  }

  if (!config) {
    return (
      <Card>
        <CardBody>
          <Alert status="warning">
            <AlertIcon />
            {t('bot.config_not_loaded')}
          </Alert>
        </CardBody>
      </Card>
    )
  }

  const totalWeight = config.strategies.reduce((sum, s) => sum + (s.enabled ? s.weight : 0), 0)
  const isMultiMode = config.strategy_mode === 'multi'

  return (
    <Card>
      <CardHeader>
        <Flex justify="space-between" align="center">
          <Box>
            <Heading size="md">{t('bot.strategy_config')}</Heading>
            <Text fontSize="sm" color="gray.500" mt={1}>
              {isMultiMode ? t('bot.multi_strategy_mode') : t('bot.single_strategy_mode')}
            </Text>
          </Box>
          <Badge colorScheme={isMultiMode ? 'blue' : 'green'}>
            {isMultiMode ? 'Multi-Strategy' : 'Single-Strategy'}
          </Badge>
        </Flex>
      </CardHeader>

      <CardBody>
        <VStack spacing={4} align="stretch">
          {/* 策略模式提示 */}
          {config.strategies.length > 1 && (
            <Alert status="info">
              <AlertIcon />
              <Box>
                <Text fontWeight="bold">{t('bot.multi_strategy_active')}</Text>
                <Text fontSize="sm">{t('bot.multi_strategy_desc')}</Text>
              </Box>
            </Alert>
          )}

          {/* 策略列表 */}
          <VStack spacing={3} align="stretch">
            {config.strategies.map((strategy, index) => {
              const strategyDef = STRATEGY_TYPES[strategy.type as keyof typeof STRATEGY_TYPES]
              const weightPercent = totalWeight > 0 ? ((strategy.weight / totalWeight) * 100).toFixed(1) : '0'

              return (
                <Card key={index} variant="outline" size="sm">
                  <CardBody p={4}>
                    <VStack spacing={3} align="stretch">
                      {/* 策略头部 */}
                      <Flex justify="space-between" align="center">
                        <HStack spacing={3}>
                          <Badge colorScheme={strategy.enabled ? 'green' : 'gray'}>
                            {strategyDef?.label || strategy.type}
                          </Badge>
                          {strategy.enabled && (
                            <Badge colorScheme="blue">{weightPercent}%</Badge>
                          )}
                          <Switch
                            isChecked={strategy.enabled}
                            onChange={() => handleToggleEnabled(index)}
                            isDisabled={botRunning}
                            size="sm"
                          />
                        </HStack>
                        {!botRunning && config.strategies.length > 1 && (
                          <IconButton
                            icon={<DeleteIcon />}
                            aria-label={t('bot.remove_strategy')}
                            size="sm"
                            colorScheme="red"
                            variant="ghost"
                            onClick={() => handleRemoveStrategy(index)}
                          />
                        )}
                      </Flex>

                      {/* 策略参数 */}
                      <Collapse in={strategy.enabled}>
                        <VStack spacing={2} align="stretch" pl={4}>
                          <Text fontSize="sm" color="gray.600">
                            {strategyDef?.description}
                          </Text>

                          {/* 权重调整 */}
                          <HStack spacing={4}>
                            <FormControl>
                              <FormLabel fontSize="sm">{t('bot.weight')}</FormLabel>
                              <NumberInput
                                size="sm"
                                value={strategy.weight}
                                min={0.1}
                                max={1}
                                step={0.1}
                                onChange={(_, value) => handleUpdateWeight(index, value)}
                                isDisabled={botRunning}
                              >
                                <NumberInputField />
                                <NumberInputStepper>
                                  <NumberIncrementStepper />
                                  <NumberDecrementStepper />
                                </NumberInputStepper>
                              </NumberInput>
                            </FormControl>
                          </HStack>

                          {/* 策略特定参数 */}
                          {strategyDef?.params && (
                            <SimpleGrid columns={2} spacing={2}>
                              {Object.entries(strategyDef.params).map(([key, param]) => (
                                <FormControl key={key} size="sm">
                                  <FormLabel fontSize="xs">{param.label}</FormLabel>
                                  <NumberInput
                                    size="sm"
                                    value={(strategy.params?.[key] as number) || param.default}
                                    onChange={(_, value) => {
                                      const newParams = { ...strategy.params, [key]: value }
                                      handleUpdateWeight(index, strategy.weight)
                                    }}
                                    isDisabled={botRunning}
                                  >
                                    <NumberInputField />
                                  </NumberInput>
                                </FormControl>
                              ))}
                            </SimpleGrid>
                          )}
                        </VStack>
                      </Collapse>
                    </VStack>
                  </CardBody>
                </Card>
              )
            })}
          </VStack>

          {/* 添加策略按钮 */}
          {!botRunning && (
            <Box>
              <Collapse in={addingStrategy}>
                <Card size="sm" mb={3}>
                  <CardBody>
                    <VStack spacing={2} align="stretch">
                      <Text fontWeight="bold">{t('bot.select_strategy_type')}</Text>
                      <SimpleGrid columns={2} spacing={2}>
                        {Object.entries(STRATEGY_TYPES).map(([type, def]) => (
                          <Button
                            key={type}
                            size="sm"
                            variant="outline"
                            justifyContent="flex-start"
                            onClick={() => {
                              handleAddStrategy(type)
                              setAddingStrategy(false)
                            }}
                          >
                            <VStack align="start" spacing={0}>
                              <Text fontWeight="bold">{def.label}</Text>
                              <Text fontSize="xs" color="gray.500">
                                {def.category}
                              </Text>
                            </VStack>
                          </Button>
                        ))}
                      </SimpleGrid>
                      <Button size="sm" variant="ghost" onClick={() => setAddingStrategy(false)}>
                        {t('common.cancel')}
                      </Button>
                    </VStack>
                  </CardBody>
                </Card>
              </Collapse>

              <Button
                leftIcon={<AddIcon />}
                onClick={() => setAddingStrategy(!addingStrategy)}
                isDisabled={addingStrategy}
                colorScheme="blue"
                width="full"
              >
                {addingStrategy ? t('common.cancel') : t('bot.add_strategy')}
              </Button>
            </Box>
          )}

          {botRunning && (
            <Alert status="warning" fontSize="sm">
              <AlertIcon />
              {t('bot.stop_to_modify_strategies')}
            </Alert>
          )}
        </VStack>
      </CardBody>
    </Card>
  )
}

export default MultiStrategyConfig
