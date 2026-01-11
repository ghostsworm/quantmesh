import React, { useState } from 'react'
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
  NumberInput,
  NumberInputField,
  Checkbox,
  useToast,
  Spinner,
  Center,
  Divider,
} from '@chakra-ui/react'
import { StarIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { saveInitialConfig, SetupInitRequest } from '../services/setup'
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
    testnet: true, // 默认使用测试网
    fee_rate: 0.0002,
  })

  const [useAIConfig, setUseAIConfig] = useState(false)
  const [aiWizardOpen, setAIWizardOpen] = useState(false)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const exchangesRequiringPassphrase = ['bitget', 'okx', 'kucoin']

  const handleExchangeConfigChange = (field: keyof SetupInitRequest, value: any) => {
    setExchangeConfig(prev => {
      const updated = { ...prev, [field]: value }
      // 当交易所改变时，清空已选的交易对
      if (field === 'exchange') {
        updated.symbols = []
      }
      return updated
    })
  }

  const handleSymbolsChange = (symbols: string[]) => {
    setExchangeConfig(prev => ({ ...prev, symbols }))
  }

  const handleNext = async () => {
    if (activeStep === 0) {
      // 欢迎页面，直接下一步
      setActiveStep(1)
    } else if (activeStep === 1) {
      // 验证交易所配置
      if (!exchangeConfig.exchange) {
        setError(t('wizard.exchange.selectExchange'))
        return
      }
      if (!exchangeConfig.api_key.trim()) {
        setError(t('wizard.exchange.enterApiKey'))
        return
      }
      if (!exchangeConfig.secret_key.trim()) {
        setError(t('wizard.exchange.enterSecretKey'))
        return
      }
      if (exchangesRequiringPassphrase.includes(exchangeConfig.exchange) && !exchangeConfig.passphrase?.trim()) {
        setError(t('wizard.exchange.enterPassphrase'))
        return
      }
      const symbols = exchangeConfig.symbols || []
      if (symbols.length === 0) {
        setError(t('wizard.exchange.selectSymbols'))
        return
      }

      // 保存交易所配置
      setIsLoading(true)
      setError(null)

      try {
        // 确保发送 symbols 数组
        const configToSave = {
          ...exchangeConfig,
          symbols: exchangeConfig.symbols || [],
        }
        const response = await saveInitialConfig(configToSave)
        if (response.success) {
          // 如果有备份路径，显示备份信息
          if (response.backup_path) {
            toast({
              title: t('wizard.exchange.configSaved'),
              description: t('wizard.exchange.backupCreated', { path: response.backup_path }) as string,
              status: 'success',
              duration: 10000,
              isClosable: true,
            })
          } else {
            toast({
              title: t('wizard.exchange.configSaved'),
              status: 'success',
              duration: 3000,
            })
          }
          setActiveStep(2)
        } else {
          setError(response.message || t('wizard.exchange.saveFailed'))
        }
      } catch (err: any) {
        setError(err.message || t('wizard.exchange.saveFailed'))
      } finally {
        setIsLoading(false)
      }
    } else if (activeStep === 2) {
      // AI 配置步骤
      if (useAIConfig) {
        setAIWizardOpen(true)
      } else {
        setActiveStep(3)
      }
    } else if (activeStep === 3) {
      // 完成，清除向导标记，跳转到主页
      sessionStorage.removeItem('wizard_step')
      navigate('/')
    }
  }

  const handleSkip = () => {
    if (activeStep === 2) {
      // 跳过 AI 配置
      setActiveStep(3)
    } else {
      // 其他步骤的跳过逻辑，清除向导标记
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

  return (
    <Box minH="100vh" bg="gray.50" py={8}>
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
            {/* 语言选择器 - 右上角 */}
            <Box position="absolute" top={4} right={4}>
              <LanguageSelector />
            </Box>
            
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
              <VStack spacing={4} align="stretch">
                <Heading size="md">{t('wizard.exchange.title')}</Heading>
                {error && (
                  <Alert status="error">
                    <AlertIcon />
                    <AlertDescription>{error}</AlertDescription>
                  </Alert>
                )}

                {/* 1. 先选择是否使用测试网 */}
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

                {/* 2. 选择交易所 */}
                <FormControl isRequired>
                  <FormLabel>{t('wizard.exchange.exchange')}</FormLabel>
                  <Select
                    value={exchangeConfig.exchange}
                    onChange={(e) => handleExchangeConfigChange('exchange', e.target.value)}
                    isDisabled={isLoading}
                  >
                    <option value="binance">Binance</option>
                    <option value="bitget">Bitget</option>
                    <option value="bybit">Bybit</option>
                    <option value="gate">Gate.io</option>
                    <option value="okx">OKX</option>
                    <option value="huobi">Huobi (HTX)</option>
                    <option value="kucoin">KuCoin</option>
                  </Select>
                </FormControl>

                <Divider />

                {/* 3. 输入 API Key 和 Secret Key */}
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

                {/* 4. 选择交易对（只有在输入了 API Key 和 Secret Key 后才显示） */}
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

        {/* AI Config Wizard Modal */}
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
