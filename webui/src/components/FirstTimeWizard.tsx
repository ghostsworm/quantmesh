import React, { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Box,
  Container,
  Heading,
  Button,
  VStack,
  HStack,
  Text,
  Stepper,
  Step,
  StepIndicator,
  StepStatus,
  StepIcon,
  StepNumber,
  StepTitle,
  StepDescription,
  StepSeparator,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  FormControl,
  FormLabel,
  Input,
  Select,
  Checkbox,
  useToast,
  Spinner,
  Center,
  Divider,
  Tag,
  TagLabel,
  Wrap,
  WrapItem,
} from '@chakra-ui/react'
import { StarIcon, AddIcon, CheckIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { saveInitialConfig, SetupInitRequest, checkSetupStatus } from '../services/setup'
import AIConfigWizard from './AIConfigWizard'
import SymbolMultiSelect from './SymbolMultiSelect'
import LanguageSelector from './LanguageSelector'

const FirstTimeWizard: React.FC = () => {
  const { t } = useTranslation()
  
  const steps = [
    { title: t('wizard.step.welcome.title'), description: t('wizard.step.welcome.description') },
    { title: t('wizard.step.exchange.title'), description: t('wizard.step.exchange.description') },
    { title: t('wizard.step.ai.title'), description: t('wizard.step.ai.description') },
    { title: t('wizard.step.complete.title'), description: t('wizard.step.complete.description') },
  ]

  const navigate = useNavigate()
  const toast = useToast()
  const [activeStep, setActiveStep] = useState(0)

  const [exchangeConfig, setExchangeConfig] = useState<SetupInitRequest>({
    exchange: 'binance',
    api_key: '',
    secret_key: '',
    passphrase: '',
    symbols: [],
    price_interval: 2,
    order_quantity: 30,
    min_order_value: 20,
    buy_window_size: 10,
    sell_window_size: 10,
    testnet: true,
    fee_rate: 0.0002,
  })

  const [configuredExchanges, setConfiguredExchanges] = useState<string[]>([])
  const [useAIConfig, setUseAIConfig] = useState(false)
  const [aiWizardOpen, setAIWizardOpen] = useState(false)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [statusData, setStatusData] = useState<any>(null)

  useEffect(() => {
    const fetchStatus = async () => {
      try {
        const status = await checkSetupStatus()
        setStatusData(status)
        if (status.exchanges && Object.keys(status.exchanges).length > 0) {
          const exchangeNames = Object.keys(status.exchanges)
          setConfiguredExchanges(exchangeNames)
          
          // 默认选中第一个交易所的信息进行回填
          const firstExchange = exchangeNames[0]
          handleEditExchange(firstExchange, status)
        }
      } catch (err) {
        console.error('获取配置状态失败:', err)
      }
    }
    fetchStatus()
  }, [])

  const handleEditExchange = (exchangeName: string, status = statusData) => {
    if (!status || !status.exchanges || !status.exchanges[exchangeName]) return

    const config = status.exchanges[exchangeName]
    const exchangeSymbols = (status.symbols || [])
      .filter((s: any) => s.exchange === exchangeName)
      .map((s: any) => s.symbol)
      
    setExchangeConfig({
      exchange: exchangeName,
      api_key: config.api_key,
      secret_key: config.secret_key,
      passphrase: config.passphrase || '',
      testnet: config.testnet ?? true,
      fee_rate: config.fee_rate || 0.0002,
      symbols: exchangeSymbols,
      price_interval: status.symbols?.find((s: any) => s.exchange === exchangeName)?.price_interval || 2,
      order_quantity: status.symbols?.find((s: any) => s.exchange === exchangeName)?.order_quantity || 30,
      buy_window_size: status.symbols?.find((s: any) => s.exchange === exchangeName)?.buy_window_size || 10,
      min_order_value: status.symbols?.find((s: any) => s.exchange === exchangeName)?.min_order_value || 20,
    })
    setError(null)
  }

  const exchangesRequiringPassphrase = ['bitget', 'okx', 'kucoin']

  const handleExchangeConfigChange = (field: keyof SetupInitRequest, value: any) => {
    setExchangeConfig(prev => {
      const updated = { ...prev, [field]: value }
      if (field === 'exchange') {
        updated.symbols = []
      }
      return updated
    })
  }

  const handleSymbolsChange = (symbols: string[]) => {
    setExchangeConfig(prev => ({ ...prev, symbols }))
  }

  const handleSaveCurrentExchange = async () => {
    if (!exchangeConfig.exchange) {
      setError(t('wizard.exchange.selectExchange'))
      return false
    }
    if (!exchangeConfig.api_key.trim()) {
      setError(t('wizard.exchange.enterApiKey'))
      return false
    }
    if (!exchangeConfig.secret_key.trim()) {
      setError(t('wizard.exchange.enterSecretKey'))
      return false
    }
    if (exchangesRequiringPassphrase.includes(exchangeConfig.exchange) && !exchangeConfig.passphrase?.trim()) {
      setError(t('wizard.exchange.enterPassphrase'))
      return false
    }
    const symbols = exchangeConfig.symbols || []
    if (symbols.length === 0) {
      setError(t('wizard.exchange.selectSymbols'))
      return false
    }

    setIsLoading(true)
    setError(null)

    try {
      const configToSave = {
        ...exchangeConfig,
        symbols: exchangeConfig.symbols || [],
      }
      const response = await saveInitialConfig(configToSave)
      if (response.success) {
        toast({
          title: t('wizard.exchange.configSaved'),
          status: 'success',
          duration: 3000,
        })
        
        if (!configuredExchanges.includes(exchangeConfig.exchange)) {
          setConfiguredExchanges(prev => [...prev, exchangeConfig.exchange])
        }
        return true
      } else {
        setError(response.message || t('wizard.exchange.saveFailed'))
        return false
      }
    } catch (err: any) {
      setError(err.message || t('wizard.exchange.saveFailed'))
      return false
    } finally {
      setIsLoading(false)
    }
  }

  const handleNext = async () => {
    if (activeStep === 0) {
      setActiveStep(1)
    } else if (activeStep === 1) {
      if (configuredExchanges.length === 0) {
        const success = await handleSaveCurrentExchange()
        if (!success) return
      }
      setActiveStep(2)
    } else if (activeStep === 2) {
      if (useAIConfig) {
        setAIWizardOpen(true)
      } else {
        setActiveStep(3)
      }
    } else if (activeStep === 3) {
      sessionStorage.removeItem('wizard_step')
      sessionStorage.removeItem('config_setup_skipped')
      setTimeout(() => {
        navigate('/')
      }, 100)
    }
  }

  const handleSkip = () => {
    if (activeStep === 2) {
      setActiveStep(3)
    } else {
      sessionStorage.removeItem('wizard_step')
      navigate('/')
    }
  }

  const handleAIConfigSuccess = () => {
    setAIWizardOpen(false)
    setActiveStep(3)
    toast({
      title: t('wizard.ai.configApplied'),
      description: t('wizard.ai.configSaved'),
      status: 'success',
      duration: 3000,
    })
  }

  const handleAddNewExchange = () => {
    setExchangeConfig({
      exchange: 'binance',
      api_key: '',
      secret_key: '',
      passphrase: '',
      symbols: [],
      price_interval: 2,
      order_quantity: 30, // 确保有默认值
      min_order_value: 20,
      buy_window_size: 10,
      sell_window_size: 10,
      testnet: true,
      fee_rate: 0.0002,
    })
    setError(null)
    toast({
      title: t('wizard.exchange.readyForNew'),
      status: 'info',
      duration: 2000,
    })
  }

  return (
    <Box minH="100vh" bg="gray.50" py={8} position="relative">
      {/* 语言选择器 - 页面最右上角 */}
      <Box position="absolute" top={4} right={8} zIndex={10}>
        <LanguageSelector />
      </Box>

      <Container maxW="4xl">
        <VStack spacing={8}>
          <Heading size="lg" textAlign="center">
            {t('wizard.title')}
          </Heading>

          <Stepper index={activeStep} colorScheme="blue" size="lg" w="100%">
            {steps.map((step, index) => (
              <Step key={index}>
                <StepIndicator>
                  <StepStatus
                    complete={<StepIcon />}
                    incomplete={<StepNumber />}
                    active={<StepNumber />}
                  />
                </StepIndicator>
                <Box flexShrink="0">
                  <StepTitle>{step.title}</StepTitle>
                  <StepDescription>{step.description}</StepDescription>
                </Box>
                <StepSeparator />
              </Step>
            ))}
          </Stepper>

          <Box w="100%" bg="white" p={8} borderRadius="lg" boxShadow="md" position="relative">
            {activeStep === 0 && (
              <VStack spacing={6}>
                <Heading size="md" textAlign="center">
                  {t('wizard.welcome.title')}
                </Heading>
                <Text textAlign="center" color="gray.600">
                  {t('wizard.welcome.description')}
                </Text>
                <VStack spacing={4} align="stretch" mt={4}>
                  <Text fontWeight="bold">{t('wizard.welcome.steps')}</Text>
                  <Text>1. {t('wizard.welcome.step1')}</Text>
                  <Text>2. {t('wizard.welcome.step2')}</Text>
                  <Text>3. {t('wizard.welcome.step3')}</Text>
                </VStack>
              </VStack>
            )}

            {activeStep === 1 && (
              <VStack spacing={6} align="stretch">
                <HStack justify="space-between">
                  <Heading size="md">{t('wizard.exchange.title')}</Heading>
                  <Button 
                    leftIcon={<AddIcon />} 
                    size="sm" 
                    variant="outline" 
                    colorScheme="blue"
                    onClick={handleAddNewExchange}
                  >
                    {t('wizard.exchange.addAnother')}
                  </Button>
                </HStack>

                {configuredExchanges.length > 0 && (
                  <Box p={4} bg="blue.50" borderRadius="md">
                    <Text fontSize="sm" fontWeight="bold" mb={2}>{t('wizard.exchange.configuredExchanges')}</Text>
                    <Wrap spacing={2}>
                      {configuredExchanges.map(ex => (
                        <WrapItem key={ex}>
                          <Tag 
                            size="lg" 
                            colorScheme="blue" 
                            borderRadius="full" 
                            cursor="pointer"
                            onClick={() => handleEditExchange(ex)}
                            _hover={{ bg: 'blue.100' }}
                          >
                            <TagLabel>{ex.toUpperCase()}</TagLabel>
                          </Tag>
                        </WrapItem>
                      ))}
                    </Wrap>
                  </Box>
                )}

                {error && (
                  <Alert status="error">
                    <AlertIcon />
                    <AlertDescription>{error}</AlertDescription>
                  </Alert>
                )}

                <FormControl>
                  <Checkbox
                    isChecked={exchangeConfig.testnet}
                    onChange={(e) => handleExchangeConfigChange('testnet', e.target.checked)}
                    isDisabled={isLoading}
                    size="lg"
                  >
                    <Text fontWeight="bold">{t('wizard.exchange.testnet')}</Text>
                  </Checkbox>
                  {exchangeConfig.testnet ? (
                    <Alert status="info" mt={2} borderRadius="md">
                      <AlertIcon />
                      <AlertDescription fontSize="sm">
                        {t('wizard.exchange.testnetInfo')}
                      </AlertDescription>
                    </Alert>
                  ) : (
                    <Alert status="warning" mt={2} bg="red.50" borderColor="red.200">
                      <AlertIcon color="red.500" />
                      <AlertDescription color="red.700" fontSize="sm">
                        {t('wizard.exchange.testnetWarning')}
                      </AlertDescription>
                    </Alert>
                  )}
                </FormControl>

                <Divider />

                <FormControl isRequired>
                  <FormLabel>{t('wizard.exchange.exchange')}</FormLabel>
                  <Select
                    value={exchangeConfig.exchange}
                    onChange={(e) => handleExchangeConfigChange('exchange', e.target.value)}
                    isDisabled={isLoading}
                  >
                    <option value="binance" disabled={configuredExchanges.includes('binance')}>Binance</option>
                    <option value="bitget" disabled={configuredExchanges.includes('bitget')}>Bitget</option>
                    <option value="bybit" disabled={configuredExchanges.includes('bybit')}>Bybit</option>
                    <option value="gate" disabled={configuredExchanges.includes('gate')}>Gate.io</option>
                    <option value="okx" disabled={configuredExchanges.includes('okx')}>OKX</option>
                    <option value="huobi" disabled={configuredExchanges.includes('huobi')}>Huobi (HTX)</option>
                    <option value="kucoin" disabled={configuredExchanges.includes('kucoin')}>KuCoin</option>
                  </Select>
                </FormControl>

                <Divider />

                <FormControl isRequired>
                  <FormLabel>{t('wizard.exchange.apiKey')}</FormLabel>
                  <Input
                    type="text"
                    value={exchangeConfig.api_key}
                    onChange={(e) => handleExchangeConfigChange('api_key', e.target.value)}
                    placeholder={t('wizard.exchange.apiKeyPlaceholder')}
                    isDisabled={isLoading}
                  />
                </FormControl>

                <FormControl isRequired>
                  <FormLabel>{t('wizard.exchange.secretKey')}</FormLabel>
                  <Input
                    type="password"
                    value={exchangeConfig.secret_key}
                    onChange={(e) => handleExchangeConfigChange('secret_key', e.target.value)}
                    placeholder={t('wizard.exchange.secretKeyPlaceholder')}
                    isDisabled={isLoading}
                  />
                </FormControl>

                {exchangesRequiringPassphrase.includes(exchangeConfig.exchange) && (
                  <FormControl isRequired>
                    <FormLabel>{t('wizard.exchange.passphrase')}</FormLabel>
                    <Input
                      type="password"
                      value={exchangeConfig.passphrase || ''}
                      onChange={(e) => handleExchangeConfigChange('passphrase', e.target.value)}
                      placeholder={t('wizard.exchange.passphrasePlaceholder')}
                      isDisabled={isLoading}
                    />
                  </FormControl>
                )}

                <Divider />

                {exchangeConfig.api_key && exchangeConfig.secret_key ? (
                  <FormControl isRequired>
                    <FormLabel>{t('wizard.exchange.symbols')}</FormLabel>
                    <SymbolMultiSelect
                      exchange={exchangeConfig.exchange}
                      selectedSymbols={exchangeConfig.symbols || []}
                      onChange={handleSymbolsChange}
                      isDisabled={isLoading}
                      apiKey={exchangeConfig.api_key}
                      secretKey={exchangeConfig.secret_key}
                      passphrase={exchangeConfig.passphrase}
                      testnet={exchangeConfig.testnet}
                    />
                  </FormControl>
                ) : (
                  <FormControl>
                    <FormLabel>{t('wizard.exchange.symbols')}</FormLabel>
                    <Alert status="info" borderRadius="md">
                      <AlertIcon />
                      <AlertDescription fontSize="sm">
                        {t('wizard.exchange.enterCredentialsToLoadSymbols')}
                      </AlertDescription>
                    </Alert>
                  </FormControl>
                )}

                <Button 
                  colorScheme="blue" 
                  leftIcon={<CheckIcon />} 
                  onClick={handleSaveCurrentExchange}
                  isLoading={isLoading}
                  variant="solid"
                  size="lg"
                  w="100%"
                >
                  {t('wizard.exchange.saveCurrent')}
                </Button>
              </VStack>
            )}

            {activeStep === 2 && (
              <VStack spacing={6}>
                <Heading size="md">{t('wizard.ai.title')}</Heading>
                <Text color="gray.600" textAlign="center">
                  {t('wizard.ai.description')}
                </Text>

                <Alert status="info">
                  <AlertIcon />
                  <Box>
                    <AlertTitle>{t('wizard.ai.alertTitle')}</AlertTitle>
                    <AlertDescription>
                      {t('wizard.ai.alertDescription')}
                    </AlertDescription>
                  </Box>
                </Alert>

                <FormControl>
                  <Checkbox
                    isChecked={useAIConfig}
                    onChange={(e) => setUseAIConfig(e.target.checked)}
                  >
                    {t('wizard.ai.useAI')}
                  </Checkbox>
                </FormControl>

                {useAIConfig && (
                  <Button
                    leftIcon={<StarIcon />}
                    colorScheme="purple"
                    onClick={() => setAIWizardOpen(true)}
                    w="100%"
                  >
                    {t('wizard.ai.openAssistant')}
                  </Button>
                )}
              </VStack>
            )}

            {activeStep === 3 && (
              <VStack spacing={6}>
                <Heading size="md" color="green.500">
                  {t('wizard.complete.title')}
                </Heading>
                <Text textAlign="center" color="gray.600">
                  {t('wizard.complete.description')}
                </Text>
                <Alert status="success">
                  <AlertIcon />
                  <Box>
                    <AlertTitle>{t('wizard.complete.nextStep')}</AlertTitle>
                    <AlertDescription>
                      {t('wizard.complete.nextStepDescription')}
                    </AlertDescription>
                  </Box>
                </Alert>
              </VStack>
            )}

            {isLoading && (
              <Center py={8}>
                <Spinner size="lg" />
              </Center>
            )}
          </Box>

          <HStack spacing={4} w="100%" justify="flex-end">
            {activeStep > 0 && activeStep < 3 && (
              <Button variant="ghost" onClick={() => setActiveStep(activeStep - 1)}>
                {t('wizard.button.previous')}
              </Button>
            )}
            {activeStep === 2 && (
              <Button variant="outline" onClick={handleSkip}>
                {t('wizard.button.skip')}
              </Button>
            )}
            <Button
              colorScheme="blue"
              onClick={handleNext}
              isDisabled={isLoading}
            >
              {activeStep === 3 ? t('wizard.button.complete') : t('wizard.button.next')}
            </Button>
          </HStack>
        </VStack>

        <AIConfigWizard
          isOpen={aiWizardOpen}
          onClose={() => setAIWizardOpen(false)}
          onSuccess={handleAIConfigSuccess}
          exchange={exchangeConfig.exchange}
          symbols={exchangeConfig.symbols || []}
        />
      </Container>
    </Box>
  )
}

export default FirstTimeWizard
