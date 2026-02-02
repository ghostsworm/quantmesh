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
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { useSymbol } from '../contexts/SymbolContext'
import {
  getConfig,
  updateConfig,
  previewConfig,
  getBackups,
  restoreBackup,
  deleteBackup,
  getConfigYAML,
  validateConfigYAML,
  updateConfigYAML,
  Config,
  BackupInfo,
  ConfigDiff,
} from '../services/config'
import AIConfigWizard from './AIConfigWizard'
import SymbolManager from './SymbolManager'
import YamlEditor from './YamlEditor'
import DiffPreviewModal from './DiffPreviewModal'
import ConfigHistory from './ConfigHistory'

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
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { isGlobalView, selectedExchange, selectedSymbol } = useSymbol()
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
  
  // YAML Editor state
  const [yamlContent, setYamlContent] = useState<string>('')
  const [originalYamlContent, setOriginalYamlContent] = useState<string>('')
  const [yamlLoading, setYamlLoading] = useState(false)
  const [yamlValid, setYamlValid] = useState(true)
  const [yamlError, setYamlError] = useState<string | null>(null)
  const [yamlSaving, setYamlSaving] = useState(false)
  
  const { isOpen: isPreviewOpen, onOpen: onPreviewOpen, onClose: onPreviewClose } = useDisclosure()
  const { isOpen: isBackupsOpen, onOpen: onBackupsOpen, onClose: onBackupsClose } = useDisclosure()
  const { isOpen: isAIWizardOpen, onOpen: onAIWizardOpen, onClose: onAIWizardClose } = useDisclosure()
  const { isOpen: isDiffOpen, onOpen: onDiffOpen, onClose: onDiffClose } = useDisclosure()
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
      setError(err instanceof Error ? err.message : t('configuration.loadFailed'))
    } finally {
      setLoading(false)
    }
  }

  const loadBackups = async () => {
    try {
      const backupList = await getBackups()
      setBackups(backupList)
    } catch (err) {
      console.error(t('configuration.loadBackupListFailed'), err)
    }
  }

  const loadYamlContent = async () => {
    try {
      setYamlLoading(true)
      const content = await getConfigYAML()
      setYamlContent(content)
      setOriginalYamlContent(content)
      setYamlValid(true)
      setYamlError(null)
    } catch (err) {
      console.error('加載 YAML 配置失败:', err)
      toast({
        title: '加載失败',
        description: err instanceof Error ? err.message : '加載 YAML 配置失败',
        status: 'error',
        duration: 3000,
      })
    } finally {
      setYamlLoading(false)
    }
  }

  const handleYamlValidate = async () => {
    try {
      const result = await validateConfigYAML(yamlContent)
      setYamlValid(result.valid)
      setYamlError(result.error || null)
      if (result.valid) {
        toast({
          title: '驗证通過',
          description: '配置语法正确',
          status: 'success',
          duration: 2000,
        })
      } else {
        toast({
          title: '驗证失败',
          description: result.error,
          status: 'error',
          duration: 5000,
        })
      }
    } catch (err) {
      toast({
        title: '驗证失败',
        description: err instanceof Error ? err.message : '驗证配置失败',
        status: 'error',
        duration: 3000,
      })
    }
  }

  const handleYamlPreview = async () => {
    // 先驗证
    const result = await validateConfigYAML(yamlContent)
    if (!result.valid) {
      toast({
        title: '配置無效',
        description: result.error || '请先修複配置錯误',
        status: 'error',
        duration: 3000,
      })
      return
    }
    onDiffOpen()
  }

  const handleYamlSave = async () => {
    try {
      setYamlSaving(true)
      const result = await updateConfigYAML(yamlContent)
      
      toast({
        title: '保存成功',
        description: result.requires_restart ? '部分配置需要重啟后生效' : '配置已更新並生效',
        status: 'success',
        duration: 3000,
      })
      
      onDiffClose()
      setOriginalYamlContent(yamlContent)
      
      // 刷新 JSON 配置
      await loadConfig()
    } catch (err) {
      toast({
        title: '保存失败',
        description: err instanceof Error ? err.message : '保存配置失败',
        status: 'error',
        duration: 5000,
      })
    } finally {
      setYamlSaving(false)
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
      toast({ title: t('configuration.previewFailed'), status: 'error' })
    }
  }

  const handleSave = async () => {
    if (!config) return
    setSaving(true)
    setError(null)
    setSuccess(null)
    try {
      const result = await updateConfig(config)
      setSuccess(result.message)
      onPreviewClose()
      toast({ 
        title: t('configuration.saveSuccess'), 
        status: 'success',
        duration: 3000,
        isClosable: true
      })
      await loadConfig()
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : t('configuration.saveFailed')
      setError(errorMessage)
      toast({ 
        title: t('configuration.saveFailed'), 
        description: errorMessage,
        status: 'error',
        duration: 5000,
        isClosable: true
      })
      console.error('保存配置失败:', err)
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

  // ===== 交易對視图：读写 trading.symbols[*] =====
  // 后端會在 Validate() 中用 symbols[0] 回写 trading.* 舊字段（兼容逻辑），
  // 所以在“交易對視图”里必須直接修改 symbols 配置，否则保存后會被覆盖回舊值。
  const getSelectedSymbolConfigIndex = (): number => {
    if (!config) return -1
    const symbols = config.trading?.symbols || []
    const currentExchange = (selectedExchange || config.app?.current_exchange || '').toLowerCase()
    const currentSymbol = (selectedSymbol || '').toLowerCase()
    if (!currentSymbol) return -1

    const getEffectiveExchange = (sc: any) => (sc?.exchange || config.app?.current_exchange || '').toLowerCase()
    return symbols.findIndex((sc: any) => getEffectiveExchange(sc) === currentExchange && (sc?.symbol || '').toLowerCase() === currentSymbol)
  }

  const getSelectedSymbolConfig = (): any | null => {
    if (!config) return null
    const idx = getSelectedSymbolConfigIndex()
    if (idx < 0) return null
    return config.trading?.symbols?.[idx] || null
  }

  const updateSelectedSymbolField = (field: string, value: any) => {
    if (!config) return
    const newConfig: any = { ...config }
    if (!newConfig.trading) newConfig.trading = {}
    const symbols: any[] = Array.isArray(newConfig.trading.symbols) ? [...newConfig.trading.symbols] : []

    const idx = getSelectedSymbolConfigIndex()
    const fallbackSellWindow = (newConfig.trading.sell_window_size && newConfig.trading.sell_window_size > 0)
      ? newConfig.trading.sell_window_size
      : (newConfig.trading.buy_window_size || 0)

    if (idx >= 0) {
      symbols[idx] = { ...symbols[idx], [field]: value }
    } else {
      // 若没找到對应交易對配置，则創建一份（Validate 會补齐缺失字段/默认值）
      symbols.push({
        exchange: selectedExchange || newConfig.app?.current_exchange || '',
        symbol: selectedSymbol,
        price_interval: newConfig.trading.price_interval || 0,
        order_quantity: newConfig.trading.order_quantity || 0,
        buy_window_size: newConfig.trading.buy_window_size || 0,
        sell_window_size: fallbackSellWindow || 0,
        [field]: value,
      })
    }

    newConfig.trading.symbols = symbols
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
            aria-label={show ? t('configuration.hide') : t('configuration.show')}
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
  if (!config) return <Container maxW="container.xl" py={8}><Alert status="error"><AlertIcon />{t('configuration.loadFailed')}</Alert></Container>

  const globalTabs = [t('configuration.globalTabs.general'), t('configuration.globalTabs.exchangeAPI'), t('configuration.globalTabs.notifications'), t('configuration.globalTabs.storageWeb'), 'YAML 编辑器', '历史版本']
  const symbolTabs = [t('configuration.symbolTabs.tradingParams'), t('configuration.symbolTabs.riskControl'), t('configuration.symbolTabs.aiStrategy')]

  const activeTabs = isGlobalView ? globalTabs : symbolTabs

  return (
    <Container maxW="container.lg" py={10}>
      <VStack spacing={8} align="stretch">
        <Flex justify="space-between" align="flex-end">
          <Box>
            <Heading size="xl" fontWeight="800" mb={2}>{t('configuration.title')}</Heading>
            <Text color="gray.500">
              {isGlobalView ? t('configuration.globalDescription') : t('configuration.symbolDescription', { symbol: selectedSymbol })}
            </Text>
          </Box>
          <HStack spacing={3}>
            <Button size="sm" variant="outline" onClick={onBackupsOpen} borderRadius="full">{t('configuration.backupManagement')}</Button>
            <Button size="sm" colorScheme="blue" onClick={handleSave} isLoading={saving} borderRadius="full" px={6}>{t('configuration.saveChanges')}</Button>
          </HStack>
        </Flex>

        {error && (
          <Alert status="error" borderRadius="lg">
            <AlertIcon />
            <AlertTitle>{t('configuration.saveFailed')}</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        {success && (
          <Alert status="success" borderRadius="lg">
            <AlertIcon />
            <AlertTitle>{t('configuration.saveSuccess')}</AlertTitle>
            <AlertDescription>{success}</AlertDescription>
          </Alert>
        )}

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
                    <ConfigCard title={t('configuration.generalAppConfig')} icon={<SettingsIcon />}>
                      <FormControl>
                        <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.defaultExchange')}</FormLabel>
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
                    <ConfigCard title={t('configuration.systemBasicConfig')} icon={<SettingsIcon />}>
                      <SimpleGrid columns={2} spacing={6}>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.logLevel')}</FormLabel>
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
                          <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.timezone')}</FormLabel>
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
                            <Text fontWeight="600" size="sm">{t('configuration.cancelOnExit')}</Text>
                            <Text fontSize="xs" color="gray.500">{t('configuration.cancelOnExitDesc')}</Text>
                          </Box>
                          <Switch
                            isChecked={config.system?.cancel_on_exit || false}
                            onChange={(e) => updateConfigField('system.cancel_on_exit', e.target.checked)}
                          />
                        </Flex>
                        <Flex justify="space-between" align="center">
                          <Box>
                            <Text fontWeight="600" size="sm" color="red.500">{t('configuration.closePositionsOnExit')}</Text>
                            <Text fontSize="xs" color="gray.500">{t('configuration.closePositionsOnExitDesc')}</Text>
                          </Box>
                          <Switch
                            colorScheme="red"
                            isChecked={config.system?.close_positions_on_exit || false}
                            onChange={(e) => updateConfigField('system.close_positions_on_exit', e.target.checked)}
                          />
                        </Flex>
                      </Stack>
                    </ConfigCard>
                    <ConfigCard title="交易對管理" icon={<RepeatIcon />}>
                      <SymbolManager
                        config={config}
                        onUpdate={async (symbols) => {
                          const newConfig = { ...config }
                          if (!newConfig.trading) {
                            newConfig.trading = {} as any
                          }
                          newConfig.trading.symbols = symbols
                          setConfig(newConfig)
                          
                          // 自动保存交易對变更到后端
                          try {
                            await updateConfig(newConfig)
                            toast({
                              title: t('configuration.saveSuccess'),
                              description: '交易對配置已自动保存',
                              status: 'success',
                              duration: 2000,
                              isClosable: true,
                            })
                          } catch (err) {
                            const errorMessage = err instanceof Error ? err.message : t('configuration.saveFailed')
                            toast({
                              title: t('configuration.saveFailed'),
                              description: errorMessage,
                              status: 'error',
                              duration: 5000,
                              isClosable: true,
                            })
                            console.error('自动保存交易對配置失败:', err)
                          }
                        }}
                      />
                    </ConfigCard>
                  </VStack>
                )}

                {tabIndex === 1 && (
                  <VStack spacing={6} align="stretch">
                    <ConfigCard title="AI 配置助手" icon={<StarIcon />}>
                      <VStack spacing={4} align="stretch">
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">Gemini API Key</FormLabel>
                          {renderPasswordInput('ai.gemini_api_key', '输入您的 Gemini API Key')}
                          <Text fontSize="xs" color="gray.500" mt={1}>
                            用於 AI 配置助手功能，帮助您自动生成最优的网格交易参數和资金分配方案。系统内置异步任務处理，無需配置額外代理。
                          </Text>
                        </FormControl>
                        
                        <Button
                          leftIcon={<StarIcon />}
                          colorScheme="purple"
                          variant="outline"
                          onClick={onAIWizardOpen}
                          isDisabled={!getNestedValue(config, 'ai.gemini_api_key')}
                        >
                          打开 AI 配置助手
                        </Button>
                        {!getNestedValue(config, 'ai.gemini_api_key') && (
                          <Alert status="info" size="sm" borderRadius="md">
                            <AlertIcon />
                            <AlertDescription fontSize="xs">
                              请先配置 Gemini API Key 以使用 AI 配置助手功能
                            </AlertDescription>
                          </Alert>
                        )}
                      </VStack>
                    </ConfigCard>

                    <ConfigCard title="新聞監控配置" icon={<InfoIcon />}>
                      <VStack spacing={4} align="stretch">
                        <Flex justify="space-between" align="center">
                          <Box>
                            <Text fontWeight="600">啟用新聞監控</Text>
                            <Text fontSize="xs" color="gray.500">使用 Gemini 分析新闻，預测價格波動风險</Text>
                          </Box>
                          <Switch
                            colorScheme="blue"
                            isChecked={config.news_monitor?.enabled || false}
                            onChange={(e) => updateConfigField('news_monitor.enabled', e.target.checked)}
                          />
                        </Flex>
                        
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">NewsAPI Key</FormLabel>
                          {renderPasswordInput('news_monitor.news_api_key', '從 newsapi.org 獲取')}
                          <Text fontSize="xs" color="gray.500" mt={1}>
                            用於收集新聞數據。免费版每天 100 次請求，付费版無限制。
                            <a href="https://newsapi.org" target="_blank" rel="noopener noreferrer" style={{ color: '#3182ce', marginLeft: '4px' }}>
                              獲取 API Key →
                            </a>
                          </Text>
                        </FormControl>

                        <Flex justify="space-between" align="center">
                          <Box>
                            <Text fontWeight="600">Gemini 實時搜索</Text>
                            <Text fontSize="xs" color="gray.500">啟用 Gemini 實時搜索获取更多新闻</Text>
                          </Box>
                          <Switch
                            colorScheme="green"
                            isChecked={config.news_monitor?.use_gemini_search !== false}
                            onChange={(e) => updateConfigField('news_monitor.use_gemini_search', e.target.checked)}
                          />
                        </Flex>

                        <SimpleGrid columns={2} spacing={4}>
                          <FormControl>
                            <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">新聞收集间隔</FormLabel>
                            <Select
                              value={config.news_monitor?.news_collect_interval || '5m'}
                              onChange={(e) => updateConfigField('news_monitor.news_collect_interval', e.target.value)}
                              borderRadius="xl"
                              size="sm"
                            >
                              <option value="5m">5 分钟</option>
                              <option value="10m">10 分钟</option>
                              <option value="15m">15 分钟</option>
                              <option value="30m">30 分钟</option>
                            </Select>
                          </FormControl>
                          <FormControl>
                            <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">AI 分析间隔</FormLabel>
                            <Select
                              value={config.news_monitor?.analysis_interval || '30m'}
                              onChange={(e) => updateConfigField('news_monitor.analysis_interval', e.target.value)}
                              borderRadius="xl"
                              size="sm"
                            >
                              <option value="15m">15 分钟</option>
                              <option value="30m">30 分钟</option>
                              <option value="60m">60 分钟</option>
                            </Select>
                          </FormControl>
                        </SimpleGrid>

                        {!getNestedValue(config, 'news_monitor.news_api_key') && (
                          <Alert status="warning" size="sm" borderRadius="md">
                            <AlertIcon />
                            <AlertDescription fontSize="xs">
                              未配置 NewsAPI Key，新聞監控功能将無法收集新闻
                            </AlertDescription>
                          </Alert>
                        )}
                      </VStack>
                    </ConfigCard>

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
                            <Text fontSize="sm" fontWeight="600">{t('configuration.useTestnet')}</Text>
                          </HStack>
                          <HStack>
                            <Text fontSize="xs" color="gray.500">{t('configuration.feeRate')}</Text>
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
                    <ConfigCard title={t('configuration.globalNotificationSwitch')} icon={<BellIcon />}>
                      <Flex justify="space-between" align="center">
                        <Text fontWeight="600">{t('configuration.enableNotifications')}</Text>
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
                      <ConfigCard title={t('configuration.email')}>
                        <FormControl mb={4}>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.emailProvider')}</FormLabel>
                          <Select
                            value={config.notifications?.email?.provider || 'smtp'}
                            onChange={(e) => updateConfigField('notifications.email.provider', e.target.value)}
                            borderRadius="xl"
                          >
                            <option value="smtp">SMTP</option>
                            <option value="resend">Resend</option>
                            <option value="mailgun">Mailgun</option>
                          </Select>
                        </FormControl>
                        {config.notifications?.email?.provider === 'smtp' && (
                          <>
                            <FormControl mb={4}>
                              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.smtpHost')}</FormLabel>
                              <Input
                                value={config.notifications?.email?.smtp?.host || ''}
                                onChange={(e) => updateConfigField('notifications.email.smtp.host', e.target.value)}
                                borderRadius="xl"
                              />
                            </FormControl>
                            <FormControl mb={4}>
                              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.smtpPort')}</FormLabel>
                              <NumberInput value={config.notifications?.email?.smtp?.port || 587} onChange={(_, v) => updateConfigField('notifications.email.smtp.port', v)}>
                                <NumberInputField borderRadius="xl" />
                              </NumberInput>
                            </FormControl>
                            <FormControl mb={4}>
                              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.smtpUsername')}</FormLabel>
                              <Input
                                value={config.notifications?.email?.smtp?.username || ''}
                                onChange={(e) => updateConfigField('notifications.email.smtp.username', e.target.value)}
                                borderRadius="xl"
                              />
                            </FormControl>
                            <FormControl mb={4}>
                              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.smtpPassword')}</FormLabel>
                              {renderPasswordInput('notifications.email.smtp.password')}
                            </FormControl>
                          </>
                        )}
                        {config.notifications?.email?.provider === 'resend' && (
                          <FormControl mb={4}>
                            <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.resendApiKey')}</FormLabel>
                            {renderPasswordInput('notifications.email.resend.api_key')}
                          </FormControl>
                        )}
                        {config.notifications?.email?.provider === 'mailgun' && (
                          <>
                            <FormControl mb={4}>
                              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.mailgunApiKey')}</FormLabel>
                              {renderPasswordInput('notifications.email.mailgun.api_key')}
                            </FormControl>
                            <FormControl mb={4}>
                              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.mailgunDomain')}</FormLabel>
                              <Input
                                value={config.notifications?.email?.mailgun?.domain || ''}
                                onChange={(e) => updateConfigField('notifications.email.mailgun.domain', e.target.value)}
                                borderRadius="xl"
                              />
                            </FormControl>
                          </>
                        )}
                        <FormControl mb={4}>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.emailFrom')}</FormLabel>
                          <Input
                            value={config.notifications?.email?.from || ''}
                            onChange={(e) => updateConfigField('notifications.email.from', e.target.value)}
                            placeholder="alerts@yourdomain.com"
                            borderRadius="xl"
                          />
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.emailTo')}</FormLabel>
                          <Input
                            value={config.notifications?.email?.to || ''}
                            onChange={(e) => updateConfigField('notifications.email.to', e.target.value)}
                            placeholder="admin@yourdomain.com"
                            borderRadius="xl"
                          />
                        </FormControl>
                      </ConfigCard>
                      <ConfigCard title={t('configuration.feishu')}>
                        <FormControl mb={4}>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.webhookUrl')}</FormLabel>
                          <Input
                            value={config.notifications?.feishu?.webhook || ''}
                            onChange={(e) => updateConfigField('notifications.feishu.webhook', e.target.value)}
                            placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/..."
                            borderRadius="xl"
                          />
                        </FormControl>
                      </ConfigCard>
                      <ConfigCard title={t('configuration.dingtalk')}>
                        <FormControl mb={4}>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.webhookUrl')}</FormLabel>
                          <Input
                            value={config.notifications?.dingtalk?.webhook || ''}
                            onChange={(e) => updateConfigField('notifications.dingtalk.webhook', e.target.value)}
                            placeholder="https://oapi.dingtalk.com/robot/send?access_token=..."
                            borderRadius="xl"
                          />
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.dingtalkSecret')}</FormLabel>
                          {renderPasswordInput('notifications.dingtalk.secret')}
                        </FormControl>
                      </ConfigCard>
                      <ConfigCard title={t('configuration.wechatWork')}>
                        <FormControl mb={4}>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.webhookUrl')}</FormLabel>
                          <Input
                            value={config.notifications?.wechat_work?.webhook || ''}
                            onChange={(e) => updateConfigField('notifications.wechat_work.webhook', e.target.value)}
                            placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=..."
                            borderRadius="xl"
                          />
                        </FormControl>
                      </ConfigCard>
                      <ConfigCard title={t('configuration.slack')}>
                        <FormControl mb={4}>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.webhookUrl')}</FormLabel>
                          <Input
                            value={config.notifications?.slack?.webhook || ''}
                            onChange={(e) => updateConfigField('notifications.slack.webhook', e.target.value)}
                            placeholder="https://hooks.slack.com/services/..."
                            borderRadius="xl"
                          />
                        </FormControl>
                      </ConfigCard>
                    </SimpleGrid>
                  </VStack>
                )}

                {tabIndex === 3 && (
                  <SimpleGrid columns={2} spacing={6}>
                    <ConfigCard title={t('configuration.dataStorage')} icon={<SettingsIcon />}>
                      <FormControl mb={4} display="flex" alignItems="center">
                        <FormLabel fontSize="xs" fontWeight="bold" mb={0} flex="1">
                          {t('configuration.storageEnabled', '启用数据存储')}
                        </FormLabel>
                        <Switch
                          isChecked={config.storage?.enabled !== false}
                          onChange={(e) => updateConfigField('storage.enabled', e.target.checked)}
                          colorScheme="blue"
                        />
                      </FormControl>
                      <Text fontSize="xs" color="gray.500" mb={3}>
                        {t('configuration.storageEnabledHint')}
                      </Text>
                      <FormControl mb={4}>
                        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.databasePath')}</FormLabel>
                        <Input
                          value={config.storage?.path || ''}
                          onChange={(e) => updateConfigField('storage.path', e.target.value)}
                          borderRadius="xl"
                        />
                      </FormControl>
                      <HStack spacing={4}>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.buffer')}</FormLabel>
                          <NumberInput value={config.storage?.buffer_size || 1000} onChange={(_, v) => updateConfigField('storage.buffer_size', v)}>
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.flushInterval')}</FormLabel>
                          <NumberInput value={config.storage?.flush_interval || 5} onChange={(_, v) => updateConfigField('storage.flush_interval', v)}>
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                        </FormControl>
                      </HStack>
                    </ConfigCard>
                    <ConfigCard title={t('configuration.webService')} icon={<SettingsIcon />}>
                      <FormControl mb={4}>
                        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.listenPort')}</FormLabel>
                        <NumberInput value={config.web?.port || 28888} onChange={(_, v) => updateConfigField('web.port', v)}>
                          <NumberInputField borderRadius="xl" />
                        </NumberInput>
                      </FormControl>
                      <FormControl>
                        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.apiKeyOptional')}</FormLabel>
                        {renderPasswordInput('web.api_key')}
                      </FormControl>
                    </ConfigCard>
                  </SimpleGrid>
                )}

                {tabIndex === 4 && (
                  <VStack spacing={6} align="stretch">
                    <ConfigCard title="YAML 配置编辑器" icon={<SettingsIcon />}>
                      <VStack spacing={4} align="stretch">
                        <HStack justify="space-between">
                          <Text fontSize="sm" color="gray.500">
                            直接编辑完整的 YAML 配置文件，支援语法高亮和錯誤提示
                          </Text>
                          <HStack spacing={2}>
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={loadYamlContent}
                              isLoading={yamlLoading}
                            >
                              刷新
                            </Button>
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={handleYamlValidate}
                              isDisabled={!yamlContent}
                            >
                              驗证
                            </Button>
                            <Button
                              size="sm"
                              colorScheme="blue"
                              variant="outline"
                              onClick={handleYamlPreview}
                              isDisabled={!yamlContent || yamlContent === originalYamlContent}
                            >
                              預览变更
                            </Button>
                          </HStack>
                        </HStack>

                        {yamlError && (
                          <Alert status="error" borderRadius="md">
                            <AlertIcon />
                            <AlertDescription>{yamlError}</AlertDescription>
                          </Alert>
                        )}

                        {yamlLoading ? (
                          <Center py={20}>
                            <Spinner size="lg" />
                          </Center>
                        ) : yamlContent ? (
                          <YamlEditor
                            value={yamlContent}
                            onChange={(val) => setYamlContent(val)}
                            onValidate={(valid, err) => {
                              setYamlValid(valid)
                              if (err) setYamlError(err)
                            }}
                            height="60vh"
                          />
                        ) : (
                          <Center py={10}>
                            <VStack spacing={3}>
                              <Text color="gray.500">点击"刷新"加載配置</Text>
                              <Button onClick={loadYamlContent}>加載配置</Button>
                            </VStack>
                          </Center>
                        )}
                      </VStack>
                    </ConfigCard>
                  </VStack>
                )}

                {tabIndex === 5 && (
                  <VStack spacing={6} align="stretch">
                    <ConfigCard title="配置历史版本" icon={<RepeatIcon />}>
                      <Text fontSize="sm" color="gray.500" mb={4}>
                        查看和管理配置的历史版本，支援版本對比和恢複
                      </Text>
                      <ConfigHistory
                        onRestore={() => {
                          loadConfig()
                          loadYamlContent()
                        }}
                      />
                    </ConfigCard>
                  </VStack>
                )}
              </>
            ) : (
              <>
                {tabIndex === 0 && (
                  <VStack spacing={6} align="stretch">
                    <ConfigCard title={t('configuration.tradingPairParams', { symbol: selectedSymbol })} icon={<RepeatIcon />}>
                      <SimpleGrid columns={2} spacing={6}>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.priceInterval')}</FormLabel>
                          <NumberInput
                            value={(getSelectedSymbolConfig()?.price_interval ?? config.trading?.price_interval) || 0}
                            onChange={(_, v) => updateSelectedSymbolField('price_interval', v)}
                            precision={6}
                            step={0.01}
                          >
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.orderAmount')}</FormLabel>
                          <NumberInput
                            value={(getSelectedSymbolConfig()?.order_quantity ?? config.trading?.order_quantity) || 0}
                            onChange={(_, v) => updateSelectedSymbolField('order_quantity', v)}
                            precision={2}
                            min={selectedExchange === 'binance' ? 100 : 0}
                          >
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                          {selectedExchange === 'binance' && (
                            <Text fontSize="xs" color="orange.600" mt={1}>
                              币安合約最小下單名义金額通常要求 ≥ 100 USDT。為避免數量精度/步進導致 99.x 的临界失败，建议設置 ≥ 105。
                            </Text>
                          )}
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.buyWindowSize')}</FormLabel>
                          <NumberInput
                            value={(getSelectedSymbolConfig()?.buy_window_size ?? config.trading?.buy_window_size) || 0}
                            onChange={(_, v) => updateSelectedSymbolField('buy_window_size', v)}
                          >
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.sellWindowSize')}</FormLabel>
                          <NumberInput
                            value={(getSelectedSymbolConfig()?.sell_window_size ?? config.trading?.sell_window_size) || 0}
                            onChange={(_, v) => updateSelectedSymbolField('sell_window_size', v)}
                          >
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                        </FormControl>
                      </SimpleGrid>
                    </ConfigCard>

                    <ConfigCard title={t('configuration.dynamicAdjustment')} icon={<SettingsIcon />}>
                      <Flex justify="space-between" align="center" mb={4}>
                        <Box>
                          <Text fontWeight="600">{t('configuration.enableDynamicAdjustment')}</Text>
                          <Text fontSize="xs" color="gray.500">{t('configuration.enableDynamicAdjustmentDesc')}</Text>
                        </Box>
                        <Switch
                          isChecked={getNestedValue(config, 'trading.dynamic_adjustment.enabled') || false}
                          onChange={(e) => updateConfigField('trading.dynamic_adjustment.enabled', e.target.checked)}
                        />
                      </Flex>
                      <Flex justify="space-between" align="center" mb={4}>
                        <Box>
                          <Text fontWeight="600">{t('configuration.enableDynamicOrderQty')}</Text>
                          <Text fontSize="xs" color="gray.500">{t('configuration.enableDynamicOrderQtyDesc')}</Text>
                        </Box>
                        <Switch
                          isChecked={getNestedValue(config, 'trading.dynamic_adjustment.order_quantity.enabled') || false}
                          onChange={(e) => updateConfigField('trading.dynamic_adjustment.order_quantity.enabled', e.target.checked)}
                        />
                      </Flex>
                      <SimpleGrid columns={2} spacing={6}>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.dynamicOrderQtyMin')}</FormLabel>
                          <NumberInput
                            value={getNestedValue(config, 'trading.dynamic_adjustment.order_quantity.min') ?? 50}
                            onChange={(_, v) => updateConfigField('trading.dynamic_adjustment.order_quantity.min', v ?? 50)}
                            min={10}
                            max={10000}
                          >
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.dynamicOrderQtyMinHint')}</Text>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.dynamicOrderQtyMax')}</FormLabel>
                          <NumberInput
                            value={getNestedValue(config, 'trading.dynamic_adjustment.order_quantity.max') ?? 500}
                            onChange={(_, v) => updateConfigField('trading.dynamic_adjustment.order_quantity.max', v ?? 500)}
                            min={50}
                            max={100000}
                          >
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.dynamicOrderQtyMaxHint')}</Text>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.dynamicOrderQtyFreqThreshold')}</FormLabel>
                          <NumberInput
                            value={getNestedValue(config, 'trading.dynamic_adjustment.order_quantity.frequency_threshold') ?? 5}
                            onChange={(_, v) => updateConfigField('trading.dynamic_adjustment.order_quantity.frequency_threshold', v ?? 5)}
                            min={1}
                            max={60}
                          >
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.dynamicOrderQtyFreqThresholdHint')}</Text>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.dynamicOrderQtyStep')}</FormLabel>
                          <NumberInput
                            value={getNestedValue(config, 'trading.dynamic_adjustment.order_quantity.adjustment_step') ?? 20}
                            onChange={(_, v) => updateConfigField('trading.dynamic_adjustment.order_quantity.adjustment_step', v ?? 20)}
                            min={1}
                            max={500}
                          >
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.dynamicOrderQtyStepHint')}</Text>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.dynamicOrderQtyCheckInterval')}</FormLabel>
                          <NumberInput
                            value={getNestedValue(config, 'trading.dynamic_adjustment.order_quantity.check_interval') ?? 60}
                            onChange={(_, v) => updateConfigField('trading.dynamic_adjustment.order_quantity.check_interval', v ?? 60)}
                            min={30}
                            max={600}
                          >
                            <NumberInputField borderRadius="xl" />
                          </NumberInput>
                          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.dynamicOrderQtyCheckIntervalHint')}</Text>
                        </FormControl>
                      </SimpleGrid>
                    </ConfigCard>
                  </VStack>
                )}

                {tabIndex === 1 && (
                  <>
                  <ConfigCard title={t('configuration.riskControlSettings')} icon={<LockIcon />}>
                    <Flex justify="space-between" align="center" mb={6}>
                      <Box>
                        <Text fontWeight="600">{t('configuration.enableRiskEngine')}</Text>
                        <Text fontSize="xs" color="gray.500">{t('configuration.enableRiskEngineDesc')}</Text>
                      </Box>
                      <Switch
                        colorScheme="orange"
                        isChecked={config.risk_control?.enabled || false}
                        onChange={(e) => updateConfigField('risk_control.enabled', e.target.checked)}
                      />
                    </Flex>
                    <SimpleGrid columns={2} spacing={6}>
                      <FormControl>
                        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.maxLeverage')}</FormLabel>
                        <NumberInput value={config.risk_control?.max_leverage || 0} onChange={(_, v) => updateConfigField('risk_control.max_leverage', v)}>
                          <NumberInputField borderRadius="xl" />
                        </NumberInput>
                      </FormControl>
                      <FormControl>
                        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.volumeMultiplier')}</FormLabel>
                        <NumberInput value={config.risk_control?.volume_multiplier || 0} onChange={(_, v) => updateConfigField('risk_control.volume_multiplier', v)} precision={1}>
                          <NumberInputField borderRadius="xl" />
                        </NumberInput>
                      </FormControl>
                    </SimpleGrid>
                  </ConfigCard>

                  <ConfigCard title="新聞監控" icon={<BellIcon />}>
                    <Flex justify="space-between" align="center" mb={6}>
                      <Box>
                        <Text fontWeight="600">啟用新聞監控</Text>
                        <Text fontSize="xs" color="gray.500">使用 Gemini 分析新闻，預测價格波動风險</Text>
                      </Box>
                      <Switch
                        colorScheme="blue"
                        isChecked={config.news_monitor?.enabled || false}
                        onChange={(e) => updateConfigField('news_monitor.enabled', e.target.checked)}
                      />
                    </Flex>
                    <FormControl mb={4}>
                      <FormLabel fontSize="xs" fontWeight="bold">NewsAPI Key</FormLabel>
                      {renderPasswordInput('news_monitor.news_api_key', '用於收集新闻')}
                    </FormControl>
                    <FormControl mb={4}>
                      <FormLabel fontSize="xs" fontWeight="bold">Gemini 搜索</FormLabel>
                      <Switch
                        isChecked={config.news_monitor?.use_gemini_search !== false}
                        onChange={(e) => updateConfigField('news_monitor.use_gemini_search', e.target.checked)}
                      />
                      <Text fontSize="xs" color="gray.500" mt={1}>啟用 Gemini 實時搜索分析</Text>
                    </FormControl>
                    <SimpleGrid columns={2} spacing={6}>
                      <FormControl>
                        <FormLabel fontSize="xs" fontWeight="bold">分析间隔</FormLabel>
                        <Select
                          value={config.news_monitor?.analysis_interval || '30m'}
                          onChange={(e) => updateConfigField('news_monitor.analysis_interval', e.target.value)}
                          borderRadius="xl"
                        >
                          <option value="15m">15 分钟</option>
                          <option value="30m">30 分钟</option>
                          <option value="60m">60 分钟</option>
                        </Select>
                      </FormControl>
                      <FormControl>
                        <FormLabel fontSize="xs" fontWeight="bold">暂停交易阈值</FormLabel>
                        <NumberInput
                          value={(config.news_monitor?.risk_thresholds?.stop_trading_probability ?? 0.7) * 100}
                          onChange={(_, v) => updateConfigField('news_monitor.risk_thresholds.stop_trading_probability', (v || 70) / 100)}
                          min={50}
                          max={100}
                        >
                          <NumberInputField borderRadius="xl" />
                        </NumberInput>
                        <Text fontSize="xs" color="gray.500">概率超過此值暂停交易 (%)</Text>
                      </FormControl>
                    </SimpleGrid>
                  </ConfigCard>
                  </>
                )}

                {tabIndex === 2 && (
                  <VStack spacing={6} align="stretch">
                    <ConfigCard title={t('configuration.aiDecisionEngine')} icon={<StarIcon />}>
                      <Flex justify="space-between" align="center" mb={6}>
                        <Box>
                          <Text fontWeight="600">{t('configuration.enableAIAssist')}</Text>
                          <Text fontSize="xs" color="gray.500">{t('configuration.enableAIAssistDesc')}</Text>
                        </Box>
                        <Switch
                          colorScheme="purple"
                          isChecked={config.ai?.enabled || false}
                          onChange={(e) => updateConfigField('ai.enabled', e.target.checked)}
                        />
                      </Flex>
                      <SimpleGrid columns={2} spacing={6}>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.provider')}</FormLabel>
                          <Select value={config.ai?.provider || ''} onChange={(e) => updateConfigField('ai.provider', e.target.value)} borderRadius="xl">
                            <option value="gemini">Gemini</option>
                            <option value="openai">OpenAI</option>
                          </Select>
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.decisionMode')}</FormLabel>
                          <Select value={config.ai?.decision_mode || ''} onChange={(e) => updateConfigField('ai.decision_mode', e.target.value)} borderRadius="xl">
                            <option value="advisor">{t('configuration.advisorMode')}</option>
                            <option value="executor">{t('configuration.executorMode')}</option>
                          </Select>
                        </FormControl>
                      </SimpleGrid>
                      <FormControl mt={4}>
                        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.apiKey')}</FormLabel>
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
            <ModalHeader>{t('configuration.confirmChanges')}</ModalHeader>
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
              <Button variant="ghost" mr={3} onClick={onPreviewClose}>{t('configuration.cancel')}</Button>
              <Button colorScheme="blue" onClick={handleSave} isLoading={saving}>{t('configuration.confirmSave')}</Button>
            </ModalFooter>
          </ModalContent>
        </Modal>

        {/* Backups Modal */}
        <Modal isOpen={isBackupsOpen} onClose={onBackupsClose} size="lg">
          <ModalOverlay backdropFilter="blur(4px)" />
          <ModalContent borderRadius="2xl">
            <ModalHeader>{t('configuration.backupManagementTitle')}</ModalHeader>
            <ModalCloseButton />
            <ModalBody>
              <TableContainer>
                <Table variant="simple" size="sm">
                  <Thead><Tr><Th>{t('configuration.time')}</Th><Th>{t('configuration.size')}</Th><Th>{t('configuration.action')}</Th></Tr></Thead>
                  <Tbody>
                    {backups.map((b) => (
                      <Tr key={b.id}>
                        <Td>{new Date(b.timestamp).toLocaleString()}</Td>
                        <Td>{(b.size / 1024).toFixed(1)}KB</Td>
                        <Td>
                          <Button size="xs" variant="link" colorScheme="blue" onClick={() => {}}>{t('configuration.restore')}</Button>
                        </Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </TableContainer>
            </ModalBody>
          </ModalContent>
        </Modal>

        {/* AI Config Wizard */}
        <AIConfigWizard
          isOpen={isAIWizardOpen}
          onClose={onAIWizardClose}
          onSuccess={() => {
            loadConfig()
            onAIWizardClose()
          }}
          exchange={config?.app?.current_exchange || 'binance'}
          symbols={config?.trading?.symbols?.map((s: any) => s.symbol) || []}
        />

        {/* YAML Diff Preview Modal */}
        <DiffPreviewModal
          isOpen={isDiffOpen}
          onClose={onDiffClose}
          onConfirm={handleYamlSave}
          oldValue={originalYamlContent}
          newValue={yamlContent}
          oldTitle="當前配置"
          newTitle="修改后的配置"
          title="配置变更預览"
          confirmText="确认保存"
          isLoading={yamlSaving}
        />
      </VStack>
    </Container>
  )
}

export default Configuration
