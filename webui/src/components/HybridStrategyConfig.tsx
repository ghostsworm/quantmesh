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
  Alert,
  AlertIcon,
  Divider,
  SimpleGrid,
  FormControl,
  FormLabel,
  Input,
  Select,
  Switch,
  Collapse,
  Flex,
  Spacer,
  Tooltip,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalFooter,
  ModalBody,
  ModalCloseButton,
  Code,
  useDisclosure,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
  Tag,
  TagLabel,
  Wrap,
} from '@chakra-ui/react'
import {
  AddIcon,
  DeleteIcon,
  EditIcon,
  CheckIcon,
  CloseIcon,
  InfoIcon,
  SettingsIcon,
  ArrowForwardIcon,
} from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import {
  getHybridStrategyConfig,
  updateHybridStrategy,
  enableHybridMode,
  disableHybridMode,
  getHybridStrategyStatus,
  getBuiltInRuleTemplates,
  BotConfigFile,
  HybridStrategyConfig,
} from '../services/api'
import DecimalNumberInput from './DecimalNumberInput'

interface HybridStrategyConfigProps {
  botId: string
  botName: string
  botRunning: boolean
  onConfigUpdate?: () => void
}

// 子策略配置卡片
const SubStrategyCard: React.FC<{
  subStrategy: any
  onUpdate?: (id: string, config: any) => void
  onRemove?: (id: string) => void
  botRunning: boolean
}> = ({ subStrategy, onUpdate, onRemove, botRunning }) => {
  const { t } = useTranslation()

  return (
    <Card size="sm" variant="outline">
      <CardBody>
        <VStack spacing={3} align="stretch">
          {/* 头部 */}
          <Flex justify="space-between" align="center">
            <HStack spacing={2}>
              <Badge colorScheme={subStrategy.enabled ? 'green' : 'gray'}>
                {subStrategy.role}
              </Badge>
              <Text fontWeight="bold">{subStrategy.name || subStrategy.type}</Text>
            </HStack>
            {!botRunning && onRemove && (
              <IconButton
                icon={<DeleteIcon />}
                aria-label="Remove"
                size="sm"
                colorScheme="red"
                variant="ghost"
                onClick={() => onRemove(subStrategy.id)}
              />
            )}
          </Flex>

          {/* 参数配置 */}
          <SimpleGrid columns={2} spacing={2}>
            <FormControl size="sm">
              <FormLabel fontSize="xs">权重</FormLabel>
              <DecimalNumberInput
                size="sm"
                value={subStrategy.weight}
                min={0}
                max={1}
                step={0.1}
                isDisabled={botRunning}
                onChange={(v) => {
                  if (onUpdate) {
                    const num = typeof v === 'number' ? v : parseFloat(String(v)) || 0
                    onUpdate(subStrategy.id, { ...subStrategy, weight: num })
                  }
                }}
              />
            </FormControl>

            <FormControl size="sm" display="flex" alignItems="center" gap={2}>
              <FormLabel fontSize="xs">启用</FormLabel>
              <Switch
                isChecked={subStrategy.enabled}
                isDisabled={botRunning}
                onChange={(e) => {
                  if (onUpdate) {
                    onUpdate(subStrategy.id, { ...subStrategy, enabled: e.target.checked })
                  }
                }}
              />
            </FormControl>
          </SimpleGrid>

          {/* 策略参数 */}
          {Object.keys(subStrategy.config || {}).length > 0 && (
            <Collapse in={true}>
              <VStack align="stretch" spacing={2} pl={4}>
                <Text fontSize="xs" fontWeight="bold" color="gray.500">
                  策略参数
                </Text>
                {Object.entries(subStrategy.config || {}).map(([key, value]) => (
                  <FormControl key={key} size="sm">
                    <FormLabel fontSize="xs">{key}</FormLabel>
                    <Input
                      size="sm"
                      value={String(value)}
                      isDisabled={botRunning}
                      onChange={(e) => {
                        if (onUpdate) {
                          onUpdate(subStrategy.id, {
                            ...subStrategy,
                            config: { ...subStrategy.config, [key]: e.target.value }
                          })
                        }
                      }}
                    />
                  </FormControl>
                ))}
              </VStack>
            </Collapse>
          )}
        </VStack>
      </CardBody>
    </Card>
  )
}

// 协作规则卡片
const CollaborationRuleCard: React.FC<{
  rule: any
  onEdit?: (rule: any) => void
  onRemove?: (id: string) => void
  botRunning: boolean
}> = ({ rule, onEdit, onRemove, botRunning }) => {
  const { t } = useTranslation()

  return (
    <Card size="sm" variant={rule.enabled ? "outline" : "filled"}>
      <CardBody>
        <VStack spacing={2} align="stretch">
          <Flex justify="space-between" align="center">
            <HStack spacing={2}>
              <Badge colorScheme={rule.enabled ? 'blue' : 'gray'}>
                优先级 {rule.priority}
              </Badge>
              <Text fontWeight="bold" fontSize="sm">
                {rule.name}
              </Text>
            </HStack>
            <HStack spacing={1}>
              <Switch
                size="sm"
                isChecked={rule.enabled}
                isDisabled={botRunning}
                onChange={() => {
                  if (onEdit) {
                    onEdit({ ...rule, enabled: !rule.enabled })
                  }
                }}
              />
              {!botRunning && onRemove && (
                <IconButton
                  icon={<DeleteIcon />}
                  aria-label="Remove rule"
                  size="sm"
                  colorScheme="red"
                  variant="ghost"
                  onClick={() => onRemove(rule.id)}
                />
              )}
            </HStack>
          </Flex>

          {rule.description && (
            <Text fontSize="xs" color="gray.600">
              {rule.description}
            </Text>
          )}

          {/* 条件 */}
          <Box pl={4}>
            <Text fontSize="xs" color="gray.500">
              <strong>条件:</strong> 当 {rule.when?.source_strategy} 的 {rule.when?.signal_type}
              {rule.when?.operator} {String(rule.when?.value)} 时
            </Text>
          </Box>

          {/* 动作 */}
          <Box pl={4}>
            <Text fontSize="xs" color="gray.500" mb={1}>
              <strong>执行动作:</strong>
            </Text>
            <VStack align="stretch" spacing={1}>
              {(rule.then || []).map((action: any, index: number) => (
                <Text key={index} fontSize="xs" color="gray.700">
                  {index + 1}. {action.target_strategy}: {action.operation}
                </Text>
              ))}
            </VStack>
          </Box>
        </VStack>
      </CardBody>
    </Card>
  )
}

const HybridStrategyConfig: React.FC<HybridStrategyConfigProps> = ({
  botId,
  botName,
  botRunning,
  onConfigUpdate
}) => {
  const { t } = useTranslation()
  const toast = useToast()
  const {
    isOpen: isRuleTemplatesOpen,
    onOpen: onRuleTemplatesOpen,
    onClose: onRuleTemplatesClose
  } = useDisclosure()

  const [config, setConfig] = useState<HybridStrategyConfig | null>(null)
  const [status, setStatus] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const [ruleTemplates, setRuleTemplates] = useState<any[]>([])

  // 加载配置
  const loadConfig = async () => {
    try {
      setLoading(true)
      const [configResp, statusResp] = await Promise.all([
        getHybridStrategyConfig(botId),
        getHybridStrategyStatus(botId),
      ])

      if (configResp.hybrid_mode) {
        setConfig(configResp.config)
      }
      setStatus(statusResp)
    } catch (error) {
      console.error('Failed to load hybrid config:', error)
    } finally {
      setLoading(false)
    }
  }

  // 加载内置规则模板
  const loadRuleTemplates = async () => {
    try {
      const resp = await getBuiltInRuleTemplates()
      setRuleTemplates(resp.templates)
    } catch (error) {
      console.error('Failed to load rule templates:', error)
      toast({
        title: '加载规则模板失败',
        status: 'error',
        duration: 3000,
      })
    }
  }

  useEffect(() => {
    loadConfig()
  }, [botId])

  // 启用混合模式
  const handleEnableHybrid = async () => {
    try {
      // 使用默认配置创建混合策略
      const defaultConfig: any = {
        name: `${botName} 混合策略`,
        description: '网格策略 + 趋势过滤',
        sub_strategies: [
          {
            id: 'grid_primary',
            name: '主策略-网格',
            type: 'grid',
            role: 'primary',
            weight: 1.0,
            enabled: true,
            config: {
              price_interval: 100,
              order_quantity: 50,
            },
          },
          {
            id: 'trend_signal',
            name: '信号策略-趋势',
            type: 'trend_following',
            role: 'signal',
            weight: 0.0,
            enabled: true,
            config: {
              trend_period: 60,
              trend_threshold: 0.5,
            },
          },
        ],
        collaboration_rules: [
          {
            id: 'trend_filter_long',
            name: '趋势过滤-做多',
            description: '当趋势向下时，阻止做多开仓',
            priority: 100,
            enabled: true,
            when: {
              source_strategy: 'trend_signal',
              signal_type: 'trend_direction',
              operator: '==',
              value: 'down',
            },
            then: [
              {
                target_strategy: 'grid_primary',
                operation: 'deny_open',
                condition: "direction == 'LONG'",
              },
            ],
          },
        ],
      }

      await enableHybridMode(botId, defaultConfig)

      toast({
        title: '已启用混合模式',
        description: '网格策略 + 趋势过滤',
        status: 'success',
        duration: 3000,
      })

      await loadConfig()
      onConfigUpdate?.()
    } catch (error) {
      console.error('Failed to enable hybrid mode:', error)
      toast({
        title: '启用混合模式失败',
        description: error instanceof Error ? error.message : String(error),
        status: 'error',
        duration: 5000,
      })
    }
  }

  // 禁用混合模式
  const handleDisableHybrid = async () => {
    try {
      await disableHybridMode(botId)

      toast({
        title: '已禁用混合模式',
        status: 'success',
        duration: 3000,
      })

      await loadConfig()
      onConfigUpdate?.()
    } catch (error) {
      console.error('Failed to disable hybrid mode:', error)
      toast({
        title: '禁用混合模式失败',
        description: error instanceof Error ? error.message : String(error),
        status: 'error',
        duration: 5000,
      })
    }
  }

  // 添加规则模板
  const handleAddRuleTemplate = (template: any) => {
    if (!config) return

    const newRule = { ...template, id: `custom_${Date.now()}` }
    setConfig({
      ...config,
      collaboration_rules: [...config.collaboration_rules, newRule],
    })
  }

  if (loading) {
    return (
      <Card>
        <CardBody>
          <Flex justify="center" py={8}>
            <Text>加载中...</Text>
          </Flex>
        </CardBody>
      </Card>
    )
  }

  const isHybrid = status?.hybrid_mode
  const enabledSubStrategies = status?.enabled_sub_strategies || 0
  const enabledRules = status?.enabled_rules || 0

  return (
    <Card>
      <CardHeader>
        <Flex justify="space-between" align="center">
          <Box>
            <Heading size="md">混合策略模式</Heading>
            <Text fontSize="sm" color="gray.500" mt={1}>
              策略协作与条件联动
            </Text>
          </Box>
          <Badge colorScheme={isHybrid ? 'purple' : 'gray'} fontSize="lg">
            {isHybrid ? '已启用' : '未启用'}
          </Badge>
        </Flex>
      </CardHeader>

      <CardBody>
        <VStack spacing={4} align="stretch">
          {/* 说明 */}
          <Alert status="info">
            <AlertIcon />
            <Box>
              <Text fontWeight="bold">什么是混合策略？</Text>
              <Text fontSize="sm">
                混合策略允许您组合多个策略，并通过协作规则实现条件联动。
                例如：网格策略 + 趋势过滤，当趋势向下时阻止做多开仓。
              </Text>
            </Box>
          </Alert>

          {/* 状态和操作 */}
          <HStack spacing={4} width="100%">
            {isHybrid ? (
              <>
                <VStack align="start" spacing={0}>
                  <Text fontSize="sm" color="gray.500">子策略</Text>
                  <Text fontSize="lg" fontWeight="bold">
                    {enabledSubStrategies}/{status?.sub_strategies_count || 0}
                  </Text>
                </VStack>
                <VStack align="start" spacing={0}>
                  <Text fontSize="sm" color="gray.500">协作规则</Text>
                  <Text fontSize="lg" fontWeight="bold">
                    {enabledRules}/{status?.rules_count || 0}
                  </Text>
                </VStack>
                <Spacer />
                {!botRunning && (
                  <Button
                    colorScheme="red"
                    variant="outline"
                    onClick={handleDisableHybrid}
                  >
                    禁用混合模式
                  </Button>
                )}
              </>
            ) : (
              <>
                <Text fontSize="sm" color="gray.500">
                  启用混合模式以配置多策略协作
                </Text>
                <Spacer />
                <Button
                  colorScheme="purple"
                  leftIcon={<SettingsIcon />}
                  onClick={handleEnableHybrid}
                  isDisabled={botRunning}
                >
                  启用混合模式
                </Button>
              </>
            )}
          </HStack>

          {/* 混合策略配置 */}
          {isHybrid && config && (
            <>
              <Divider />

              {/* 子策略列表 */}
              <Box>
                <HStack justify="space-between" align="center" mb={3}>
                  <Text fontWeight="bold">子策略</Text>
                </HStack>
                <VStack spacing={3}>
                  {config.sub_strategies.map((subStrategy: any) => (
                    <SubStrategyCard
                      key={subStrategy.id}
                      subStrategy={subStrategy}
                      botRunning={botRunning}
                      onUpdate={(id, updated) => {
                        const newSubStrategies = config.sub_strategies.map((s: any) =>
                          s.id === id ? updated : s
                        )
                        setConfig({ ...config, sub_strategies: newSubStrategies })
                      }}
                    />
                  ))}
                </VStack>
              </Box>

              {/* 协作规则列表 */}
              <Box>
                <HStack justify="space-between" align="center" mb={3}>
                  <Text fontWeight="bold">协作规则</Text>
                  <HStack spacing={2}>
                    <Button
                      size="sm"
                      variant="outline"
                      leftIcon={<InfoIcon />}
                      onClick={() => {
                        loadRuleTemplates()
                        onRuleTemplatesOpen()
                      }}
                      isDisabled={botRunning}
                    >
                      查看内置模板
                    </Button>
                  </HStack>
                </HStack>
                <VStack spacing={3}>
                  {config.collaboration_rules?.map((rule: any) => (
                    <CollaborationRuleCard
                      key={rule.id}
                      rule={rule}
                      botRunning={botRunning}
                      onEdit={(updated) => {
                        const newRules = config.collaboration_rules.map((r: any) =>
                          r.id === rule.id ? updated : r
                        )
                        setConfig({ ...config, collaboration_rules: newRules })
                      }}
                      onRemove={(id) => {
                        setConfig({
                          ...config,
                          collaboration_rules: config.collaboration_rules.filter((r: any) => r.id !== id)
                        })
                      }}
                    />
                  ))}
                </VStack>
              </Box>

              {/* 保存按钮 */}
              {!botRunning && (
                <Button
                  colorScheme="blue"
                  size="lg"
                  onClick={async () => {
                    try {
                      await updateHybridStrategy(botId, { hybrid_strategy: config })
                      toast({
                        title: '保存成功',
                        status: 'success',
                        duration: 2000,
                      })
                      onConfigUpdate?.()
                    } catch (error) {
                      toast({
                        title: '保存失败',
                        status: 'error',
                        duration: 3000,
                      })
                    }
                  }}
                >
                  保存配置
                </Button>
              )}

              {botRunning && (
                <Alert status="warning" fontSize="sm">
                  <AlertIcon />
                  请先停止 Bot 以修改混合策略配置
                </Alert>
              )}
            </>
          )}
        </VStack>
      </CardBody>

      {/* 内置规则模板对话框 */}
      <Modal isOpen={isRuleTemplatesOpen} onClose={onRuleTemplatesClose} size="xl">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>内置协作规则模板</ModalHeader>
          <ModalCloseButton />
          <ModalBody pb={6}>
            <VStack spacing={4}>
              <Text fontSize="sm" color="gray.600">
                点击模板可以快速添加到您的混合策略配置中
              </Text>
              <VStack spacing={3} align="stretch">
                {ruleTemplates.map((template) => (
                  <Card key={template.id} size="sm" variant="outline">
                    <CardBody>
                      <VStack spacing={2} align="stretch">
                        <Flex justify="space-between" align="center">
                          <HStack spacing={2}>
                            <Badge>{template.priority}</Badge>
                            <Text fontWeight="bold">{template.name}</Text>
                          </HStack>
                          <Switch
                            size="sm"
                            isChecked={template.enabled}
                            onChange={(e) => {
                              const updated = ruleTemplates.map((t) =>
                                t.id === template.id ? { ...t, enabled: e.target.checked } : t
                              )
                              setRuleTemplates(updated)
                            }}
                          />
                        </Flex>
                        <Text fontSize="sm" color="gray.600">
                          {template.description}
                        </Text>
                        <Collapse in={template.enabled}>
                          <Box pl={4} bg="gray.50" p={2} borderRadius="md">
                            <VStack align="stretch" spacing={1}>
                              <Text fontSize="xs" color="gray.500">条件:</Text>
                              <Text fontSize="xs">
                                {template.when?.source_strategy}.{template.when?.signal_type}
                                {template.when?.operator} {String(template.when?.value)}
                              </Text>
                              <Text fontSize="xs" color="gray.500" mt={2}>动作:</Text>
                              {(template.then || []).map((action: any, i: number) => (
                                <Text key={i} fontSize="xs">
                                  {i + 1}. {action.target_strategy}: {action.operation}
                                </Text>
                              ))}
                            </VStack>
                          </Box>
                        </Collapse>
                      </VStack>
                    </CardBody>
                  </Card>
                ))}
              </VStack>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" onClick={onRuleTemplatesClose}>
              关闭
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </Card>
  )
}

export default HybridStrategyConfig
