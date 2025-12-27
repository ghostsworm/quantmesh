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
  Accordion,
  AccordionItem,
  AccordionButton,
  AccordionPanel,
  AccordionIcon,
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
} from '@chakra-ui/react'
import { ViewIcon, ViewOffIcon } from '@chakra-ui/icons'
import {
  getConfig,
  updateConfig,
  previewConfig,
  getBackups,
  restoreBackup,
  deleteBackup,
  Config,
  ConfigChange,
  ConfigDiff,
  BackupInfo,
} from '../services/config'

const Configuration: React.FC = () => {
  const [config, setConfig] = useState<Config | null>(null)
  const [originalConfig, setOriginalConfig] = useState<Config | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [previewDiff, setPreviewDiff] = useState<ConfigDiff | null>(null)
  const [requiresRestart, setRequiresRestart] = useState(false)
  
  // 备份管理
  const [backups, setBackups] = useState<BackupInfo[]>([])
  const [restoringBackup, setRestoringBackup] = useState<string | null>(null)
  
  // 密码显示状态
  const [showPasswords, setShowPasswords] = useState<Record<string, boolean>>({})
  
  const { isOpen: isPreviewOpen, onOpen: onPreviewOpen, onClose: onPreviewClose } = useDisclosure()
  const { isOpen: isBackupsOpen, onOpen: onBackupsOpen, onClose: onBackupsClose } = useDisclosure()
  const toast = useToast()

  // 切换密码显示
  const togglePasswordVisibility = (key: string) => {
    setShowPasswords(prev => ({ ...prev, [key]: !prev[key] }))
  }

  // 加载配置
  const loadConfig = async () => {
    try {
      setLoading(true)
      setError(null)
      const cfg = await getConfig()
      setConfig(cfg)
      setOriginalConfig(JSON.parse(JSON.stringify(cfg))) // 深拷贝
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载配置失败')
      toast({
        title: '加载失败',
        description: err instanceof Error ? err.message : '加载配置失败',
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setLoading(false)
    }
  }

  // 加载备份列表
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

  // 预览变更
  const handlePreview = async () => {
    if (!config) return

    try {
      setError(null)
      const diff = await previewConfig(config)
      setPreviewDiff(diff)
      setRequiresRestart(diff.requires_restart)
      onPreviewOpen()
    } catch (err) {
      setError(err instanceof Error ? err.message : '预览变更失败')
      toast({
        title: '预览失败',
        description: err instanceof Error ? err.message : '预览变更失败',
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    }
  }

  // 保存配置
  const handleSave = async () => {
    if (!config) return

    setSaving(true)
    setError(null)
    setSuccess(null)

    try {
      // 先预览
      const diff = await previewConfig(config)
      setPreviewDiff(diff)
      setRequiresRestart(diff.requires_restart)

      // 确认保存
      const result = await updateConfig(config)
      setSuccess(result.message + (result.requires_restart ? ' (需要重启才能生效)' : ''))
      onPreviewClose()
      
      toast({
        title: '保存成功',
        description: result.message + (result.requires_restart ? ' (需要重启才能生效)' : ''),
        status: 'success',
        duration: 5000,
        isClosable: true,
      })
      
      // 重新加载配置和备份列表
      await loadConfig()
      await loadBackups()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存配置失败')
      toast({
        title: '保存失败',
        description: err instanceof Error ? err.message : '保存配置失败',
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setSaving(false)
    }
  }

  // 恢复备份
  const handleRestoreBackup = async (backupId: string) => {
    if (!window.confirm('确定要恢复此备份吗？当前配置将被覆盖。')) {
      return
    }

    try {
      setRestoringBackup(backupId)
      await restoreBackup(backupId)
      setSuccess('备份恢复成功')
      toast({
        title: '恢复成功',
        description: '备份恢复成功',
        status: 'success',
        duration: 5000,
        isClosable: true,
      })
      await loadConfig()
      await loadBackups()
    } catch (err) {
      setError(err instanceof Error ? err.message : '恢复备份失败')
      toast({
        title: '恢复失败',
        description: err instanceof Error ? err.message : '恢复备份失败',
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setRestoringBackup(null)
    }
  }

  // 删除备份
  const handleDeleteBackup = async (backupId: string) => {
    if (!window.confirm('确定要删除此备份吗？')) {
      return
    }

    try {
      await deleteBackup(backupId)
      setSuccess('备份删除成功')
      toast({
        title: '删除成功',
        description: '备份删除成功',
        status: 'success',
        duration: 5000,
        isClosable: true,
      })
      await loadBackups()
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除备份失败')
      toast({
        title: '删除失败',
        description: err instanceof Error ? err.message : '删除备份失败',
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    }
  }

  // 更新配置字段
  const updateConfigField = (path: string, value: any) => {
    if (!config) return

    const keys = path.split('.')
    const newConfig = { ...config }
    let current: any = newConfig

    for (let i = 0; i < keys.length - 1; i++) {
      if (!current[keys[i]]) {
        current[keys[i]] = {}
      }
      current = current[keys[i]]
    }

    current[keys[keys.length - 1]] = value
    setConfig(newConfig)
  }

  // 格式化文件大小
  const formatFileSize = (bytes: number): string => {
    if (bytes < 1024) return bytes + ' B'
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB'
    return (bytes / (1024 * 1024)).toFixed(2) + ' MB'
  }

  // 格式化时间
  const formatTime = (timestamp: string): string => {
    return new Date(timestamp).toLocaleString('zh-CN')
  }

  // 渲染密码输入框
  const renderPasswordInput = (path: string, placeholder?: string) => {
    const key = path.replace(/\./g, '_')
    const show = showPasswords[key] || false
    const value = getNestedValue(config, path) || ''
    
    return (
      <InputGroup>
        <Input
          type={show ? 'text' : 'password'}
          value={value}
          onChange={(e) => updateConfigField(path, e.target.value)}
          placeholder={placeholder}
        />
        <InputRightElement width="4.5rem">
          <IconButton
            h="1.75rem"
            size="sm"
            onClick={() => togglePasswordVisibility(key)}
            aria-label={show ? '隐藏' : '显示'}
            icon={show ? <ViewOffIcon /> : <ViewIcon />}
          />
        </InputRightElement>
      </InputGroup>
    )
  }

  // 获取嵌套值
  const getNestedValue = (obj: any, path: string): any => {
    const keys = path.split('.')
    let current = obj
    for (const key of keys) {
      if (current == null) return undefined
      current = current[key]
    }
    return current
  }

  // 渲染重要配置项（红字）
  const renderWarningLabel = (label: string, isWarning: boolean = true) => {
    if (isWarning) {
      return (
        <FormLabel>
          <Text as="span" color="red.500" fontWeight="bold">
            {label}
          </Text>
        </FormLabel>
      )
    }
    return <FormLabel>{label}</FormLabel>
  }

  // 交易所列表
  const exchanges = ['binance', 'bitget', 'bybit', 'gate', 'edgex', 'bit']
  const exchangeNames: Record<string, string> = {
    binance: '币安 (Binance)',
    bitget: 'Bitget',
    bybit: 'Bybit',
    gate: 'Gate.io',
    edgex: 'EdgeX',
    bit: 'Bit.com',
  }

  if (loading) {
    return (
      <Container maxW="container.xl" py={8}>
        <Center h="400px">
          <Spinner size="xl" />
        </Center>
      </Container>
    )
  }

  if (!config) {
    return (
      <Container maxW="container.xl" py={8}>
        <Alert status="error">
          <AlertIcon />
          <AlertTitle>加载配置失败</AlertTitle>
        </Alert>
      </Container>
    )
  }

  return (
    <Container maxW="container.xl" py={8}>
      <VStack spacing={6} align="stretch">
        <Heading size="lg">系统配置</Heading>

        {error && (
          <Alert status="error">
            <AlertIcon />
            <AlertTitle>错误</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        {success && (
          <Alert status="success">
            <AlertIcon />
            <AlertTitle>成功</AlertTitle>
            <AlertDescription>{success}</AlertDescription>
          </Alert>
        )}

        <HStack spacing={4}>
          <Button onClick={onBackupsOpen}>备份管理</Button>
          <Button onClick={handlePreview} colorScheme="blue">预览变更</Button>
          <Button
            onClick={handleSave}
            colorScheme="green"
            isLoading={saving}
            loadingText="保存中..."
          >
            保存配置
          </Button>
        </HStack>

        {/* 配置表单 */}
        <Accordion defaultIndex={[0]} allowMultiple>
          {/* 应用配置 */}
          <AccordionItem>
            <AccordionButton>
              <Box flex="1" textAlign="left">
                <Heading size="sm">应用配置</Heading>
              </Box>
              <AccordionIcon />
            </AccordionButton>
            <AccordionPanel pb={4}>
              <VStack spacing={4} align="stretch">
                <FormControl>
                  <FormLabel>当前交易所</FormLabel>
                  <Select
                    value={config.app?.current_exchange || ''}
                    onChange={(e) => updateConfigField('app.current_exchange', e.target.value)}
                  >
                    {exchanges.map((ex) => (
                      <option key={ex} value={ex}>
                        {exchangeNames[ex] || ex}
                      </option>
                    ))}
                  </Select>
                </FormControl>
              </VStack>
            </AccordionPanel>
          </AccordionItem>

          {/* 交易所配置 */}
          <AccordionItem>
            <AccordionButton>
              <Box flex="1" textAlign="left">
                <Heading size="sm">交易所配置</Heading>
              </Box>
              <AccordionIcon />
            </AccordionButton>
            <AccordionPanel pb={4}>
              <Accordion allowMultiple>
                {exchanges.map((exchange) => (
                  <AccordionItem key={exchange}>
                    <AccordionButton>
                      <Box flex="1" textAlign="left">
                        {exchangeNames[exchange] || exchange}
                      </Box>
                      <AccordionIcon />
                    </AccordionButton>
                    <AccordionPanel pb={4}>
                      <VStack spacing={4} align="stretch">
                        <FormControl>
                          <FormLabel>API Key</FormLabel>
                          {renderPasswordInput(`exchanges.${exchange}.api_key`, '请输入API Key')}
                        </FormControl>
                        <FormControl>
                          <FormLabel>Secret Key</FormLabel>
                          {renderPasswordInput(`exchanges.${exchange}.secret_key`, '请输入Secret Key')}
                        </FormControl>
                        {(exchange === 'bitget' || exchange === 'bybit') && (
                          <FormControl>
                            <FormLabel>Passphrase</FormLabel>
                            {renderPasswordInput(`exchanges.${exchange}.passphrase`, '请输入Passphrase')}
                          </FormControl>
                        )}
                        <FormControl>
                          <FormLabel>手续费率</FormLabel>
                          <NumberInput
                            value={getNestedValue(config, `exchanges.${exchange}.fee_rate`) || 0}
                            onChange={(_, value) => updateConfigField(`exchanges.${exchange}.fee_rate`, value)}
                            precision={6}
                            step={0.0001}
                          >
                            <NumberInputField />
                            <NumberInputStepper>
                              <NumberIncrementStepper />
                              <NumberDecrementStepper />
                            </NumberInputStepper>
                          </NumberInput>
                        </FormControl>
                        <FormControl>
                          <Stack direction="row" align="center">
                            <Switch
                              id={`testnet-${exchange}`}
                              isChecked={getNestedValue(config, `exchanges.${exchange}.testnet`) || false}
                              onChange={(e) => updateConfigField(`exchanges.${exchange}.testnet`, e.target.checked)}
                            />
                            {renderWarningLabel('是否使用测试网', true)}
                          </Stack>
                          <Alert status="warning" mt={2}>
                            <AlertIcon />
                            <AlertDescription>
                              测试网模式下不会进行真实交易，请确认当前设置
                            </AlertDescription>
                          </Alert>
                        </FormControl>
                      </VStack>
                    </AccordionPanel>
                  </AccordionItem>
                ))}
              </Accordion>
            </AccordionPanel>
          </AccordionItem>

          {/* 交易配置 */}
          <AccordionItem>
            <AccordionButton>
              <Box flex="1" textAlign="left">
                <Heading size="sm">交易配置</Heading>
              </Box>
              <AccordionIcon />
            </AccordionButton>
            <AccordionPanel pb={4}>
              <VStack spacing={4} align="stretch">
                <FormControl>
                  <FormLabel>交易对</FormLabel>
                  <Input
                    value={config.trading?.symbol || ''}
                    onChange={(e) => updateConfigField('trading.symbol', e.target.value)}
                    placeholder="例如: ETHUSDT"
                  />
                </FormControl>
                <FormControl>
                  <FormLabel>价格间隔</FormLabel>
                  <NumberInput
                    value={config.trading?.price_interval || 0}
                    onChange={(_, value) => updateConfigField('trading.price_interval', value)}
                    precision={6}
                    step={0.01}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>订单金额 (USDT)</FormLabel>
                  <NumberInput
                    value={config.trading?.order_quantity || 0}
                    onChange={(_, value) => updateConfigField('trading.order_quantity', value)}
                    precision={2}
                    step={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>最小订单价值 (USDT)</FormLabel>
                  <NumberInput
                    value={config.trading?.min_order_value || 0}
                    onChange={(_, value) => updateConfigField('trading.min_order_value', value)}
                    precision={2}
                    step={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>买单窗口大小</FormLabel>
                  <NumberInput
                    value={config.trading?.buy_window_size || 0}
                    onChange={(_, value) => updateConfigField('trading.buy_window_size', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>卖单窗口大小</FormLabel>
                  <NumberInput
                    value={config.trading?.sell_window_size || 0}
                    onChange={(_, value) => updateConfigField('trading.sell_window_size', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>对账间隔 (秒)</FormLabel>
                  <NumberInput
                    value={config.trading?.reconcile_interval || 0}
                    onChange={(_, value) => updateConfigField('trading.reconcile_interval', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>订单清理上限</FormLabel>
                  <NumberInput
                    value={config.trading?.order_cleanup_threshold || 0}
                    onChange={(_, value) => updateConfigField('trading.order_cleanup_threshold', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>清理批次大小</FormLabel>
                  <NumberInput
                    value={config.trading?.cleanup_batch_size || 0}
                    onChange={(_, value) => updateConfigField('trading.cleanup_batch_size', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>保证金锁定时长 (秒)</FormLabel>
                  <NumberInput
                    value={config.trading?.margin_lock_duration_seconds || 0}
                    onChange={(_, value) => updateConfigField('trading.margin_lock_duration_seconds', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>持仓安全性检查</FormLabel>
                  <NumberInput
                    value={config.trading?.position_safety_check || 0}
                    onChange={(_, value) => updateConfigField('trading.position_safety_check', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
              </VStack>
            </AccordionPanel>
          </AccordionItem>

          {/* 系统配置 */}
          <AccordionItem>
            <AccordionButton>
              <Box flex="1" textAlign="left">
                <Heading size="sm">系统配置</Heading>
              </Box>
              <AccordionIcon />
            </AccordionButton>
            <AccordionPanel pb={4}>
              <VStack spacing={4} align="stretch">
                <FormControl>
                  <FormLabel>日志级别</FormLabel>
                  <Select
                    value={config.system?.log_level || 'INFO'}
                    onChange={(e) => updateConfigField('system.log_level', e.target.value)}
                  >
                    <option value="DEBUG">DEBUG</option>
                    <option value="INFO">INFO</option>
                    <option value="WARN">WARN</option>
                    <option value="ERROR">ERROR</option>
                    <option value="FATAL">FATAL</option>
                  </Select>
                </FormControl>
                <FormControl>
                  <FormLabel>系统时区</FormLabel>
                  <Input
                    value={config.system?.timezone || ''}
                    onChange={(e) => updateConfigField('system.timezone', e.target.value)}
                    placeholder="例如: Asia/Shanghai"
                  />
                </FormControl>
                <FormControl>
                  <Stack direction="row" align="center">
                    <Switch
                      id="cancel_on_exit"
                      isChecked={config.system?.cancel_on_exit || false}
                      onChange={(e) => updateConfigField('system.cancel_on_exit', e.target.checked)}
                    />
                    <FormLabel htmlFor="cancel_on_exit" mb={0}>
                      退出时撤销所有订单
                    </FormLabel>
                  </Stack>
                </FormControl>
                <FormControl>
                  <Stack direction="row" align="center">
                    <Switch
                      id="close_positions_on_exit"
                      isChecked={config.system?.close_positions_on_exit || false}
                      onChange={(e) => updateConfigField('system.close_positions_on_exit', e.target.checked)}
                    />
                    {renderWarningLabel('退出时自动平仓', true)}
                  </Stack>
                  <Alert status="warning" mt={2}>
                    <AlertIcon />
                    <AlertDescription>
                      启用后，系统退出时会自动平掉所有持仓，请谨慎操作
                    </AlertDescription>
                  </Alert>
                </FormControl>
              </VStack>
            </AccordionPanel>
          </AccordionItem>

          {/* 风控配置 */}
          <AccordionItem>
            <AccordionButton>
              <Box flex="1" textAlign="left">
                <Heading size="sm">风控配置</Heading>
              </Box>
              <AccordionIcon />
            </AccordionButton>
            <AccordionPanel pb={4}>
              <VStack spacing={4} align="stretch">
                <FormControl>
                  <Stack direction="row" align="center">
                    <Switch
                      id="risk_control_enabled"
                      isChecked={config.risk_control?.enabled || false}
                      onChange={(e) => updateConfigField('risk_control.enabled', e.target.checked)}
                    />
                    {renderWarningLabel('启用风控', true)}
                  </Stack>
                  <Alert status="warning" mt={2}>
                    <AlertIcon />
                    <AlertDescription>
                      风控系统用于监控市场异常，关闭后可能增加交易风险
                    </AlertDescription>
                  </Alert>
                </FormControl>
                <FormControl>
                  <FormLabel>监控币种 (每行一个)</FormLabel>
                  <Input
                    as="textarea"
                    minH="100px"
                    value={config.risk_control?.monitor_symbols?.join('\n') || ''}
                    onChange={(e) => {
                      const symbols = e.target.value.split('\n').filter(s => s.trim())
                      updateConfigField('risk_control.monitor_symbols', symbols)
                    }}
                    placeholder="BTCUSDT&#10;ETHUSDT&#10;SOLUSDT"
                  />
                </FormControl>
                <FormControl>
                  <FormLabel>K线周期</FormLabel>
                  <Select
                    value={config.risk_control?.interval || '1m'}
                    onChange={(e) => updateConfigField('risk_control.interval', e.target.value)}
                  >
                    <option value="1m">1分钟</option>
                    <option value="3m">3分钟</option>
                    <option value="5m">5分钟</option>
                  </Select>
                </FormControl>
                <FormControl>
                  <FormLabel>成交量倍数</FormLabel>
                  <NumberInput
                    value={config.risk_control?.volume_multiplier || 0}
                    onChange={(_, value) => updateConfigField('risk_control.volume_multiplier', value)}
                    precision={1}
                    step={0.1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>移动平均窗口</FormLabel>
                  <NumberInput
                    value={config.risk_control?.average_window || 0}
                    onChange={(_, value) => updateConfigField('risk_control.average_window', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>恢复交易阈值</FormLabel>
                  <NumberInput
                    value={config.risk_control?.recovery_threshold || 0}
                    onChange={(_, value) => updateConfigField('risk_control.recovery_threshold', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>
                    <Text as="span" color="red.500" fontWeight="bold">
                      最大杠杆倍数
                    </Text>
                  </FormLabel>
                  <NumberInput
                    value={config.risk_control?.max_leverage || 0}
                    onChange={(_, value) => updateConfigField('risk_control.max_leverage', value)}
                    min={0}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                  <Alert status="warning" mt={2}>
                    <AlertIcon />
                    <AlertDescription>
                      设置最大允许的杠杆倍数，0表示不限制。高杠杆会增加风险，请谨慎设置
                    </AlertDescription>
                  </Alert>
                </FormControl>
              </VStack>
            </AccordionPanel>
          </AccordionItem>

          {/* AI配置 */}
          <AccordionItem>
            <AccordionButton>
              <Box flex="1" textAlign="left">
                <Heading size="sm">AI配置</Heading>
              </Box>
              <AccordionIcon />
            </AccordionButton>
            <AccordionPanel pb={4}>
              <VStack spacing={4} align="stretch">
                <FormControl>
                  <Stack direction="row" align="center">
                    <Switch
                      id="ai_enabled"
                      isChecked={config.ai?.enabled || false}
                      onChange={(e) => updateConfigField('ai.enabled', e.target.checked)}
                    />
                    <FormLabel htmlFor="ai_enabled" mb={0}>
                      启用AI功能
                    </FormLabel>
                  </Stack>
                </FormControl>
                <FormControl>
                  <FormLabel>AI服务提供商</FormLabel>
                  <Select
                    value={config.ai?.provider || ''}
                    onChange={(e) => updateConfigField('ai.provider', e.target.value)}
                  >
                    <option value="">请选择</option>
                    <option value="gemini">Gemini</option>
                    <option value="openai">OpenAI</option>
                  </Select>
                </FormControl>
                <FormControl>
                  <FormLabel>API Key</FormLabel>
                  {renderPasswordInput('ai.api_key', '请输入AI API Key')}
                </FormControl>
                <FormControl>
                  <FormLabel>Base URL (可选)</FormLabel>
                  <Input
                    value={config.ai?.base_url || ''}
                    onChange={(e) => updateConfigField('ai.base_url', e.target.value)}
                    placeholder="自定义API端点，留空使用默认"
                  />
                </FormControl>
                <FormControl>
                  <FormLabel>决策模式</FormLabel>
                  <Select
                    value={config.ai?.decision_mode || ''}
                    onChange={(e) => updateConfigField('ai.decision_mode', e.target.value)}
                  >
                    <option value="advisor">建议模式</option>
                    <option value="executor">执行模式</option>
                    <option value="hybrid">混合模式</option>
                  </Select>
                </FormControl>
                <Divider />
                <Heading size="xs">AI模块配置</Heading>
                {config.ai?.modules && (
                  <>
                    {config.ai.modules.market_analysis && (
                      <FormControl>
                        <Stack direction="row" align="center">
                          <Switch
                            id="market_analysis_enabled"
                            isChecked={config.ai.modules.market_analysis.enabled || false}
                            onChange={(e) => updateConfigField('ai.modules.market_analysis.enabled', e.target.checked)}
                          />
                          <FormLabel htmlFor="market_analysis_enabled" mb={0}>
                            市场分析
                          </FormLabel>
                        </Stack>
                      </FormControl>
                    )}
                    {config.ai.modules.parameter_optimization && (
                      <FormControl>
                        <Stack direction="row" align="center">
                          <Switch
                            id="parameter_optimization_enabled"
                            isChecked={config.ai.modules.parameter_optimization.enabled || false}
                            onChange={(e) => updateConfigField('ai.modules.parameter_optimization.enabled', e.target.checked)}
                          />
                          <FormLabel htmlFor="parameter_optimization_enabled" mb={0}>
                            参数优化
                          </FormLabel>
                        </Stack>
                      </FormControl>
                    )}
                    {config.ai.modules.risk_analysis && (
                      <FormControl>
                        <Stack direction="row" align="center">
                          <Switch
                            id="risk_analysis_enabled"
                            isChecked={config.ai.modules.risk_analysis.enabled || false}
                            onChange={(e) => updateConfigField('ai.modules.risk_analysis.enabled', e.target.checked)}
                          />
                          <FormLabel htmlFor="risk_analysis_enabled" mb={0}>
                            风险分析
                          </FormLabel>
                        </Stack>
                      </FormControl>
                    )}
                    {config.ai.modules.sentiment_analysis && (
                      <FormControl>
                        <Stack direction="row" align="center">
                          <Switch
                            id="sentiment_analysis_enabled"
                            isChecked={config.ai.modules.sentiment_analysis.enabled || false}
                            onChange={(e) => updateConfigField('ai.modules.sentiment_analysis.enabled', e.target.checked)}
                          />
                          <FormLabel htmlFor="sentiment_analysis_enabled" mb={0}>
                            情绪分析
                          </FormLabel>
                        </Stack>
                      </FormControl>
                    )}
                    {config.ai.modules.polymarket_signal && (
                      <FormControl>
                        <Stack direction="row" align="center">
                          <Switch
                            id="polymarket_signal_enabled"
                            isChecked={config.ai.modules.polymarket_signal.enabled || false}
                            onChange={(e) => updateConfigField('ai.modules.polymarket_signal.enabled', e.target.checked)}
                          />
                          <FormLabel htmlFor="polymarket_signal_enabled" mb={0}>
                            Polymarket信号
                          </FormLabel>
                        </Stack>
                      </FormControl>
                    )}
                  </>
                )}
              </VStack>
            </AccordionPanel>
          </AccordionItem>

          {/* 通知配置 */}
          <AccordionItem>
            <AccordionButton>
              <Box flex="1" textAlign="left">
                <Heading size="sm">通知配置</Heading>
              </Box>
              <AccordionIcon />
            </AccordionButton>
            <AccordionPanel pb={4}>
              <VStack spacing={4} align="stretch">
                <FormControl>
                  <Stack direction="row" align="center">
                    <Switch
                      id="notifications_enabled"
                      isChecked={config.notifications?.enabled || false}
                      onChange={(e) => updateConfigField('notifications.enabled', e.target.checked)}
                    />
                    <FormLabel htmlFor="notifications_enabled" mb={0}>
                      启用通知
                    </FormLabel>
                  </Stack>
                </FormControl>
                <Divider />
                <Heading size="xs">Telegram通知</Heading>
                <FormControl>
                  <Stack direction="row" align="center">
                    <Switch
                      id="telegram_enabled"
                      isChecked={config.notifications?.telegram?.enabled || false}
                      onChange={(e) => updateConfigField('notifications.telegram.enabled', e.target.checked)}
                    />
                    <FormLabel htmlFor="telegram_enabled" mb={0}>
                      启用Telegram通知
                    </FormLabel>
                  </Stack>
                </FormControl>
                <FormControl>
                  <FormLabel>Bot Token</FormLabel>
                  {renderPasswordInput('notifications.telegram.bot_token', '请输入Telegram Bot Token')}
                </FormControl>
                <FormControl>
                  <FormLabel>Chat ID</FormLabel>
                  <Input
                    value={config.notifications?.telegram?.chat_id || ''}
                    onChange={(e) => updateConfigField('notifications.telegram.chat_id', e.target.value)}
                    placeholder="请输入Chat ID"
                  />
                </FormControl>
                <Divider />
                <Heading size="xs">Webhook通知</Heading>
                <FormControl>
                  <Stack direction="row" align="center">
                    <Switch
                      id="webhook_enabled"
                      isChecked={config.notifications?.webhook?.enabled || false}
                      onChange={(e) => updateConfigField('notifications.webhook.enabled', e.target.checked)}
                    />
                    <FormLabel htmlFor="webhook_enabled" mb={0}>
                      启用Webhook通知
                    </FormLabel>
                  </Stack>
                </FormControl>
                <FormControl>
                  <FormLabel>Webhook URL</FormLabel>
                  <Input
                    value={config.notifications?.webhook?.url || ''}
                    onChange={(e) => updateConfigField('notifications.webhook.url', e.target.value)}
                    placeholder="https://your-webhook-url.com/api/notify"
                  />
                </FormControl>
                <FormControl>
                  <FormLabel>超时时间 (秒)</FormLabel>
                  <NumberInput
                    value={config.notifications?.webhook?.timeout || 3}
                    onChange={(_, value) => updateConfigField('notifications.webhook.timeout', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <Divider />
                <Heading size="xs">邮件通知</Heading>
                <FormControl>
                  <Stack direction="row" align="center">
                    <Switch
                      id="email_enabled"
                      isChecked={config.notifications?.email?.enabled || false}
                      onChange={(e) => updateConfigField('notifications.email.enabled', e.target.checked)}
                    />
                    <FormLabel htmlFor="email_enabled" mb={0}>
                      启用邮件通知
                    </FormLabel>
                  </Stack>
                </FormControl>
                <FormControl>
                  <FormLabel>邮件服务商</FormLabel>
                  <Select
                    value={config.notifications?.email?.provider || 'smtp'}
                    onChange={(e) => updateConfigField('notifications.email.provider', e.target.value)}
                  >
                    <option value="smtp">SMTP</option>
                    <option value="resend">Resend</option>
                    <option value="mailgun">Mailgun</option>
                  </Select>
                </FormControl>
                {config.notifications?.email?.provider === 'smtp' && (
                  <>
                    <FormControl>
                      <FormLabel>SMTP主机</FormLabel>
                      <Input
                        value={config.notifications?.email?.smtp?.host || ''}
                        onChange={(e) => updateConfigField('notifications.email.smtp.host', e.target.value)}
                        placeholder="smtp.example.com"
                      />
                    </FormControl>
                    <FormControl>
                      <FormLabel>SMTP端口</FormLabel>
                      <NumberInput
                        value={config.notifications?.email?.smtp?.port || 587}
                        onChange={(_, value) => updateConfigField('notifications.email.smtp.port', value)}
                        min={1}
                        max={65535}
                      >
                        <NumberInputField />
                        <NumberInputStepper>
                          <NumberIncrementStepper />
                          <NumberDecrementStepper />
                        </NumberInputStepper>
                      </NumberInput>
                    </FormControl>
                    <FormControl>
                      <FormLabel>SMTP用户名</FormLabel>
                      <Input
                        value={config.notifications?.email?.smtp?.username || ''}
                        onChange={(e) => updateConfigField('notifications.email.smtp.username', e.target.value)}
                        placeholder="your_username"
                      />
                    </FormControl>
                    <FormControl>
                      <FormLabel>SMTP密码</FormLabel>
                      {renderPasswordInput('notifications.email.smtp.password', '请输入SMTP密码')}
                    </FormControl>
                  </>
                )}
                {config.notifications?.email?.provider === 'resend' && (
                  <FormControl>
                    <FormLabel>Resend API Key</FormLabel>
                    {renderPasswordInput('notifications.email.resend.api_key', '请输入Resend API Key')}
                  </FormControl>
                )}
                {config.notifications?.email?.provider === 'mailgun' && (
                  <>
                    <FormControl>
                      <FormLabel>Mailgun API Key</FormLabel>
                      {renderPasswordInput('notifications.email.mailgun.api_key', '请输入Mailgun API Key')}
                    </FormControl>
                    <FormControl>
                      <FormLabel>Mailgun域名</FormLabel>
                      <Input
                        value={config.notifications?.email?.mailgun?.domain || ''}
                        onChange={(e) => updateConfigField('notifications.email.mailgun.domain', e.target.value)}
                        placeholder="your_domain.com"
                      />
                    </FormControl>
                  </>
                )}
                <FormControl>
                  <FormLabel>发件人邮箱</FormLabel>
                  <Input
                    value={config.notifications?.email?.from || ''}
                    onChange={(e) => updateConfigField('notifications.email.from', e.target.value)}
                    placeholder="alerts@yourdomain.com"
                  />
                </FormControl>
                <FormControl>
                  <FormLabel>收件人邮箱</FormLabel>
                  <Input
                    value={config.notifications?.email?.to || ''}
                    onChange={(e) => updateConfigField('notifications.email.to', e.target.value)}
                    placeholder="admin@yourdomain.com"
                  />
                </FormControl>
                <FormControl>
                  <FormLabel>邮件主题</FormLabel>
                  <Input
                    value={config.notifications?.email?.subject || ''}
                    onChange={(e) => updateConfigField('notifications.email.subject', e.target.value)}
                    placeholder="QuantMesh 交易通知"
                  />
                </FormControl>
              </VStack>
            </AccordionPanel>
          </AccordionItem>

          {/* 存储配置 */}
          <AccordionItem>
            <AccordionButton>
              <Box flex="1" textAlign="left">
                <Heading size="sm">存储配置</Heading>
              </Box>
              <AccordionIcon />
            </AccordionButton>
            <AccordionPanel pb={4}>
              <VStack spacing={4} align="stretch">
                <FormControl>
                  <Stack direction="row" align="center">
                    <Switch
                      id="storage_enabled"
                      isChecked={config.storage?.enabled || false}
                      onChange={(e) => updateConfigField('storage.enabled', e.target.checked)}
                    />
                    <FormLabel htmlFor="storage_enabled" mb={0}>
                      启用数据存储
                    </FormLabel>
                  </Stack>
                </FormControl>
                <FormControl>
                  <FormLabel>存储类型</FormLabel>
                  <Select
                    value={config.storage?.type || 'sqlite'}
                    onChange={(e) => updateConfigField('storage.type', e.target.value)}
                  >
                    <option value="sqlite">SQLite</option>
                  </Select>
                </FormControl>
                <FormControl>
                  <FormLabel>数据库路径</FormLabel>
                  <Input
                    value={config.storage?.path || ''}
                    onChange={(e) => updateConfigField('storage.path', e.target.value)}
                    placeholder="./data/quantmesh.db"
                  />
                </FormControl>
                <FormControl>
                  <FormLabel>缓冲区大小</FormLabel>
                  <NumberInput
                    value={config.storage?.buffer_size || 1000}
                    onChange={(_, value) => updateConfigField('storage.buffer_size', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>批量写入大小</FormLabel>
                  <NumberInput
                    value={config.storage?.batch_size || 100}
                    onChange={(_, value) => updateConfigField('storage.batch_size', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>刷新间隔 (秒)</FormLabel>
                  <NumberInput
                    value={config.storage?.flush_interval || 5}
                    onChange={(_, value) => updateConfigField('storage.flush_interval', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
              </VStack>
            </AccordionPanel>
          </AccordionItem>

          {/* Web服务配置 */}
          <AccordionItem>
            <AccordionButton>
              <Box flex="1" textAlign="left">
                <Heading size="sm">Web服务配置</Heading>
              </Box>
              <AccordionIcon />
            </AccordionButton>
            <AccordionPanel pb={4}>
              <VStack spacing={4} align="stretch">
                <FormControl>
                  <Stack direction="row" align="center">
                    <Switch
                      id="web_enabled"
                      isChecked={config.web?.enabled || false}
                      onChange={(e) => updateConfigField('web.enabled', e.target.checked)}
                    />
                    <FormLabel htmlFor="web_enabled" mb={0}>
                      启用Web服务
                    </FormLabel>
                  </Stack>
                </FormControl>
                <FormControl>
                  <FormLabel>监听地址</FormLabel>
                  <Input
                    value={config.web?.host || ''}
                    onChange={(e) => updateConfigField('web.host', e.target.value)}
                    placeholder="0.0.0.0"
                  />
                </FormControl>
                <FormControl>
                  <FormLabel>监听端口</FormLabel>
                  <NumberInput
                    value={config.web?.port || 28888}
                    onChange={(_, value) => updateConfigField('web.port', value)}
                    min={1}
                    max={65535}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>API密钥 (可选)</FormLabel>
                  {renderPasswordInput('web.api_key', '请输入API密钥')}
                </FormControl>
              </VStack>
            </AccordionPanel>
          </AccordionItem>

          {/* 时间间隔配置 */}
          <AccordionItem>
            <AccordionButton>
              <Box flex="1" textAlign="left">
                <Heading size="sm">时间间隔配置</Heading>
              </Box>
              <AccordionIcon />
            </AccordionButton>
            <AccordionPanel pb={4}>
              <VStack spacing={4} align="stretch">
                <Heading size="xs">WebSocket相关</Heading>
                <FormControl>
                  <FormLabel>WebSocket断线重连等待时间 (秒)</FormLabel>
                  <NumberInput
                    value={config.timing?.websocket_reconnect_delay || 5}
                    onChange={(_, value) => updateConfigField('timing.websocket_reconnect_delay', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>WebSocket写入等待时间 (秒)</FormLabel>
                  <NumberInput
                    value={config.timing?.websocket_write_wait || 10}
                    onChange={(_, value) => updateConfigField('timing.websocket_write_wait', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>WebSocket PONG等待时间 (秒)</FormLabel>
                  <NumberInput
                    value={config.timing?.websocket_pong_wait || 60}
                    onChange={(_, value) => updateConfigField('timing.websocket_pong_wait', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>WebSocket PING间隔 (秒)</FormLabel>
                  <NumberInput
                    value={config.timing?.websocket_ping_interval || 20}
                    onChange={(_, value) => updateConfigField('timing.websocket_ping_interval', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>listenKey保活间隔 (分钟)</FormLabel>
                  <NumberInput
                    value={config.timing?.listen_key_keepalive_interval || 30}
                    onChange={(_, value) => updateConfigField('timing.listen_key_keepalive_interval', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <Divider />
                <Heading size="xs">价格监控相关</Heading>
                <FormControl>
                  <FormLabel>定期发送价格的间隔 (毫秒)</FormLabel>
                  <NumberInput
                    value={config.timing?.price_send_interval || 50}
                    onChange={(_, value) => updateConfigField('timing.price_send_interval', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <Divider />
                <Heading size="xs">订单执行相关</Heading>
                <FormControl>
                  <FormLabel>速率限制重试等待时间 (秒)</FormLabel>
                  <NumberInput
                    value={config.timing?.rate_limit_retry_delay || 1}
                    onChange={(_, value) => updateConfigField('timing.rate_limit_retry_delay', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>其他错误重试等待时间 (毫秒)</FormLabel>
                  <NumberInput
                    value={config.timing?.order_retry_delay || 500}
                    onChange={(_, value) => updateConfigField('timing.order_retry_delay', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>等待获取价格的轮询间隔 (毫秒)</FormLabel>
                  <NumberInput
                    value={config.timing?.price_poll_interval || 500}
                    onChange={(_, value) => updateConfigField('timing.price_poll_interval', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>定期打印状态的间隔 (分钟)</FormLabel>
                  <NumberInput
                    value={config.timing?.status_print_interval || 1}
                    onChange={(_, value) => updateConfigField('timing.status_print_interval', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel>订单清理检查间隔 (秒)</FormLabel>
                  <NumberInput
                    value={config.timing?.order_cleanup_interval || 10}
                    onChange={(_, value) => updateConfigField('timing.order_cleanup_interval', value)}
                    min={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>
              </VStack>
            </AccordionPanel>
          </AccordionItem>
        </Accordion>

        {/* 预览对话框 */}
        <Modal isOpen={isPreviewOpen} onClose={onPreviewClose} size="xl">
          <ModalOverlay />
          <ModalContent>
            <ModalHeader>配置变更预览</ModalHeader>
            <ModalCloseButton />
            <ModalBody>
              {requiresRestart && (
                <Alert status="warning" mb={4}>
                  <AlertIcon />
                  <AlertTitle>需要重启</AlertTitle>
                  <AlertDescription>
                    部分配置需要重启才能生效
                  </AlertDescription>
                </Alert>
              )}
              <VStack spacing={4} align="stretch">
                <Text fontWeight="bold">
                  变更列表 ({previewDiff?.changes.length || 0} 项)
                </Text>
                {previewDiff?.changes.map((change, index) => (
                  <Box
                    key={index}
                    p={3}
                    borderWidth="1px"
                    borderRadius="md"
                    bg="gray.50"
                  >
                    <HStack spacing={2} mb={2}>
                      <Code>{change.path}</Code>
                      <Badge
                        colorScheme={
                          change.type === 'added'
                            ? 'green'
                            : change.type === 'deleted'
                            ? 'red'
                            : 'blue'
                        }
                      >
                        {change.type}
                      </Badge>
                      {change.requires_restart && (
                        <Badge colorScheme="orange">需要重启</Badge>
                      )}
                    </HStack>
                    {change.old_value !== undefined && (
                      <Text fontSize="sm" color="gray.600">
                        旧值: <Code>{JSON.stringify(change.old_value)}</Code>
                      </Text>
                    )}
                    {change.new_value !== undefined && (
                      <Text fontSize="sm" color="gray.600">
                        新值: <Code>{JSON.stringify(change.new_value)}</Code>
                      </Text>
                    )}
                  </Box>
                ))}
              </VStack>
            </ModalBody>
            <ModalFooter>
              <ButtonGroup>
                <Button onClick={onPreviewClose}>关闭</Button>
                <Button
                  colorScheme="green"
                  onClick={handleSave}
                  isLoading={saving}
                  loadingText="保存中..."
                >
                  确认保存
                </Button>
              </ButtonGroup>
            </ModalFooter>
          </ModalContent>
        </Modal>

        {/* 备份管理对话框 */}
        <Modal isOpen={isBackupsOpen} onClose={onBackupsClose} size="xl">
          <ModalOverlay />
          <ModalContent>
            <ModalHeader>备份管理</ModalHeader>
            <ModalCloseButton />
            <ModalBody>
              {backups.length === 0 ? (
                <Text>暂无备份</Text>
              ) : (
                <TableContainer>
                  <Table variant="simple">
                    <Thead>
                      <Tr>
                        <Th>备份时间</Th>
                        <Th>文件大小</Th>
                        <Th>操作</Th>
                      </Tr>
                    </Thead>
                    <Tbody>
                      {backups.map((backup) => (
                        <Tr key={backup.id}>
                          <Td>{formatTime(backup.timestamp)}</Td>
                          <Td>{formatFileSize(backup.size)}</Td>
                          <Td>
                            <ButtonGroup size="sm">
                              <Button
                                colorScheme="blue"
                                onClick={() => handleRestoreBackup(backup.id)}
                                isLoading={restoringBackup === backup.id}
                                loadingText="恢复中..."
                              >
                                恢复
                              </Button>
                              <Button
                                colorScheme="red"
                                onClick={() => handleDeleteBackup(backup.id)}
                              >
                                删除
                              </Button>
                            </ButtonGroup>
                          </Td>
                        </Tr>
                      ))}
                    </Tbody>
                  </Table>
                </TableContainer>
              )}
            </ModalBody>
            <ModalFooter>
              <Button onClick={onBackupsClose}>关闭</Button>
            </ModalFooter>
          </ModalContent>
        </Modal>
      </VStack>
    </Container>
  )
}

export default Configuration
