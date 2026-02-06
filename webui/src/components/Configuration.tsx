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
  Slider,
  SliderTrack,
  SliderFilledTrack,
  SliderThumb,
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
  getSecurityStatus,
  generateMasterKey,
  Config,
  BackupInfo,
  ConfigDiff,
} from '../services/config'
import {
  getPendingOrders,
  batchCancelOrders,
  closeAllPositions,
  stopTrading,
  startTrading,
  getPriceRange,
} from '../services/api'
import type { PriceRangeData } from '../services/api'
import AIConfigWizard from './AIConfigWizard'
import SymbolManager from './SymbolManager'
import YamlEditor from './YamlEditor'
import DiffPreviewModal from './DiffPreviewModal'
import ConfigHistory from './ConfigHistory'
import ConfirmDialog from './ConfirmDialog'
import ParamAdvisor from './ParamAdvisor'
import { trackConfigSaved } from '../services/telemetry'

const MotionBox = motion(Box)

const WINDOW_SIZE_PRESETS = [10, 20, 30, 50, 100] as const

const WindowSizeSlider: React.FC<{
  value: number
  onChange: (v: number) => void
  size?: 'sm' | 'md'
}> = ({ value, onChange, size = 'md' }) => {
  const displayValue = Math.max(1, Math.min(100, value || 10))
  const handleChange = (v: number) => onChange(Math.max(1, Math.min(100, v)))
  return (
    <VStack align="stretch" spacing={2}>
      <HStack spacing={3} align="center">
        <Slider
          flex={1}
          value={displayValue}
          min={1}
          max={100}
          step={1}
          onChange={handleChange}
        >
          <SliderTrack bg="gray.200">
            <SliderFilledTrack bg="blue.500" />
          </SliderTrack>
          <SliderThumb boxSize={size === 'sm' ? 3 : 4} />
        </Slider>
        <Text fontWeight="bold" minW={8} textAlign="right" fontSize={size === 'sm' ? 'sm' : 'md'}>
          {displayValue}
        </Text>
      </HStack>
      <HStack flexWrap="wrap" gap={1}>
        {WINDOW_SIZE_PRESETS.map((preset) => (
          <Button
            key={preset}
            size="xs"
            variant={displayValue === preset ? 'solid' : 'outline'}
            colorScheme="blue"
            onClick={() => handleChange(preset)}
          >
            {preset}
          </Button>
        ))}
      </HStack>
    </VStack>
  )
}

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
  const [feeRateInputs, setFeeRateInputs] = useState<Record<string, string>>({})
  const [directionConfirm, setDirectionConfirm] = useState<{ isOpen: boolean; newDirection: string; loading: boolean }>({
    isOpen: false,
    newDirection: '',
    loading: false,
  })
  
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
  
  // Security settings state
  const [securityStatus, setSecurityStatus] = useState<{
    encryption_enabled: boolean
    master_key_path: string
    master_key_exists: boolean
  } | null>(null)
  const [generatingKey, setGeneratingKey] = useState(false)
  
  // Price range state
  const [priceRange, setPriceRange] = useState<PriceRangeData | null>(null)
  const [priceRangeSource, setPriceRangeSource] = useState<string>('')
  const [priceRangeLoading, setPriceRangeLoading] = useState(false)
  const [hotUpdatedSymbols, setHotUpdatedSymbols] = useState<string[]>([])
  
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
      const nextFeeRateInputs: Record<string, string> = {}
      Object.keys(cfg.exchanges || {}).forEach((exchange) => {
        const value = (cfg.exchanges as any)?.[exchange]?.fee_rate
        nextFeeRateInputs[exchange] = value === undefined || value === null ? '' : String(value)
      })
      setFeeRateInputs(nextFeeRateInputs)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('configuration.loadFailed'))
    } finally {
      setLoading(false)
    }
  }

  const fetchPriceRange = async () => {
    if (!selectedExchange || !selectedSymbol) return
    setPriceRangeLoading(true)
    try {
      const res = await getPriceRange(selectedExchange, selectedSymbol)
      if (res.success) {
        setPriceRange(res.data)
        setPriceRangeSource(res.source)
      }
    } catch {
      setPriceRange(null)
      setPriceRangeSource('')
    } finally {
      setPriceRangeLoading(false)
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
        title: t('configuration.loadFailedToast'),
        description: err instanceof Error ? err.message : t('configuration.loadYamlFailed'),
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
          title: t('configuration.validationPassed'),
          description: t('configuration.configSyntaxCorrect'),
          status: 'success',
          duration: 2000,
        })
      } else {
        toast({
          title: t('configuration.validationFailed'),
          description: result.error,
          status: 'error',
          duration: 5000,
        })
      }
    } catch (err) {
      toast({
        title: t('configuration.validationFailed'),
        description: err instanceof Error ? err.message : t('configuration.validateConfigFailed'),
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
        title: t('configuration.configInvalid'),
        description: result.error || t('configuration.fixConfigErrors'),
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
      
      // 追踪配置保存事件
      trackConfigSaved('yaml')
      
      toast({
        title: t('configuration.saveSuccess'),
        description: result.requires_restart ? t('configuration.requiresRestart') : t('configuration.configUpdated'),
        status: 'success',
        duration: 3000,
      })
      
      onDiffClose()
      setOriginalYamlContent(yamlContent)
      
      // 刷新 JSON 配置
      await loadConfig()
    } catch (err) {
      toast({
        title: t('configuration.saveFailed'),
        description: err instanceof Error ? err.message : t('configuration.saveConfigFailed'),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setYamlSaving(false)
    }
  }

  const loadSecurityStatus = async () => {
    try {
      const status = await getSecurityStatus()
      setSecurityStatus(status)
    } catch (err) {
      console.error('加载安全状态失败:', err)
    }
  }

  const handleGenerateMasterKey = async () => {
    try {
      setGeneratingKey(true)
      const result = await generateMasterKey()
      toast({
        title: t('configuration.generateSuccess'),
        description: t('configuration.masterKeyGenerated', { path: result.master_key_path }),
        status: 'success',
        duration: 5000,
      })
      await loadSecurityStatus()
    } catch (err) {
      toast({
        title: t('configuration.generateFailed'),
        description: err instanceof Error ? err.message : t('configuration.generateMasterKeyFailed'),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setGeneratingKey(false)
    }
  }

  useEffect(() => {
    loadConfig()
    loadBackups()
    loadSecurityStatus()
  }, [])

  // Fetch price range when symbol changes
  useEffect(() => {
    if (selectedExchange && selectedSymbol) {
      fetchPriceRange()
    } else {
      setPriceRange(null)
    }
  }, [selectedExchange, selectedSymbol])

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
      // 追踪配置保存事件
      trackConfigSaved(isGlobalView ? 'global' : 'symbol')
      setSuccess(result.message)
      onPreviewClose()
      
      // Check if trading params were hot-updated
      const hotUpdated = (result as any).hot_updated as string[] | undefined
      if (hotUpdated && hotUpdated.length > 0) {
        setHotUpdatedSymbols(hotUpdated)
        toast({ 
          title: t('configuration.saveSuccess'), 
          description: t('configuration.priceRangeHotUpdateSuccess'),
          status: 'success',
          duration: 5000,
          isClosable: true
        })
      } else {
        toast({ 
          title: t('configuration.saveSuccess'), 
          status: 'success',
          duration: 3000,
          isClosable: true
        })
      }
      await loadConfig()
      // Refresh price range after config save
      if (selectedExchange && selectedSymbol) {
        setTimeout(fetchPriceRange, 500)
      }
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
        profit_spread: newConfig.trading.profit_spread ?? 0,
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
    binance: t('exchanges.binance'),
    bitget: t('exchanges.bitget'),
    bybit: t('exchanges.bybit'),
    gate: t('exchanges.gate'),
    edgex: t('exchanges.edgex'),
    bit: t('exchanges.bit'),
  }

  if (loading) return <Center h="400px"><Spinner size="xl" thickness="4px" color="blue.500" /></Center>
  if (!config) return <Container maxW="container.xl" py={8}><Alert status="error"><AlertIcon />{t('configuration.loadFailed')}</Alert></Container>

  const globalTabs = [t('configuration.globalTabs.general'), t('configuration.globalTabs.exchangeAPI'), t('configuration.globalTabs.notifications'), t('configuration.globalTabs.storageWeb'), t('configuration.globalTabs.security'), t('configuration.globalTabs.yamlEditor'), t('configuration.globalTabs.history')]
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
                    <ConfigCard title={t('configuration.tradingPairManagement')} icon={<RepeatIcon />}>
                      <FormControl mb={4}>
                        <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.defaultDirection')}</FormLabel>
                        <Select
                          value={config.trading?.direction || 'LONG'}
                          onChange={(e) => updateConfigField('trading.direction', e.target.value)}
                          borderRadius="xl"
                          maxW="200px"
                        >
                          <option value="LONG">{t('configuration.directionLong')}</option>
                          <option value="SHORT">{t('configuration.directionShort')}</option>
                        </Select>
                        <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.defaultDirectionDesc')}</Text>
                      </FormControl>
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
                              description: t('configuration.pairConfigAutoSaved'),
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
                    <ConfigCard title={t('configuration.aiConfigAssistant')} icon={<StarIcon />}>
                      <VStack spacing={4} align="stretch">
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">Gemini API Key</FormLabel>
                          {renderPasswordInput('ai.gemini_api_key', t('configuration.geminiApiKeyPlaceholder'))}
                          <Text fontSize="xs" color="gray.500" mt={1}>
                            {t('configuration.geminiApiKeyDesc')}
                          </Text>
                        </FormControl>
                        
                        <Button
                          leftIcon={<StarIcon />}
                          colorScheme="purple"
                          variant="outline"
                          onClick={onAIWizardOpen}
                          isDisabled={!getNestedValue(config, 'ai.gemini_api_key')}
                        >
                          {t('configuration.openAIAssistant')}
                        </Button>
                        {!getNestedValue(config, 'ai.gemini_api_key') && (
                          <Alert status="info" size="sm" borderRadius="md">
                            <AlertIcon />
                            <AlertDescription fontSize="xs">
                              {t('configuration.configureGeminiFirst')}
                            </AlertDescription>
                          </Alert>
                        )}
                      </VStack>
                    </ConfigCard>

                    <ConfigCard title={t('configuration.newsMonitorConfig')} icon={<InfoIcon />}>
                      <VStack spacing={4} align="stretch">
                        <Flex justify="space-between" align="center">
                          <Box>
                            <Text fontWeight="600">{t('configuration.enableNewsMonitor')}</Text>
                            <Text fontSize="xs" color="gray.500">{t('configuration.newsMonitorDesc')}</Text>
                          </Box>
                          <Switch
                            colorScheme="blue"
                            isChecked={config.news_monitor?.enabled || false}
                            onChange={(e) => updateConfigField('news_monitor.enabled', e.target.checked)}
                          />
                        </Flex>

                        {config.news_monitor?.enabled && (
                          <Flex justify="space-between" align="center">
                            <Box>
                              <Text fontWeight="600">{t('configuration.enableNewsAnalysis')}</Text>
                              <Text fontSize="xs" color="gray.500">{t('configuration.newsAnalysisDesc')}</Text>
                            </Box>
                            <Switch
                              colorScheme="blue"
                              isChecked={config.news_monitor?.enable_analysis !== false}
                              onChange={(e) => updateConfigField('news_monitor.enable_analysis', e.target.checked)}
                            />
                          </Flex>
                        )}
                        
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">NewsAPI Key</FormLabel>
                          {renderPasswordInput('news_monitor.news_api_key', t('configuration.newsApiKeyFromSite'))}
                          <Text fontSize="xs" color="gray.500" mt={1}>
                            {t('configuration.newsApiKeyDesc')}
                            <a href="https://newsapi.org" target="_blank" rel="noopener noreferrer" style={{ color: '#3182ce', marginLeft: '4px' }}>
                              {t('configuration.getApiKey')}
                            </a>
                          </Text>
                        </FormControl>

                        <Flex justify="space-between" align="center">
                          <Box>
                            <Text fontWeight="600">{t('configuration.geminiRealtimeSearch')}</Text>
                            <Text fontSize="xs" color="gray.500">{t('configuration.geminiRealtimeSearchDesc')}</Text>
                          </Box>
                          <Switch
                            colorScheme="green"
                            isChecked={config.news_monitor?.use_gemini_search !== false}
                            onChange={(e) => updateConfigField('news_monitor.use_gemini_search', e.target.checked)}
                          />
                        </Flex>

                        <Divider />
                        
                        <Text fontSize="sm" fontWeight="600" color="gray.700">{t('configuration.aiProviderConfig')}</Text>
                        
                        <SimpleGrid columns={2} spacing={4}>
                          <FormControl>
                            <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">AI Provider</FormLabel>
                            <Select
                              value={config.news_monitor?.ai_provider?.provider || 'gemini'}
                              onChange={(e) => {
                                updateConfigField('news_monitor.ai_provider.provider', e.target.value)
                                // 切换provider时清空model，让用户重新选择
                                updateConfigField('news_monitor.ai_provider.model', '')
                              }}
                              borderRadius="xl"
                              size="sm"
                            >
                              <option value="gemini">Gemini</option>
                              <option value="openai">OpenAI</option>
                              <option value="claude">Claude (Anthropic)</option>
                              <option value="poe">Poe</option>
                            </Select>
                          </FormControl>
                          <FormControl>
                            <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.model')}</FormLabel>
                            <Select
                              value={config.news_monitor?.ai_provider?.model || ''}
                              onChange={(e) => updateConfigField('news_monitor.ai_provider.model', e.target.value)}
                              borderRadius="xl"
                              size="sm"
                              placeholder={t('configuration.useDefaultModel')}
                            >
                              {config.news_monitor?.ai_provider?.provider === 'gemini' && (
                                <>
                                  <option value="gemini-3-flash-preview">gemini-3-flash-preview</option>
                                  <option value="gemini-pro">gemini-pro</option>
                                  <option value="gemini-1.5-pro">gemini-1.5-pro</option>
                                </>
                              )}
                              {config.news_monitor?.ai_provider?.provider === 'openai' && (
                                <>
                                  <option value="gpt-4">gpt-4</option>
                                  <option value="gpt-4-turbo">gpt-4-turbo</option>
                                  <option value="gpt-3.5-turbo">gpt-3.5-turbo</option>
                                </>
                              )}
                              {config.news_monitor?.ai_provider?.provider === 'claude' && (
                                <>
                                  <option value="claude-3-opus-20240229">claude-3-opus</option>
                                  <option value="claude-3-sonnet-20240229">claude-3-sonnet</option>
                                  <option value="claude-3-haiku-20240307">claude-3-haiku</option>
                                </>
                              )}
                              {config.news_monitor?.ai_provider?.provider === 'poe' && (
                                <>
                                  <option value="gpt-4">gpt-4</option>
                                  <option value="claude-3-opus">claude-3-opus</option>
                                  <option value="claude-3-sonnet">claude-3-sonnet</option>
                                </>
                              )}
                            </Select>
                          </FormControl>
                        </SimpleGrid>

                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">API Key</FormLabel>
                          {renderPasswordInput('news_monitor.ai_provider.api_key', t('configuration.enterApiKeyPlaceholder'))}
                          <Text fontSize="xs" color="gray.500" mt={1}>
                            {config.news_monitor?.ai_provider?.provider === 'gemini' && t('configuration.apiKeyFromGoogleAI')}
                            {config.news_monitor?.ai_provider?.provider === 'openai' && t('configuration.apiKeyFromOpenAI')}
                            {config.news_monitor?.ai_provider?.provider === 'claude' && t('configuration.apiKeyFromAnthropic')}
                            {config.news_monitor?.ai_provider?.provider === 'poe' && 'Poe API Key'}
                          </Text>
                        </FormControl>

                        {(config.news_monitor?.ai_provider?.provider === 'poe' || config.news_monitor?.ai_provider?.provider === 'openai' || config.news_monitor?.ai_provider?.provider === 'claude') && (
                          <FormControl>
                            <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.baseUrlOptional')}</FormLabel>
                            <Input
                              value={config.news_monitor?.ai_provider?.base_url || ''}
                              onChange={(e) => updateConfigField('news_monitor.ai_provider.base_url', e.target.value)}
                              placeholder={t('configuration.baseUrlPlaceholder')}
                              borderRadius="xl"
                              size="sm"
                            />
                          </FormControl>
                        )}

                        <Divider />

                        <SimpleGrid columns={2} spacing={4}>
                          <FormControl>
                            <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.newsCollectInterval')}</FormLabel>
                            <Select
                              value={config.news_monitor?.news_collect_interval || '5m'}
                              onChange={(e) => updateConfigField('news_monitor.news_collect_interval', e.target.value)}
                              borderRadius="xl"
                              size="sm"
                            >
                              <option value="5m">{t('configuration.interval5m')}</option>
                              <option value="10m">{t('configuration.interval10m')}</option>
                              <option value="15m">{t('configuration.interval15m')}</option>
                              <option value="30m">{t('configuration.interval30m')}</option>
                            </Select>
                          </FormControl>
                          <FormControl>
                            <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.aiAnalysisInterval')}</FormLabel>
                            <Select
                              value={config.news_monitor?.analysis_interval || '30m'}
                              onChange={(e) => updateConfigField('news_monitor.analysis_interval', e.target.value)}
                              borderRadius="xl"
                              size="sm"
                            >
                              <option value="5m">{t('configuration.interval5m')}</option>
                              <option value="15m">{t('configuration.interval15m')}</option>
                              <option value="30m">{t('configuration.interval30m')}</option>
                              <option value="1h">{t('configuration.interval1h')}</option>
                              <option value="2h">{t('configuration.interval2h')}</option>
                              <option value="4h">{t('configuration.interval4h')}</option>
                              <option value="8h">{t('configuration.interval8h')}</option>
                              <option value="24h">{t('configuration.interval24h')}</option>
                            </Select>
                          </FormControl>
                        </SimpleGrid>

                        {!getNestedValue(config, 'news_monitor.news_api_key') && (
                          <Alert status="warning" size="sm" borderRadius="md">
                            <AlertIcon />
                            <AlertDescription fontSize="xs">
                              {t('configuration.newsApiKeyMissing')}
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
                              value={feeRateInputs[exchange] ?? ''}
                              onChange={(value) => {
                                setFeeRateInputs((prev) => ({ ...prev, [exchange]: value }))
                              }}
                              onBlur={() => {
                                const rawValue = feeRateInputs[exchange] ?? ''
                                const trimmed = rawValue.trim()
                                const parsed = trimmed === '' ? 0 : Number(trimmed)
                                if (Number.isNaN(parsed)) {
                                  const currentValue = getNestedValue(config, `exchanges.${exchange}.fee_rate`)
                                  const fallback = currentValue === undefined || currentValue === null ? '' : String(currentValue)
                                  setFeeRateInputs((prev) => ({ ...prev, [exchange]: fallback }))
                                  return
                                }
                                updateConfigField(`exchanges.${exchange}.fee_rate`, parsed)
                                setFeeRateInputs((prev) => ({ ...prev, [exchange]: trimmed === '' ? '0' : trimmed }))
                              }}
                              precision={6}
                              step={0.0001}
                            >
                              <NumberInputField borderRadius="md" inputMode="decimal" />
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
                          {t('configuration.storageEnabled')}
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
                    <ConfigCard title={t('configuration.securitySettings')} icon={<LockIcon />}>
                      <VStack spacing={6} align="stretch">
                        <Alert status="info" borderRadius="lg">
                          <AlertIcon />
                          <AlertDescription fontSize="sm">
                            {t('configuration.securitySettingsDesc')}
                          </AlertDescription>
                        </Alert>

                        <FormControl display="flex" alignItems="center">
                          <FormLabel fontSize="sm" fontWeight="bold" mb={0} flex="1">
                            {t('configuration.enableEncryption')}
                          </FormLabel>
                          <Switch
                            colorScheme="blue"
                            isChecked={config.security?.encryption_enabled || false}
                            onChange={(e) => updateConfigField('security.encryption_enabled', e.target.checked)}
                          />
                        </FormControl>

                        {config.security?.encryption_enabled && (
                          <>
                            <FormControl>
                              <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">
                                {t('configuration.masterKeyPath')}
                              </FormLabel>
                              <Input
                                value={securityStatus?.master_key_path || config.security?.master_key_path || './data/master.key'}
                                isReadOnly
                                borderRadius="xl"
                                bg="gray.50"
                              />
                              <Text fontSize="xs" color="gray.500" mt={1}>
                                {t('configuration.masterKeyPathDesc')}
                              </Text>
                            </FormControl>

                            <Divider />

                            <VStack spacing={4} align="stretch">
                              <Text fontSize="sm" fontWeight="600">
                                {t('configuration.masterKeyManagement')}
                              </Text>
                              
                              {securityStatus?.master_key_exists ? (
                                <Alert status="success" borderRadius="md">
                                  <AlertIcon />
                                  <AlertDescription fontSize="xs">
                                    {t('configuration.masterKeyExists')}
                                  </AlertDescription>
                                </Alert>
                              ) : (
                                <Alert status="warning" borderRadius="md">
                                  <AlertIcon />
                                  <AlertDescription fontSize="xs">
                                    {t('configuration.masterKeyNotExists')}
                                  </AlertDescription>
                                </Alert>
                              )}

                              <Button
                                size="sm"
                                colorScheme="blue"
                                variant="outline"
                                onClick={handleGenerateMasterKey}
                                isLoading={generatingKey}
                                isDisabled={securityStatus?.master_key_exists || false}
                                leftIcon={<LockIcon />}
                              >
                                {t('configuration.generateMasterKey')}
                              </Button>
                            </VStack>

                            <Alert status="warning" borderRadius="md" mt={4}>
                              <AlertIcon />
                              <AlertDescription fontSize="xs">
                                {t('configuration.encryptionWarning')}
                              </AlertDescription>
                            </Alert>
                          </>
                        )}
                      </VStack>
                    </ConfigCard>
                  </VStack>
                )}

                {tabIndex === 5 && (
                  <VStack spacing={6} align="stretch">
                    <ConfigCard title={t('configuration.yamlEditorTitle')} icon={<SettingsIcon />}>
                      <VStack spacing={4} align="stretch">
                        <HStack justify="space-between">
                          <Text fontSize="sm" color="gray.500">
                            {t('configuration.yamlEditorDesc')}
                          </Text>
                          <HStack spacing={2}>
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={loadYamlContent}
                              isLoading={yamlLoading}
                            >
                              {t('common.refresh')}
                            </Button>
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={handleYamlValidate}
                              isDisabled={!yamlContent}
                            >
                              {t('configuration.validate')}
                            </Button>
                            <Button
                              size="sm"
                              colorScheme="blue"
                              variant="outline"
                              onClick={handleYamlPreview}
                              isDisabled={!yamlContent || yamlContent === originalYamlContent}
                            >
                              {t('configuration.previewChanges')}
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
                              <Text color="gray.500">{t('configuration.clickRefreshToLoad')}</Text>
                              <Button onClick={loadYamlContent}>{t('configuration.loadConfig')}</Button>
                            </VStack>
                          </Center>
                        )}
                      </VStack>
                    </ConfigCard>
                  </VStack>
                )}

                {tabIndex === 6 && (
                  <VStack spacing={6} align="stretch">
                    <ConfigCard title={t('configuration.configHistoryTitle')} icon={<RepeatIcon />}>
                      <Text fontSize="sm" color="gray.500" mb={4}>
                        {t('configuration.configHistoryDesc')}
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
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.profitSpread')}</FormLabel>
                          <NumberInput
                            value={(getSelectedSymbolConfig()?.profit_spread ?? config.trading?.profit_spread) ?? 0}
                            onChange={(_, v) => updateSelectedSymbolField('profit_spread', v === undefined ? undefined : (v || 0))}
                            precision={6}
                            step={0.01}
                            min={0}
                          >
                            <NumberInputField borderRadius="xl" placeholder={t('configuration.profitSpreadHint')} />
                          </NumberInput>
                          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.profitSpreadHint')}</Text>
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
                              {t('configuration.binanceMinOrderWarning')}
                            </Text>
                          )}
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.buyWindowSize')}</FormLabel>
                          <WindowSizeSlider
                            value={(getSelectedSymbolConfig()?.buy_window_size ?? config.trading?.buy_window_size) || 0}
                            onChange={(v) => updateSelectedSymbolField('buy_window_size', v)}
                          />
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.sellWindowSize')}</FormLabel>
                          <WindowSizeSlider
                            value={(getSelectedSymbolConfig()?.sell_window_size ?? config.trading?.sell_window_size) || 0}
                            onChange={(v) => updateSelectedSymbolField('sell_window_size', v)}
                          />
                        </FormControl>
                        <FormControl>
                          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.direction')}</FormLabel>
                          <Select
                            value={(getSelectedSymbolConfig()?.direction ?? config.trading?.direction) || 'LONG'}
                            onChange={(e) => {
                              const newDir = e.target.value
                              const curDir = (getSelectedSymbolConfig()?.direction ?? config.trading?.direction) || 'LONG'
                              if (newDir !== curDir) {
                                setDirectionConfirm({ isOpen: true, newDirection: newDir, loading: false })
                              } else {
                                updateSelectedSymbolField('direction', newDir)
                              }
                            }}
                            borderRadius="xl"
                          >
                            <option value="LONG">{t('configuration.directionLong')}</option>
                            <option value="SHORT">{t('configuration.directionShort')}</option>
                          </Select>
                          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.directionDesc')}</Text>
                        </FormControl>
                      </SimpleGrid>

                      {/* 实时价格范围 */}
                      <Box mt={4} p={4} bg="gray.50" borderRadius="lg" borderWidth="1px" borderColor="gray.200">
                        <Flex justify="space-between" align="center" mb={3}>
                          <HStack spacing={2}>
                            <Text fontSize="sm" fontWeight="bold">{t('configuration.priceRangeTitle')}</Text>
                            {priceRangeSource && (
                              <Badge colorScheme={priceRangeSource === 'runtime' ? 'green' : 'gray'} fontSize="2xs">
                                {priceRangeSource === 'runtime' ? t('configuration.priceRangeSourceRuntime') : t('configuration.priceRangeSourceConfig')}
                              </Badge>
                            )}
                            {hotUpdatedSymbols.length > 0 && (
                              <Badge colorScheme="blue" fontSize="2xs">{t('configuration.priceRangeHotUpdated')}</Badge>
                            )}
                          </HStack>
                          <Button
                            size="xs"
                            variant="ghost"
                            leftIcon={<RepeatIcon />}
                            onClick={fetchPriceRange}
                            isLoading={priceRangeLoading}
                          >
                            {t('configuration.priceRangeRefresh')}
                          </Button>
                        </Flex>
                        {priceRangeLoading && !priceRange ? (
                          <Center py={3}>
                            <Spinner size="sm" mr={2} />
                            <Text fontSize="xs" color="gray.500">{t('configuration.priceRangeLoading')}</Text>
                          </Center>
                        ) : priceRange && priceRange.current_price > 0 ? (
                          <VStack spacing={2} align="stretch">
                            <SimpleGrid columns={3} spacing={3}>
                              <Box textAlign="center">
                                <Text fontSize="2xs" color="gray.500">{t('configuration.priceRangeCurrentPrice')}</Text>
                                <Text fontSize="sm" fontWeight="bold" color="blue.600">
                                  {priceRange.current_price.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                                </Text>
                              </Box>
                              <Box textAlign="center">
                                <Text fontSize="2xs" color="gray.500">{t('configuration.priceRangeGridPrice')}</Text>
                                <Text fontSize="sm" fontWeight="bold">
                                  {(priceRange.grid_price ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                                </Text>
                              </Box>
                              <Box textAlign="center">
                                <Text fontSize="2xs" color="gray.500">{t('configuration.priceRangeAnchorPrice')}</Text>
                                <Text fontSize="sm" fontWeight="bold" color="gray.600">
                                  {priceRange.anchor_price.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                                </Text>
                              </Box>
                            </SimpleGrid>
                            <Divider />
                            <SimpleGrid columns={2} spacing={3}>
                              <Box p={2} bg="green.50" borderRadius="md">
                                <Text fontSize="2xs" color="green.600" fontWeight="bold">{t('configuration.priceRangeBuyRange')}</Text>
                                <Text fontSize="sm">
                                  {(priceRange.buy_price_low ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                                  {' ~ '}
                                  {(priceRange.buy_price_high ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                                </Text>
                              </Box>
                              <Box p={2} bg="red.50" borderRadius="md">
                                <Text fontSize="2xs" color="red.600" fontWeight="bold">{t('configuration.priceRangeSellRange')}</Text>
                                <Text fontSize="sm">
                                  {(priceRange.sell_price_low ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                                  {' ~ '}
                                  {(priceRange.sell_price_high ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                                </Text>
                              </Box>
                            </SimpleGrid>
                          </VStack>
                        ) : (
                          <Text fontSize="xs" color="gray.400" textAlign="center" py={2}>
                            {t('configuration.priceRangeNotRunning')}
                          </Text>
                        )}
                      </Box>

                      {/* 参数建议助手 */}
                      <ParamAdvisor
                        exchange={selectedExchange || ''}
                        symbol={selectedSymbol || ''}
                        currentPriceInterval={(getSelectedSymbolConfig()?.price_interval ?? config.trading?.price_interval) || undefined}
                        currentOrderQuantity={(getSelectedSymbolConfig()?.order_quantity ?? config.trading?.order_quantity) || undefined}
                        onApplyPriceInterval={(v) => updateSelectedSymbolField('price_interval', v)}
                        onApplyOrderQuantity={(v) => updateSelectedSymbolField('order_quantity', v)}
                        buyWindowSize={(getSelectedSymbolConfig()?.buy_window_size ?? config.trading?.buy_window_size) || 0}
                        leverage={config.risk_control?.max_leverage || undefined}
                        totalCapital={undefined}
                      />
                    </ConfigCard>

                    <ConfigCard title={t('configuration.profileSwitching')} icon={<SettingsIcon />}>
                      <VStack spacing={4} align="stretch">
                        <Alert status="info" borderRadius="md" size="sm">
                          <AlertIcon />
                          <AlertDescription fontSize="xs">
                            {t('configuration.profileSwitchingDesc')}
                          </AlertDescription>
                        </Alert>

                        {/* 配置档案 */}
                        <Box>
                          <Text fontSize="sm" fontWeight="600" mb={1}>{t('configuration.profiles')}</Text>
                          <Text fontSize="2xs" color="gray.500" mb={3}>{t('configuration.profileZeroMeansDefault')}</Text>
                          <VStack spacing={3} align="stretch">
                            {/* Positive Profile */}
                            <Box p={4} borderWidth="1px" borderRadius="lg" borderColor="gray.200">
                              <Text fontSize="sm" fontWeight="600" mb={2}>{t('configuration.profilePositive')}</Text>
                              <SimpleGrid columns={2} spacing={4}>
                                <FormControl>
                                  <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.priceInterval')}</FormLabel>
                                  <NumberInput
                                    value={getSelectedSymbolConfig()?.profiles?.positive?.price_interval ?? 0}
                                    onChange={(_, v) => {
                                      const symCfg = getSelectedSymbolConfig()
                                      const profiles = symCfg?.profiles || {}
                                      updateSelectedSymbolField('profiles', {
                                        ...profiles,
                                        positive: {
                                          ...profiles.positive,
                                          price_interval: v ?? 0,
                                          profit_spread: profiles.positive?.profit_spread,
                                          order_quantity: profiles.positive?.order_quantity ?? 0,
                                          buy_window_size: profiles.positive?.buy_window_size ?? 0,
                                          sell_window_size: profiles.positive?.sell_window_size ?? 0,
                                        }
                                      })
                                    }}
                                    precision={6}
                                    step={0.01}
                                  >
                                    <NumberInputField borderRadius="xl" size="sm" />
                                  </NumberInput>
                                </FormControl>
                                <FormControl>
                                  <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.profitSpread')}</FormLabel>
                                  <NumberInput
                                    value={getSelectedSymbolConfig()?.profiles?.positive?.profit_spread ?? 0}
                                    onChange={(_, v) => {
                                      const symCfg = getSelectedSymbolConfig()
                                      const profiles = symCfg?.profiles || {}
                                      updateSelectedSymbolField('profiles', {
                                        ...profiles,
                                        positive: {
                                          ...profiles.positive,
                                          price_interval: profiles.positive?.price_interval ?? 0,
                                          profit_spread: v === undefined || v === 0 ? undefined : v,
                                          order_quantity: profiles.positive?.order_quantity ?? 0,
                                          buy_window_size: profiles.positive?.buy_window_size ?? 0,
                                          sell_window_size: profiles.positive?.sell_window_size ?? 0,
                                        }
                                      })
                                    }}
                                    precision={6}
                                    step={0.01}
                                    min={0}
                                  >
                                    <NumberInputField borderRadius="xl" size="sm" placeholder={t('configuration.profitSpreadHint')} />
                                  </NumberInput>
                                  <Text fontSize="2xs" color="gray.500" mt={0.5}>{t('configuration.profitSpreadHint')}</Text>
                                </FormControl>
                                <FormControl>
                                  <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.orderAmount')}</FormLabel>
                                  <NumberInput
                                    value={getSelectedSymbolConfig()?.profiles?.positive?.order_quantity ?? 0}
                                    onChange={(_, v) => {
                                      const symCfg = getSelectedSymbolConfig()
                                      const profiles = symCfg?.profiles || {}
                                      updateSelectedSymbolField('profiles', {
                                        ...profiles,
                                        positive: {
                                          ...profiles.positive,
                                          price_interval: profiles.positive?.price_interval ?? 0,
                                          profit_spread: profiles.positive?.profit_spread,
                                          order_quantity: v ?? 0,
                                          buy_window_size: profiles.positive?.buy_window_size ?? 0,
                                          sell_window_size: profiles.positive?.sell_window_size ?? 0,
                                        }
                                      })
                                    }}
                                    precision={2}
                                  >
                                    <NumberInputField borderRadius="xl" size="sm" />
                                  </NumberInput>
                                </FormControl>
                                <FormControl>
                                  <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.buyWindowSize')}</FormLabel>
                                  <WindowSizeSlider
                                    value={getSelectedSymbolConfig()?.profiles?.positive?.buy_window_size ?? 0}
                                    onChange={(v) => {
                                      const symCfg = getSelectedSymbolConfig()
                                      const profiles = symCfg?.profiles || {}
                                      updateSelectedSymbolField('profiles', {
                                        ...profiles,
                                        positive: {
                                          ...profiles.positive,
                                          price_interval: profiles.positive?.price_interval ?? 0,
                                          profit_spread: profiles.positive?.profit_spread,
                                          order_quantity: profiles.positive?.order_quantity ?? 0,
                                          buy_window_size: v,
                                          sell_window_size: profiles.positive?.sell_window_size ?? 0,
                                        }
                                      })
                                    }}
                                    size="sm"
                                  />
                                </FormControl>
                                <FormControl>
                                  <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.sellWindowSize')}</FormLabel>
                                  <WindowSizeSlider
                                    value={getSelectedSymbolConfig()?.profiles?.positive?.sell_window_size ?? 0}
                                    onChange={(v) => {
                                      const symCfg = getSelectedSymbolConfig()
                                      const profiles = symCfg?.profiles || {}
                                      updateSelectedSymbolField('profiles', {
                                        ...profiles,
                                        positive: {
                                          ...profiles.positive,
                                          price_interval: profiles.positive?.price_interval ?? 0,
                                          profit_spread: profiles.positive?.profit_spread,
                                          order_quantity: profiles.positive?.order_quantity ?? 0,
                                          buy_window_size: profiles.positive?.buy_window_size ?? 0,
                                          sell_window_size: v,
                                        }
                                      })
                                    }}
                                    size="sm"
                                  />
                                </FormControl>
                              </SimpleGrid>
                            </Box>

                            {/* Negative Profile */}
                            <Box p={4} borderWidth="1px" borderRadius="lg" borderColor="gray.200">
                              <Text fontSize="sm" fontWeight="600" mb={2}>{t('configuration.profileNegative')}</Text>
                              <SimpleGrid columns={2} spacing={4}>
                                <FormControl>
                                  <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.priceInterval')}</FormLabel>
                                  <NumberInput
                                    value={getSelectedSymbolConfig()?.profiles?.negative?.price_interval ?? 0}
                                    onChange={(_, v) => {
                                      const symCfg = getSelectedSymbolConfig()
                                      const profiles = symCfg?.profiles || {}
                                      updateSelectedSymbolField('profiles', {
                                        ...profiles,
                                        negative: {
                                          ...profiles.negative,
                                          price_interval: v ?? 0,
                                          profit_spread: profiles.negative?.profit_spread,
                                          order_quantity: profiles.negative?.order_quantity ?? 0,
                                          buy_window_size: profiles.negative?.buy_window_size ?? 0,
                                          sell_window_size: profiles.negative?.sell_window_size ?? 0,
                                        }
                                      })
                                    }}
                                    precision={6}
                                    step={0.01}
                                  >
                                    <NumberInputField borderRadius="xl" size="sm" />
                                  </NumberInput>
                                </FormControl>
                                <FormControl>
                                  <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.profitSpread')}</FormLabel>
                                  <NumberInput
                                    value={getSelectedSymbolConfig()?.profiles?.negative?.profit_spread ?? 0}
                                    onChange={(_, v) => {
                                      const symCfg = getSelectedSymbolConfig()
                                      const profiles = symCfg?.profiles || {}
                                      updateSelectedSymbolField('profiles', {
                                        ...profiles,
                                        negative: {
                                          ...profiles.negative,
                                          price_interval: profiles.negative?.price_interval ?? 0,
                                          profit_spread: v === undefined || v === 0 ? undefined : v,
                                          order_quantity: profiles.negative?.order_quantity ?? 0,
                                          buy_window_size: profiles.negative?.buy_window_size ?? 0,
                                          sell_window_size: profiles.negative?.sell_window_size ?? 0,
                                        }
                                      })
                                    }}
                                    precision={6}
                                    step={0.01}
                                    min={0}
                                  >
                                    <NumberInputField borderRadius="xl" size="sm" placeholder={t('configuration.profitSpreadHint')} />
                                  </NumberInput>
                                  <Text fontSize="2xs" color="gray.500" mt={0.5}>{t('configuration.profitSpreadHint')}</Text>
                                </FormControl>
                                <FormControl>
                                  <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.orderAmount')}</FormLabel>
                                  <NumberInput
                                    value={getSelectedSymbolConfig()?.profiles?.negative?.order_quantity ?? 0}
                                    onChange={(_, v) => {
                                      const symCfg = getSelectedSymbolConfig()
                                      const profiles = symCfg?.profiles || {}
                                      updateSelectedSymbolField('profiles', {
                                        ...profiles,
                                        negative: {
                                          ...profiles.negative,
                                          price_interval: profiles.negative?.price_interval ?? 0,
                                          profit_spread: profiles.negative?.profit_spread,
                                          order_quantity: v ?? 0,
                                          buy_window_size: profiles.negative?.buy_window_size ?? 0,
                                          sell_window_size: profiles.negative?.sell_window_size ?? 0,
                                        }
                                      })
                                    }}
                                    precision={2}
                                  >
                                    <NumberInputField borderRadius="xl" size="sm" />
                                  </NumberInput>
                                </FormControl>
                                <FormControl>
                                  <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.buyWindowSize')}</FormLabel>
                                  <WindowSizeSlider
                                    value={getSelectedSymbolConfig()?.profiles?.negative?.buy_window_size ?? 0}
                                    onChange={(v) => {
                                      const symCfg = getSelectedSymbolConfig()
                                      const profiles = symCfg?.profiles || {}
                                      updateSelectedSymbolField('profiles', {
                                        ...profiles,
                                        negative: {
                                          ...profiles.negative,
                                          price_interval: profiles.negative?.price_interval ?? 0,
                                          profit_spread: profiles.negative?.profit_spread,
                                          order_quantity: profiles.negative?.order_quantity ?? 0,
                                          buy_window_size: v,
                                          sell_window_size: profiles.negative?.sell_window_size ?? 0,
                                        }
                                      })
                                    }}
                                    size="sm"
                                  />
                                </FormControl>
                                <FormControl>
                                  <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.sellWindowSize')}</FormLabel>
                                  <WindowSizeSlider
                                    value={getSelectedSymbolConfig()?.profiles?.negative?.sell_window_size ?? 0}
                                    onChange={(v) => {
                                      const symCfg = getSelectedSymbolConfig()
                                      const profiles = symCfg?.profiles || {}
                                      updateSelectedSymbolField('profiles', {
                                        ...profiles,
                                        negative: {
                                          ...profiles.negative,
                                          price_interval: profiles.negative?.price_interval ?? 0,
                                          profit_spread: profiles.negative?.profit_spread,
                                          order_quantity: profiles.negative?.order_quantity ?? 0,
                                          buy_window_size: profiles.negative?.buy_window_size ?? 0,
                                          sell_window_size: v,
                                        }
                                      })
                                    }}
                                    size="sm"
                                  />
                                </FormControl>
                              </SimpleGrid>
                            </Box>
                          </VStack>
                        </Box>

                        <Divider />

                        {/* 切换规则 */}
                        <Box>
                          <Text fontSize="sm" fontWeight="600" mb={3}>{t('configuration.switchRules')}</Text>
                          <SimpleGrid columns={2} spacing={4}>
                            <FormControl>
                              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.fundingRateThreshold')}</FormLabel>
                              <NumberInput
                                value={(getSelectedSymbolConfig()?.switch_rules?.funding_rate?.threshold ?? 0) * 100}
                                onChange={(_, v) => {
                                  const symCfg = getSelectedSymbolConfig()
                                  const rules = symCfg?.switch_rules || { funding_rate: {}, fee_rate: {} }
                                  updateSelectedSymbolField('switch_rules', {
                                    ...rules,
                                    funding_rate: {
                                      threshold: (v ?? 0) / 100
                                    },
                                    fee_rate: rules.fee_rate || {},
                                    cooldown_seconds: rules.cooldown_seconds || 300
                                  })
                                }}
                                precision={4}
                                step={0.01}
                              >
                                <NumberInputField borderRadius="xl" size="sm" />
                              </NumberInput>
                              <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.fundingRateThresholdDesc')}</Text>
                            </FormControl>
                            <FormControl>
                              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.feeRateThreshold')}</FormLabel>
                              <NumberInput
                                value={(getSelectedSymbolConfig()?.switch_rules?.fee_rate?.threshold ?? 0) * 100}
                                onChange={(_, v) => {
                                  const symCfg = getSelectedSymbolConfig()
                                  const rules = symCfg?.switch_rules || { funding_rate: {}, fee_rate: {} }
                                  updateSelectedSymbolField('switch_rules', {
                                    ...rules,
                                    funding_rate: rules.funding_rate || {},
                                    fee_rate: {
                                      threshold: (v ?? 0) / 100
                                    },
                                    cooldown_seconds: rules.cooldown_seconds || 300
                                  })
                                }}
                                precision={4}
                                step={0.01}
                              >
                                <NumberInputField borderRadius="xl" size="sm" />
                              </NumberInput>
                              <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.feeRateThresholdDesc')}</Text>
                            </FormControl>
                            <FormControl>
                              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.cooldownSeconds')}</FormLabel>
                              <NumberInput
                                value={getSelectedSymbolConfig()?.switch_rules?.cooldown_seconds ?? 300}
                                onChange={(_, v) => {
                                  const symCfg = getSelectedSymbolConfig()
                                  const rules = symCfg?.switch_rules || { funding_rate: {}, fee_rate: {} }
                                  updateSelectedSymbolField('switch_rules', {
                                    ...rules,
                                    funding_rate: rules.funding_rate || {},
                                    fee_rate: rules.fee_rate || {},
                                    cooldown_seconds: v ?? 300
                                  })
                                }}
                                min={60}
                                max={3600}
                              >
                                <NumberInputField borderRadius="xl" size="sm" />
                              </NumberInput>
                              <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.cooldownSecondsDesc')}</Text>
                            </FormControl>
                          </SimpleGrid>
                        </Box>
                      </VStack>
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
                        <NumberInput value={config.risk_control?.volume_multiplier || 0} onChange={(_, v) => updateConfigField('risk_control.volume_multiplier', v)} precision={2} step={0.1}>
                          <NumberInputField borderRadius="xl" />
                        </NumberInput>
                      </FormControl>
                    </SimpleGrid>
                  </ConfigCard>

                  <ConfigCard title={t('configuration.newsMonitorTitle')} icon={<BellIcon />}>
                    <Flex justify="space-between" align="center" mb={6}>
                      <Box>
                        <Text fontWeight="600">{t('configuration.enableNewsMonitor')}</Text>
                        <Text fontSize="xs" color="gray.500">{t('configuration.newsMonitorDesc')}</Text>
                      </Box>
                      <Switch
                        colorScheme="blue"
                        isChecked={config.news_monitor?.enabled || false}
                        onChange={(e) => updateConfigField('news_monitor.enabled', e.target.checked)}
                      />
                    </Flex>
                    <FormControl mb={4}>
                      <FormLabel fontSize="xs" fontWeight="bold">NewsAPI Key</FormLabel>
                      {renderPasswordInput('news_monitor.news_api_key', t('configuration.forCollectingNews'))}
                    </FormControl>
                    <FormControl mb={4}>
                      <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.geminiSearch')}</FormLabel>
                      <Switch
                        isChecked={config.news_monitor?.use_gemini_search !== false}
                        onChange={(e) => updateConfigField('news_monitor.use_gemini_search', e.target.checked)}
                      />
                      <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.geminiRealtimeSearchDesc')}</Text>
                    </FormControl>
                    <SimpleGrid columns={2} spacing={6}>
                      <FormControl>
                        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.analysisInterval')}</FormLabel>
                        <Select
                          value={config.news_monitor?.analysis_interval || '30m'}
                          onChange={(e) => updateConfigField('news_monitor.analysis_interval', e.target.value)}
                          borderRadius="xl"
                        >
                          <option value="15m">{t('configuration.interval15m')}</option>
                          <option value="30m">{t('configuration.interval30m')}</option>
                          <option value="60m">{t('configuration.interval60m')}</option>
                          <option value="2h">{t('configuration.interval2h')}</option>
                          <option value="4h">{t('configuration.interval4h')}</option>
                          <option value="8h">{t('configuration.interval8h')}</option>
                          <option value="24h">{t('configuration.interval24h')}</option>
                        </Select>
                      </FormControl>
                      <FormControl>
                        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.pauseTradingThreshold')}</FormLabel>
                        <NumberInput
                          value={(config.news_monitor?.risk_thresholds?.stop_trading_probability ?? 0.7) * 100}
                          onChange={(_, v) => updateConfigField('news_monitor.risk_thresholds.stop_trading_probability', (v || 70) / 100)}
                          min={50}
                          max={100}
                        >
                          <NumberInputField borderRadius="xl" />
                        </NumberInput>
                        <Text fontSize="xs" color="gray.500">{t('configuration.pauseTradingThresholdDesc')}</Text>
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

        {/* 方向切换确认 */}
        <ConfirmDialog
          isOpen={directionConfirm.isOpen}
          onClose={() => setDirectionConfirm((p) => ({ ...p, isOpen: false }))}
          onConfirm={async () => {
            if (!config || !selectedExchange || !selectedSymbol) return
            setDirectionConfirm((p) => ({ ...p, loading: true }))
            const newDir = directionConfirm.newDirection
            const exchange = selectedExchange
            const symbol = selectedSymbol
            try {
              const pend = await getPendingOrders(exchange, symbol).catch(() => ({ orders: [] }))
              const orderIds = (pend.orders || []).map((o: any) => o.order_id).filter(Boolean)
              if (orderIds.length > 0) {
                await batchCancelOrders(orderIds, exchange, symbol)
              }
              await closeAllPositions(exchange, symbol)
              await stopTrading(exchange, symbol)
              const newConfig: Config = JSON.parse(JSON.stringify(config))
              const syms = newConfig.trading?.symbols || []
              const idx = syms.findIndex(
                (s: any) =>
                  (s.exchange || config.app?.current_exchange || '') === exchange &&
                  (s.symbol || '').toLowerCase() === symbol.toLowerCase()
              )
              if (idx >= 0) {
                syms[idx] = { ...syms[idx], direction: newDir }
              } else if (newConfig.trading) {
                newConfig.trading.direction = newDir
              }
              await updateConfig(newConfig)
              setConfig(newConfig)
              await startTrading(exchange, symbol)
              setDirectionConfirm((p) => ({ ...p, isOpen: false, loading: false }))
              toast({ title: t('configuration.directionSwitchSuccess'), status: 'success', duration: 3000 })
            } catch (err) {
              setDirectionConfirm((p) => ({ ...p, loading: false }))
              toast({ title: t('configuration.directionSwitchFailed'), description: err instanceof Error ? err.message : String(err), status: 'error', duration: 5000 })
              throw err
            }
          }}
          title={t('configuration.directionSwitchConfirmTitle')}
          message={t('configuration.directionSwitchConfirmMessage', {


          })}
          confirmText={t('common.confirm')}
          confirmColorScheme="orange"
          isLoading={directionConfirm.loading}
        />

        {/* YAML Diff Preview Modal */}
        <DiffPreviewModal
          isOpen={isDiffOpen}
          onClose={onDiffClose}
          onConfirm={handleYamlSave}
          oldValue={originalYamlContent}
          newValue={yamlContent}
          oldTitle={t('configuration.currentConfig')}
          newTitle={t('configuration.modifiedConfig')}
          title={t('configuration.configChangePreview')}
          confirmText={t('configuration.confirmSave')}
          isLoading={yamlSaving}
        />
      </VStack>
    </Container>
  )
}

export default Configuration
