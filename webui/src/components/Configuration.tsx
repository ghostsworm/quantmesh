import React, { useState, useEffect } from 'react'
import {
  Box,
  Container,
  Heading,
  Button,
  ButtonGroup,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Spinner,
  Center,
  FormControl,
  FormLabel,
  Input,
  NumberInput,
  NumberInputField,
  NumberInputStepper,
  NumberIncrementStepper,
  NumberDecrementStepper,
  Select,
  Switch,
  Text,
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
  TableContainer,
  InputGroup,
  InputRightElement,
  IconButton,
  VStack,
  HStack,
  Divider,
  Badge,
  useToast,
  Code,
  Stack,
  Flex,
  Tabs,
  TabList,
  Tab,
  SimpleGrid,
} from '@chakra-ui/react'
import { ViewIcon, ViewOffIcon, SettingsIcon, BellIcon, InfoIcon, RepeatIcon, StarIcon, LockIcon } from '@chakra-ui/icons'
import { motion, AnimatePresence } from 'framer-motion'
import { useSymbol } from '../contexts/SymbolContext'
import {
  getConfig,
  updateConfig,
  previewConfig,
  getBackups,
  restoreBackup,
  deleteBackup,
  Config,
  BackupInfo,
  ConfigDiff,
} from '../services/config'

const MotionBox = motion(Box)

const ConfigCard: React.FC<{ title: string; children: React.ReactNode; icon?: any }> = ({ title, children, icon }) => {
  const bg = 'white'
  const borderColor = 'gray.100'
  
  return (
    <Box
      bg={bg}
      p={6}
      borderRadius="2xl"
      border="1px"
      borderColor={borderColor}
      boxShadow="sm"
      mb={6}
    >
      <HStack mb={5} spacing={3}>
        {icon && <Box color="blue.500">{icon}</Box>}
        <Heading size="sm" fontWeight="600">{title}</Heading>
      </HStack>
      <VStack spacing={5} align="stretch">
        {children}
      </VStack>
    </Box>
  )
}

const Configuration: React.FC = () => {
  const { isGlobalView, selectedSymbol } = useSymbol()
  const [config, setConfig] = useState<Config | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [previewDiff, setPreviewDiff] = useState<ConfigDiff | null>(null)
  const [requiresRestart, setRequiresRestart] = useState(false)
  
  // Tab control
  const [tabIndex, setTabIndex] = useState(0)
  
  // Backup management
  const [backups, setBackups] = useState<BackupInfo[]>([])
  const [restoringBackup, setRestoringBackup] = useState<string | null>(null)
  
  // Password visibility
  const [showPasswords, setShowPasswords] = useState<Record<string, boolean>>({})
  
  const { isOpen: isPreviewOpen, onOpen: onPreviewOpen, onClose: onPreviewClose } = useDisclosure()
  const { isOpen: isBackupsOpen, onOpen: onBackupsOpen, onClose: onBackupsClose } = useDisclosure()
  const toast = useToast()

  const togglePasswordVisibility = (key: string) => {
    setShowPasswords(prev => ({ ...prev, [key]: !prev[key] }))
  }

  const loadConfig = async () => {
    try {
      setLoading(true)
      const cfg = await getConfig()
      setConfig(cfg)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载配置失败')
    } finally {
      setLoading(false)
    }
  }

  const loadBackups = async () => {
    try {
      const backupList = await getBackups()
      setBackups(backupList)
    } catch (err) {
      console.error('加载备份列表失败:', err)
    }
  }

  useEffect(() => {
    loadConfig()
    loadBackups()
  }, [])

  // Reset tab index when switching view mode
  useEffect(() => {
    setTabIndex(0)
  }, [isGlobalView])

  const handlePreview = async () => {
    if (!config) return
    try {
      const diff = await previewConfig(config)
      setPreviewDiff(diff)
      setRequiresRestart(diff.requires_restart)
      onPreviewOpen()
    } catch (err) {
      toast({ title: '预览失败', status: 'error' })
    }
  }

  const handleSave = async () => {
    if (!config) return
    setSaving(true)
    try {
      const result = await updateConfig(config)
      setSuccess(result.message)
      onPreviewClose()
      toast({ title: '保存成功', status: 'success' })
      await loadConfig()
    } catch (err) {
      setError('保存配置失败')
    } finally {
      setSaving(false)
    }
  }

  const updateConfigField = (path: string, value: any) => {
    if (!config) return
    const keys = path.split('.')
    const newConfig = { ...config }
    let current: any = newConfig
    for (let i = 0; i < keys.length - 1; i++) {
      if (!current[keys[i]]) current[keys[i]] = {}
      current = current[keys[i]]
    }
    current[keys[keys.length - 1]] = value
    setConfig(newConfig)
  }

  const getNestedValue = (obj: any, path: string): any => {
    const keys = path.split('.')
    let current = obj
    for (const key of keys) {
      if (current == null) return undefined
      current = current[key]
    }
    return current
  }

  const renderPasswordInput = (path: string, placeholder?: string) => {
    const key = path.replace(/\./g, '_')
    const show = showPasswords[key] || false
    const value = getNestedValue(config, path) || ''
    return (
      <InputGroup size="md">
        <Input
          type={show ? 'text' : 'password'}
          value={value}
          onChange={(e) => updateConfigField(path, e.target.value)}
          placeholder={placeholder}
          borderRadius="xl"
        />
        <InputRightElement width="3rem">
          <IconButton
            variant="ghost"
            size="sm"
            onClick={() => togglePasswordVisibility(key)}
            aria-label={show ? '隐藏' : '显示'}
            icon={show ? <ViewOffIcon /> : <ViewIcon />}
          />
        </InputRightElement>
      </InputGroup>
    )
  }

  const exchanges = ['binance', 'bitget', 'bybit', 'gate', 'edgex', 'bit']
  const exchangeNames: Record<string, string> = {
    binance: '币安 (Binance)',
    bitget: 'Bitget',
    bybit: 'Bybit',
    gate: 'Gate.io',
    edgex: 'EdgeX',
    bit: 'Bit.com',
  }

  if (loading) return <Center h="400px"><Spinner size="xl" thickness="4px" color="blue.500" /></Center>
  if (!config) return <Container maxW="container.xl" py={8}><Alert status="error"><AlertIcon />加载配置失败</Alert></Container>

  const globalTabs = ["常规设置", "交易所 API", "通知设置", "存储与 Web"]
  const symbolTabs = ["交易参数", "风险控制", "AI 策略"]

  const activeTabs = isGlobalView ? globalTabs : symbolTabs

  return (
    <Container maxW="container.lg" py={10}>
      <VStack spacing={8} align="stretch">
        <Flex justify="space-between" align="flex-end">
          <Box>
            <Heading size="xl" fontWeight="800" mb={2}>设置</Heading>
            <Text color="gray.500">
              {isGlobalView ? "配置全局系统参数" : `管理 ${selectedSymbol} 的专用配置`}
            </Text>
          </Box>
          <HStack spacing={3}>
            <Button size="sm" variant="outline" onClick={onBackupsOpen} borderRadius="full">备份管理</Button>
            <Button size="sm" colorScheme="blue" onClick={handleSave} isLoading={saving} borderRadius="full" px={6}>保存更改</Button>
          </HStack>
        </Flex>

        <Tabs 
          index={tabIndex} 
          onChange={(index) => setTabIndex(index)} 
          variant="soft-rounded" 
          colorScheme="blue"
        >
          <TabList 
            bg="gray.100" 
            p={1} 
            borderRadius="full" 
            display="inline-flex"
          >
            {activeTabs.map((tab) => (
              <Tab 
                key={tab} 
                fontSize="sm" 
                fontWeight="600" 
                px={6} 
                borderRadius="full"
                _selected={{ bg: 'white', boxShadow: 'sm', color: 'blue.600' }}
              >
                {tab}
              </Tab>
            ))}
          </TabList>
        </Tabs>

        <AnimatePresence mode="wait">
          <MotionBox
            key={isGlobalView ? `global-${tabIndex}` : `symbol-${tabIndex}`}
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -10 }}
            transition={{ duration: 0.2 }}
          >
            {isGlobalView ? (
              <>
                {tabIndex === 0 && (
                  <VStack spacing={6} align="stretch">
                    <ConfigCard title="常规应用配置" icon={<SettingsIcon />}>
                      <FormControl>
                        <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">默认交易所</FormLabel>
                        <Select
                          value={config.app?.current_exchange || ''}
                          onChange={(e) => updateConfigField('app.current_exchange', e.target.value)}
                          borderRadius="xl"
                        >
                          {exchanges.map((ex) => (
                            <option key={ex} value={ex}>{exchangeNames[ex] || ex}</option>
                          ))}
                        </Select>
                      </FormControl>
                    </ConfigCard>
                    <ConfigCard title="系统基础配置" icon={<SettingsIcon />}>
                      <SimpleGrid columns={2} spacing={6}>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">日志级别</FormLabel>
                          <Select
                            value={config.system?.log_level || 'INFO'}
                            onChange={(e) => updateConfigField('system.log_level', e.target.value)}
                            borderRadius="xl"
                          >
                            <option value="DEBUG">DEBUG</option>
                            <option value="INFO">INFO</option>
                            <option value="WARN">WARN</option>
                            <option value="ERROR">ERROR</option>
                          </Select>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">时区</FormLabel>
                          <Input
                            value={config.system?.timezone || ''}
                            onChange={(e) => updateConfigField('system.timezone', e.target.value)}
                            placeholder="Asia/Shanghai"
                            borderRadius="xl"
                          />
                        </FormControl>
                      </SimpleGrid>
                      <Divider my={2} />
                      <Stack spacing={4}>
                        <Flex justify="space-between" align="center">
                          <Box>
                            <Text fontWeight="600" size="sm">退出时撤销所有订单</Text>
                            <Text fontSize="xs" color="gray.500">停止程序时自动清理未完成的委托</Text>
                          </Box>
                          <Switch
                            isChecked={config.system?.cancel_on_exit || false}
                            onChange={(e) => updateConfigField('system.cancel_on_exit', e.target.checked)}
                          />
                        </Flex>
                        <Flex justify="space-between" align="center">
                          <Box>
                            <Text fontWeight="600" size="sm" color="red.500">退出时自动平仓</Text>
                            <Text fontSize="xs" color="gray.500">⚠️ 高风险：停止程序时将以市价卖出所有持仓</Text>
                          </Box>
                          <Switch
                            colorScheme="red"
                            isChecked={config.system?.close_positions_on_exit || false}
                            onChange={(e) => updateConfigField('system.close_positions_on_exit', e.target.checked)}
                          />
                        </Flex>
                      </Stack>
                    </ConfigCard>
                  </VStack>
                )}

                {tabIndex === 1 && (
                  <VStack spacing={6} align="stretch">
                    {exchanges.map((exchange) => (
                      <ConfigCard key={exchange} title={exchangeNames[exchange]} icon={<RepeatIcon />}>
                        <SimpleGrid columns={2} spacing={6}>
                          <FormControl>
                            <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">API Key</FormLabel>
                            {renderPasswordInput(`exchanges.${exchange}.api_key`)}
                          </FormControl>
                          <FormControl>
                            <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">Secret Key</FormLabel>
                            {renderPasswordInput(`exchanges.${exchange}.secret_key`)}
                          </FormControl>
                        </SimpleGrid>
                        <Flex justify="space-between" align="center" mt={2}>
                          <HStack>
                            <Switch
                              size="sm"
                              isChecked={getNestedValue(config, `exchanges.${exchange}.testnet`) || false}
                              onChange={(e) => updateConfigField(`exchanges.${exchange}.testnet`, e.target.checked)}
                            />
                            <Text fontSize="sm" fontWeight="600">使用测试网 (Testnet)</Text>
                          </HStack>
                          <HStack>
                            <Text fontSize="xs" color="gray.500">手续费率:</Text>
                            <NumberInput
                              size="sm"
                              w="100px"
                              value={getNestedValue(config, `exchanges.${exchange}.fee_rate`) || 0}
                              onChange={(_, value) => updateConfigField(`exchanges.${exchange}.fee_rate`, value)}
                              precision={6}
                              step={0.0001}
                            >
                              <NumberInputField borderRadius="md" />
                            </NumberInput>
                          </HStack>
                        </Flex>
                      </ConfigCard>
                    ))}
                  </VStack>
                )}

                {tabIndex === 2 && (
                  <VStack spacing={6} align="stretch">
                    <ConfigCard title="全局通知开关" icon={<BellIcon />}>
                      <Flex justify="space-between" align="center">
                        <Text fontWeight="600">启用通知系统</Text>
                        <Switch
                          isChecked={config.notifications?.enabled || false}
                          onChange={(e) => updateConfigField('notifications.enabled', e.target.checked)}
                        />
                      </Flex>
                    </ConfigCard>
                    <SimpleGrid columns={2} spacing={6}>
                      <ConfigCard title="Telegram Bot">
                        <FormControl mb={4}>
                          <FormLabel fontSize="xs" fontWeight="bold">Token</FormLabel>
                          {renderPasswordInput('notifications.telegram.bot_token')}
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">Chat ID</FormLabel>
                          <Input
                            value={config.notifications?.telegram?.chat_id || ''}
                            onChange={(e) => updateConfigField('notifications.telegram.chat_id', e.target.value)}
                            borderRadius="xl"
                          />
                        </FormControl>
                      </ConfigCard>
                      <ConfigCard title="Webhook">
                        <FormControl mb={4}>
                          <FormLabel fontSize="xs" fontWeight="bold">URL</FormLabel>
                          <Input
                            value={config.notifications?.webhook?.url || ''}
                            onChange={(e) => updateConfigField('notifications.webhook.url', e.target.value)}
                            placeholder="https://..."
                            borderRadius="xl"
                          />
                        </FormControl>
                      </ConfigCard>
                    </SimpleGrid>
                  </VStack>
                )}

                {tabIndex === 3 && (
                  <SimpleGrid columns={2} spacing={6}>
                    <ConfigCard title="数据存储" icon={<SettingsIcon />}>
                      <FormControl mb={4}>
                        <FormLabel fontSize="xs" fontWeight="bold">数据库路径</FormLabel>
                        <Input
                          value={config.storage?.path || ''}
                          onChange={(e) => updateConfigField('storage.path', e.target.value)}
                          borderRadius="xl"
                        />
                      </FormControl>
                      <HStack spacing={4}>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">缓冲区</FormLabel>
                          <NumberInput value={config.storage?.buffer_size || 1000} onChange={(_, v) => updateConfigField('storage.buffer_size', v)}>
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">刷新 (秒)</FormLabel>
                          <NumberInput value={config.storage?.flush_interval || 5} onChange={(_, v) => updateConfigField('storage.flush_interval', v)}>
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                        </FormControl>
                      </HStack>
                    </ConfigCard>
                    <ConfigCard title="Web 服务" icon={<SettingsIcon />}>
                      <FormControl mb={4}>
                        <FormLabel fontSize="xs" fontWeight="bold">监听端口</FormLabel>
                        <NumberInput value={config.web?.port || 28888} onChange={(_, v) => updateConfigField('web.port', v)}>
                          <NumberInputField borderRadius="xl" />
                        </NumberInput>
                      </FormControl>
                      <FormControl>
                        <FormLabel fontSize="xs" fontWeight="bold">API 密钥 (可选)</FormLabel>
                        {renderPasswordInput('web.api_key')}
                      </FormControl>
                    </ConfigCard>
                  </SimpleGrid>
                )}
              </>
            ) : (
              <>
                {tabIndex === 0 && (
                  <VStack spacing={6} align="stretch">
                    <ConfigCard title={`交易对参数: ${selectedSymbol}`} icon={<RepeatIcon />}>
                      <SimpleGrid columns={2} spacing={6}>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">价格间隔 (Interval)</FormLabel>
                          <NumberInput value={config.trading?.price_interval || 0} onChange={(_, v) => updateConfigField('trading.price_interval', v)} precision={6} step={0.01}>
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">单笔订单金额 (USDT)</FormLabel>
                          <NumberInput value={config.trading?.order_quantity || 0} onChange={(_, v) => updateConfigField('trading.order_quantity', v)} precision={2}>
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">买单窗口大小</FormLabel>
                          <NumberInput value={config.trading?.buy_window_size || 0} onChange={(_, v) => updateConfigField('trading.buy_window_size', v)}>
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">卖单窗口大小</FormLabel>
                          <NumberInput value={config.trading?.sell_window_size || 0} onChange={(_, v) => updateConfigField('trading.sell_window_size', v)}>
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                        </FormControl>
                      </SimpleGrid>
                    </ConfigCard>
                  </VStack>
                )}

                {tabIndex === 1 && (
                  <ConfigCard title="风险控制设置" icon={<LockIcon />}>
                    <Flex justify="space-between" align="center" mb={6}>
                      <Box>
                        <Text fontWeight="600">启用风控引擎</Text>
                        <Text fontSize="xs" color="gray.500">自动监控异常市场波动并采取保护措施</Text>
                      </Box>
                      <Switch
                        colorScheme="orange"
                        isChecked={config.risk_control?.enabled || false}
                        onChange={(e) => updateConfigField('risk_control.enabled', e.target.checked)}
                      />
                    </Flex>
                    <SimpleGrid columns={2} spacing={6}>
                      <FormControl>
                        <FormLabel fontSize="xs" fontWeight="bold">最大允许杠杆</FormLabel>
                        <NumberInput value={config.risk_control?.max_leverage || 0} onChange={(_, v) => updateConfigField('risk_control.max_leverage', v)}>
                          <NumberInputField borderRadius="xl" />
                        </NumberInput>
                      </FormControl>
                      <FormControl>
                        <FormLabel fontSize="xs" fontWeight="bold">成交量异常倍数</FormLabel>
                        <NumberInput value={config.risk_control?.volume_multiplier || 0} onChange={(_, v) => updateConfigField('risk_control.volume_multiplier', v)} precision={1}>
                          <NumberInputField borderRadius="xl" />
                        </NumberInput>
                      </FormControl>
                    </SimpleGrid>
                  </ConfigCard>
                )}

                {tabIndex === 2 && (
                  <VStack spacing={6} align="stretch">
                    <ConfigCard title="AI 决策引擎" icon={<StarIcon />}>
                      <Flex justify="space-between" align="center" mb={6}>
                        <Box>
                          <Text fontWeight="600">启用 AI 辅助决策</Text>
                          <Text fontSize="xs" color="gray.500">使用大模型分析行情并优化交易参数</Text>
                        </Box>
                        <Switch
                          colorScheme="purple"
                          isChecked={config.ai?.enabled || false}
                          onChange={(e) => updateConfigField('ai.enabled', e.target.checked)}
                        />
                      </Flex>
                      <SimpleGrid columns={2} spacing={6}>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">服务商</FormLabel>
                          <Select value={config.ai?.provider || ''} onChange={(e) => updateConfigField('ai.provider', e.target.value)} borderRadius="xl">
                            <option value="gemini">Gemini</option>
                            <option value="openai">OpenAI</option>
                          </Select>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">决策模式</FormLabel>
                          <Select value={config.ai?.decision_mode || ''} onChange={(e) => updateConfigField('ai.decision_mode', e.target.value)} borderRadius="xl">
                            <option value="advisor">建议模式 (只读)</option>
                            <option value="executor">执行模式 (自动下单)</option>
                          </Select>
                        </FormControl>
                      </SimpleGrid>
                      <FormControl mt={4}>
                        <FormLabel fontSize="xs" fontWeight="bold">API Key</FormLabel>
                        {renderPasswordInput('ai.api_key')}
                      </FormControl>
                    </ConfigCard>
                  </VStack>
                )}
              </>
            )}
          </MotionBox>
        </AnimatePresence>

        {/* Restore Modals & Overlays from previous version */}
        <Modal isOpen={isPreviewOpen} onClose={onPreviewClose} size="xl">
          <ModalOverlay backdropFilter="blur(4px)" />
          <ModalContent borderRadius="2xl">
            <ModalHeader>确认变更</ModalHeader>
            <ModalCloseButton />
            <ModalBody>
              <VStack spacing={4} align="stretch">
                {previewDiff?.changes.map((change, i) => (
                  <Box key={i} p={3} borderRadius="lg" bg="gray.50">
                    <Text fontSize="xs" fontWeight="bold" mb={1}>{change.path}</Text>
                    <HStack fontSize="sm">
                      <Badge colorScheme="red">{JSON.stringify(change.old_value)}</Badge>
                      <Text>→</Text>
                      <Badge colorScheme="green">{JSON.stringify(change.new_value)}</Badge>
                    </HStack>
                  </Box>
                ))}
              </VStack>
            </ModalBody>
            <ModalFooter>
              <Button variant="ghost" mr={3} onClick={onPreviewClose}>取消</Button>
              <Button colorScheme="blue" onClick={handleSave} isLoading={saving}>确认保存</Button>
            </ModalFooter>
          </ModalContent>
        </Modal>

        {/* Backups Modal */}
        <Modal isOpen={isBackupsOpen} onClose={onBackupsClose} size="lg">
          <ModalOverlay backdropFilter="blur(4px)" />
          <ModalContent borderRadius="2xl">
            <ModalHeader>备份管理</ModalHeader>
            <ModalCloseButton />
            <ModalBody>
              <TableContainer>
                <Table variant="simple" size="sm">
                  <Thead><Tr><Th>时间</Th><Th>大小</Th><Th>操作</Th></Tr></Thead>
                  <Tbody>
                    {backups.map((b) => (
                      <Tr key={b.id}>
                        <Td>{new Date(b.timestamp).toLocaleString()}</Td>
                        <Td>{(b.size / 1024).toFixed(1)}KB</Td>
                        <Td>
                          <Button size="xs" variant="link" colorScheme="blue" onClick={() => {}}>恢复</Button>
                        </Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </TableContainer>
            </ModalBody>
          </ModalContent>
        </Modal>
      </VStack>
    </Container>
  )
}

export default Configuration
