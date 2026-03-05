import React, { useState, useCallback } from 'react'
import {
  Box,
  Button,
  Text,
  FormControl,
  FormLabel,
  Input,
  Switch,
  Badge,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Flex,
  Grid,
  SimpleGrid,
  Divider,
  IconButton,
  Collapse,
  useDisclosure,
  Progress,
  Heading,
  Card,
  CardBody,
  Stack,
  HStack,
  VStack,
  Icon,
  Tooltip,
  Spinner,
} from '@chakra-ui/react'
import {
  FiTrendingUp,
  FiTrendingDown,
  FiActivity,
  FiClock,
  FiShield,
  FiZap,
  FiServer,
  FiAlertTriangle,
  FiCheckCircle,
  FiInfo,
  FiChevronDown,
  FiChevronUp,
  FiRefreshCw,
  FiSave,
  FiPlay,
} from 'react-icons/fi'
import { useTranslation } from 'react-i18next'

// 策略類型定義
interface StrategyConfig {
  type: string
  name: string
  direction: 'LONG' | 'SHORT' | 'BOTH'
  parameters: Record<string, any>
}

// 风險評估結果
interface RiskAssessment {
  overallScore: number
  riskLevel: 'low' | 'medium' | 'high' | 'extreme'
  scoreBreakdown: {
    capitalManagement: number
    riskControl: number
    strategyFit: number
    marketCondition: number
  }
  warnings: string[]
  suggestions: Array<{
    title: string
    description: string
    priority: 'high' | 'medium' | 'low'
  }>
  recommended: boolean
}

// 預設模板
const STRATEGY_TEMPLATES = {
  conservative: {
    icon: FiShield,
    color: 'green',
    defaults: {
      maxLayers: 10,
      stopLoss: 5,
      takeProfit: 2,
      leverage: 3,
      priceStep: 2,
      multiplier: 1.2,
      trendFilter: true,
      cascadeProtection: true,
    },
  },
  balanced: {
    icon: FiActivity,
    color: 'yellow',
    defaults: {
      maxLayers: 20,
      stopLoss: 10,
      takeProfit: 3,
      leverage: 5,
      priceStep: 1.5,
      multiplier: 1.5,
      trendFilter: true,
      cascadeProtection: true,
    },
  },
  aggressive: {
    icon: FiZap,
    color: 'red',
    defaults: {
      maxLayers: 30,
      stopLoss: 15,
      takeProfit: 5,
      leverage: 10,
      priceStep: 1,
      multiplier: 2,
      trendFilter: false,
      cascadeProtection: true,
    },
  },
}

// 策略類型
const STRATEGY_TYPES = [
  {
    type: 'dca',
    icon: FiClock,
    featureKeys: ['dynamicSpacing', 'tripleTP', 'cascadeProtection'],
  },
  {
    type: 'martingale',
    icon: FiTrendingDown,
    featureKeys: ['doubleDown', 'riskDecrement', 'reverseMartingale'],
  },
  {
    type: 'combo',
    icon: FiServer,
    featureKeys: ['longShortHedge', 'adaptiveWeight', 'allMarketCoverage'],
  },
  {
    type: 'trend',
    icon: FiTrendingUp,
    featureKeys: ['trendIdentification', 'dynamicTP', 'trailingStop'],
  },
]

interface StrategyWizardProps {
  onComplete: (config: StrategyConfig) => void
  onCancel: () => void
  initialConfig?: StrategyConfig
}

const StrategyWizard: React.FC<StrategyWizardProps> = ({
  onComplete,
  onCancel,
  initialConfig,
}) => {
  const { t } = useTranslation()
  const { isOpen: showAdvanced, onToggle: toggleAdvanced } = useDisclosure()
  const [activeStep, setActiveStep] = useState(0)
  const [template, setTemplate] = useState<string>('balanced')
  const [strategyType, setStrategyType] = useState<string>('dca')
  const [direction, setDirection] = useState<'LONG' | 'SHORT' | 'BOTH'>('LONG')
  const [symbol, setSymbol] = useState<string>('BTCUSDT')
  const [capital, setCapital] = useState<number>(1000)
  const [parameters, setParameters] = useState<Record<string, any>>(
    STRATEGY_TEMPLATES.balanced.defaults
  )
  const [riskAssessment, setRiskAssessment] = useState<RiskAssessment | null>(null)
  const [isAssessing, setIsAssessing] = useState(false)

  // 步驟定義
  const steps = [
    { label: t('strategyWizard.steps.style.label'), description: t('strategyWizard.steps.style.description') },
    { label: t('strategyWizard.steps.strategy.label'), description: t('strategyWizard.steps.strategy.description') },
    { label: t('strategyWizard.steps.params.label'), description: t('strategyWizard.steps.params.description') },
    { label: t('strategyWizard.steps.riskAssessment.label'), description: t('strategyWizard.steps.riskAssessment.description') },
    { label: t('strategyWizard.steps.confirm.label'), description: t('strategyWizard.steps.confirm.description') },
  ]

  // 處理模板選擇
  const handleTemplateSelect = useCallback((templateKey: string) => {
    setTemplate(templateKey)
    const templateConfig = STRATEGY_TEMPLATES[templateKey as keyof typeof STRATEGY_TEMPLATES]
    setParameters(templateConfig.defaults)
  }, [])

  // 處理參數變化
  const handleParamChange = useCallback((key: string, value: any) => {
    setParameters((prev) => ({ ...prev, [key]: value }))
  }, [])

  // 執行風險評估
  const runRiskAssessment = useCallback(async () => {
    setIsAssessing(true)
    try {
      // 模擬 API 調用
      await new Promise((resolve) => setTimeout(resolve, 1500))

      // 計算風險評分
      let score = 100
      const warnings: string[] = []
      const suggestions: RiskAssessment['suggestions'] = []

      // 評估槓桿
      if (parameters.leverage > 10) {
        score -= 20
        warnings.push(t('strategyWizard.warnings.highLeverage'))
      } else if (parameters.leverage > 5) {
        score -= 10
      }

      if (!parameters.stopLoss || parameters.stopLoss <= 0) {
        score -= 25
        warnings.push(t('strategyWizard.warnings.noStopLoss'))
        suggestions.push({
          title: t('strategyWizard.suggestions.addStopLoss'),
          description: t('strategyWizard.suggestions.addStopLossDesc'),
          priority: 'high',
        })
      } else if (parameters.stopLoss > 20) {
        score -= 10
        suggestions.push({
          title: t('strategyWizard.suggestions.adjustStopLoss'),
          description: t('strategyWizard.suggestions.adjustStopLossDesc'),
          priority: 'medium',
        })
      }

      if (parameters.maxLayers > 30) {
        score -= 15
        warnings.push(t('strategyWizard.warnings.tooManyLayers'))
      }

      if (!parameters.trendFilter) {
        score -= 5
        suggestions.push({
          title: t('strategyWizard.suggestions.enableTrendFilter'),
          description: t('strategyWizard.suggestions.enableTrendFilterDesc'),
          priority: 'medium',
        })
      }

      // 確保評分在有效範圍內
      score = Math.max(0, Math.min(100, score))

      const riskLevel: RiskAssessment['riskLevel'] =
        score >= 80 ? 'low' :
        score >= 60 ? 'medium' :
        score >= 40 ? 'high' : 'extreme'

      setRiskAssessment({
        overallScore: score,
        riskLevel,
        scoreBreakdown: {
          capitalManagement: Math.min(25, Math.round(score * 0.25)),
          riskControl: Math.min(25, Math.round(score * 0.25)),
          strategyFit: Math.min(25, Math.round(score * 0.25)),
          marketCondition: Math.min(25, Math.round(score * 0.25)),
        },
        warnings,
        suggestions,
        recommended: score >= 60,
      })
    } catch (error) {
      console.error('Risk assessment failed:', error)
    } finally {
      setIsAssessing(false)
    }
  }, [parameters, t])

  // 下一步
  const handleNext = useCallback(() => {
    if (activeStep === 3 && !riskAssessment) {
      runRiskAssessment()
    }
    setActiveStep((prev) => prev + 1)
  }, [activeStep, riskAssessment, runRiskAssessment])

  // 上一步
  const handleBack = useCallback(() => {
    setActiveStep((prev) => prev - 1)
  }, [])

  // 完成配置
  const handleComplete = useCallback(() => {
    const config: StrategyConfig = {
      type: strategyType,
      name: `${t(`strategyWizard.strategyTypes.${strategyType}.name`)}_${symbol}`,
      direction,
      parameters: {
        symbol,
        capital,
        ...parameters,
      },
    }
    onComplete(config)
  }, [strategyType, symbol, direction, capital, parameters, onComplete, t])

  // 獲取風險等級顏色
  const getRiskColor = (level: string) => {
    switch (level) {
      case 'low': return 'green'
      case 'medium': return 'yellow'
      case 'high': return 'orange'
      case 'extreme': return 'red'
      default: return 'gray'
    }
  }

  // 渲染步驟內容
  const renderStepContent = (step: number) => {
    switch (step) {
      case 0:
        return (
          <Box>
            <Heading size="md" mb={4}>
              {t('strategyWizard.selectTradingStyle')}
            </Heading>
            <SimpleGrid columns={{ base: 1, md: 3 }} spacing={4}>
              {Object.entries(STRATEGY_TEMPLATES).map(([key, value]) => (
                <Card
                  key={key}
                  cursor="pointer"
                  border="2px"
                  borderColor={template === key ? `${value.color}.500` : 'gray.200'}
                  bg={template === key ? `${value.color}.50` : 'white'}
                  _hover={{ transform: 'translateY(-4px)', boxShadow: 'lg' }}
                  transition="all 0.3s"
                  onClick={() => handleTemplateSelect(key)}
                >
                  <CardBody>
                    <HStack mb={2}>
                      <Icon as={value.icon} boxSize={6} color={`${value.color}.500`} />
                      <Text fontSize="lg" fontWeight="semibold">
                        {t(`strategyWizard.templates.${key}.name`)}
                      </Text>
                    </HStack>
                    <Text fontSize="sm" color="gray.600">
                      {t(`strategyWizard.templates.${key}.description`)}
                    </Text>
                    {template === key && (
                      <Badge mt={2} colorScheme="blue">
                        {t('strategyWizard.selected')}
                      </Badge>
                    )}
                  </CardBody>
                </Card>
              ))}
            </SimpleGrid>
          </Box>
        )

      case 1:
        return (
          <Box>
            <Heading size="md" mb={4}>
              {t('strategyWizard.selectStrategyType')}
            </Heading>
            <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4} mb={6}>
              {STRATEGY_TYPES.map((strategy) => (
                <Card
                  key={strategy.type}
                  cursor="pointer"
                  border="2px"
                  borderColor={strategyType === strategy.type ? 'blue.500' : 'gray.200'}
                  bg={strategyType === strategy.type ? 'blue.50' : 'white'}
                  _hover={{ transform: 'translateY(-4px)', boxShadow: 'lg' }}
                  transition="all 0.3s"
                  onClick={() => setStrategyType(strategy.type)}
                >
                  <CardBody>
                    <HStack mb={2}>
                      <Icon as={strategy.icon} boxSize={5} />
                      <Text fontSize="lg" fontWeight="semibold">
                        {t(`strategyWizard.strategyTypes.${strategy.type}.name`)}
                      </Text>
                    </HStack>
                    <Text fontSize="sm" color="gray.600" mb={3}>
                      {t(`strategyWizard.strategyTypes.${strategy.type}.description`)}
                    </Text>
                    <Flex gap={2} flexWrap="wrap">
                      {strategy.featureKeys.map((featureKey) => (
                        <Badge
                          key={featureKey}
                          variant="outline"
                          colorScheme="gray"
                        >
                          {t(`strategyWizard.features.${featureKey}`)}
                        </Badge>
                      ))}
                    </Flex>
                  </CardBody>
                </Card>
              ))}
            </SimpleGrid>

            <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
              <FormControl>
                <FormLabel>{t('strategyWizard.tradingPair')}</FormLabel>
                <Box as="select"
                  value={symbol}
                  onChange={(e) => setSymbol(e.target.value)}
                  p={2}
                  border="1px solid"
                  borderColor="gray.300"
                  borderRadius="md"
                  w="full"
                >
                  <option value="BTCUSDT">BTC/USDT</option>
                  <option value="ETHUSDT">ETH/USDT</option>
                  <option value="BNBUSDT">BNB/USDT</option>
                  <option value="SOLUSDT">SOL/USDT</option>
                </Box>
              </FormControl>
              <FormControl>
                <FormLabel>{t('strategyWizard.tradingDirection')}</FormLabel>
                <Box as="select"
                  value={direction}
                  onChange={(e) => setDirection(e.target.value as any)}
                  p={2}
                  border="1px solid"
                  borderColor="gray.300"
                  borderRadius="md"
                  w="full"
                >
                  <option value="LONG">{t('strategyWizard.longOnly')}</option>
                  <option value="SHORT">{t('strategyWizard.shortOnly')}</option>
                  <option value="BOTH">{t('strategyWizard.longShortBoth')}</option>
                </Box>
              </FormControl>
            </SimpleGrid>
          </Box>
        )

      case 2:
        return (
          <Box>
            <Heading size="md" mb={4}>
              {t('strategyWizard.configureParams')}
            </Heading>

            <Card mb={4} p={4}>
              <Text fontSize="lg" fontWeight="semibold" mb={4}>
                {t('strategyWizard.capitalConfig')}
              </Text>
              <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
                <FormControl>
                  <FormLabel>{t('strategyWizard.capitalInput')}</FormLabel>
                  <Input
                    type="number"
                    min={100}
                    value={capital}
                    onChange={(e) => setCapital(Number(e.target.value))}
                  />
                </FormControl>
                <FormControl>
                  <FormLabel>
                    {t('strategyWizard.leverageMultiplier')}: {parameters.leverage}x
                  </FormLabel>
                  <Box mt={4}>
                    <Input
                      type="range"
                      min={1}
                      max={20}
                      value={parameters.leverage}
                      onChange={(e) => handleParamChange('leverage', Number(e.target.value))}
                    />
                    <Flex justify="space-between" fontSize="xs" color="gray.500" mt={1}>
                      <Text>1x</Text>
                      <Text>5x</Text>
                      <Text>10x</Text>
                      <Text>20x</Text>
                    </Flex>
                  </Box>
                </FormControl>
              </SimpleGrid>
            </Card>

            <Card mb={4} p={4}>
              <Text fontSize="lg" fontWeight="semibold" mb={4}>
                {t('strategyWizard.riskControl')}
              </Text>
              <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
                <FormControl>
                  <FormLabel>
                    {t('strategyWizard.stopLossRatio')}: {parameters.stopLoss}%
                  </FormLabel>
                  <Box mt={4}>
                    <Input
                      type="range"
                      min={1}
                      max={30}
                      value={parameters.stopLoss}
                      onChange={(e) => handleParamChange('stopLoss', Number(e.target.value))}
                    />
                    <Flex justify="space-between" fontSize="xs" color="gray.500" mt={1}>
                      <Text>5%</Text>
                      <Text>15%</Text>
                      <Text>30%</Text>
                    </Flex>
                  </Box>
                </FormControl>
                <FormControl>
                  <FormLabel>
                    {t('strategyWizard.takeProfitRatio')}: {parameters.takeProfit}%
                  </FormLabel>
                  <Box mt={4}>
                    <Input
                      type="range"
                      min={0.5}
                      max={10}
                      step={0.5}
                      value={parameters.takeProfit}
                      onChange={(e) => handleParamChange('takeProfit', Number(e.target.value))}
                    />
                    <Flex justify="space-between" fontSize="xs" color="gray.500" mt={1}>
                      <Text>1%</Text>
                      <Text>5%</Text>
                      <Text>10%</Text>
                    </Flex>
                  </Box>
                </FormControl>
                <FormControl>
                  <FormLabel>
                    {t('strategyWizard.maxLayers')}: {parameters.maxLayers}
                  </FormLabel>
                  <Box mt={4}>
                    <Input
                      type="range"
                      min={5}
                      max={50}
                      value={parameters.maxLayers}
                      onChange={(e) => handleParamChange('maxLayers', Number(e.target.value))}
                    />
                    <Flex justify="space-between" fontSize="xs" color="gray.500" mt={1}>
                      <Text>10</Text>
                      <Text>30</Text>
                      <Text>50</Text>
                    </Flex>
                  </Box>
                </FormControl>
                <FormControl>
                  <FormLabel>
                    {t('strategyWizard.priceSpacing')}: {parameters.priceStep}%
                  </FormLabel>
                  <Box mt={4}>
                    <Input
                      type="range"
                      min={0.5}
                      max={5}
                      step={0.1}
                      value={parameters.priceStep}
                      onChange={(e) => handleParamChange('priceStep', Number(e.target.value))}
                    />
                    <Flex justify="space-between" fontSize="xs" color="gray.500" mt={1}>
                      <Text>1%</Text>
                      <Text>2%</Text>
                      <Text>5%</Text>
                    </Flex>
                  </Box>
                </FormControl>
              </SimpleGrid>
            </Card>

            <Card p={4}>
              <Flex justify="space-between" align="center">
                <Text fontSize="lg" fontWeight="semibold">
                  {t('strategyWizard.advancedSettings')}
                </Text>
                <IconButton
                  aria-label="Toggle advanced"
                  icon={showAdvanced ? <FiChevronUp /> : <FiChevronDown />}
                  onClick={toggleAdvanced}
                  variant="ghost"
                />
              </Flex>
              <Collapse in={showAdvanced}>
                <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4} mt={4}>
                  <FormControl display="flex" alignItems="center">
                    <Switch
                      id="trend-filter"
                      isChecked={parameters.trendFilter}
                      onChange={(e) => handleParamChange('trendFilter', e.target.checked)}
                      mr={3}
                    />
                    <FormLabel htmlFor="trend-filter" mb={0}>
                      {t('strategyWizard.trendFilter')}
                    </FormLabel>
                  </FormControl>
                  <FormControl display="flex" alignItems="center">
                    <Switch
                      id="cascade-protection"
                      isChecked={parameters.cascadeProtection}
                      onChange={(e) => handleParamChange('cascadeProtection', e.target.checked)}
                      mr={3}
                    />
                    <FormLabel htmlFor="cascade-protection" mb={0}>
                      {t('strategyWizard.cascadeProtectionSwitch')}
                    </FormLabel>
                  </FormControl>
                  {strategyType === 'martingale' && (
                    <FormControl>
                      <FormLabel>
                        {t('strategyWizard.positionMultiplier')}: {parameters.multiplier}x
                      </FormLabel>
                      <Box mt={4}>
                        <Input
                          type="range"
                          min={1}
                          max={3}
                          step={0.1}
                          value={parameters.multiplier}
                          onChange={(e) => handleParamChange('multiplier', Number(e.target.value))}
                        />
                        <Flex justify="space-between" fontSize="xs" color="gray.500" mt={1}>
                          <Text>1x</Text>
                          <Text>2x</Text>
                          <Text>3x</Text>
                        </Flex>
                      </Box>
                    </FormControl>
                  )}
                </SimpleGrid>
              </Collapse>
            </Card>
          </Box>
        )

      case 3:
        return (
          <Box>
            <Heading size="md" mb={4}>
              {t('strategyWizard.aiRiskAssessment')}
            </Heading>

            {isAssessing ? (
              <Flex direction="column" align="center" py={8}>
                <Spinner size="xl" mb={4} />
                <Text>{t('strategyWizard.analyzingStrategy')}</Text>
              </Flex>
            ) : riskAssessment ? (
              <Box>
                <Card mb={4} p={6} textAlign="center" bg="gray.50">
                  <Text fontSize="4xl" fontWeight="bold" mb={2}>
                    {riskAssessment.overallScore}
                  </Text>
                  <Badge colorScheme={getRiskColor(riskAssessment.riskLevel)} fontSize="md" px={3} py={1}>
                    {t(`strategyWizard.riskLevels.${riskAssessment.riskLevel}`)}
                  </Badge>
                </Card>

                <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4} mb={4}>
                  {Object.entries(riskAssessment.scoreBreakdown).map(([key, value]) => (
                    <Card key={key} p={4} textAlign="center">
                      <Text fontSize="2xl" fontWeight="bold">{value}/25</Text>
                      <Text fontSize="sm" color="gray.600">
                        {t(`strategyWizard.scoreBreakdown.${key}`)}
                      </Text>
                    </Card>
                  ))}
                </SimpleGrid>

                {riskAssessment.warnings.length > 0 && (
                  <Alert status="warning" mb={4}>
                    <AlertIcon />
                    <Box>
                      <AlertTitle>{t('strategyWizard.warnings.title')}</AlertTitle>
                      <AlertDescription as="ul" style={{ margin: 0, paddingLeft: 20 }}>
                        {riskAssessment.warnings.map((warning, i) => (
                          <li key={i}>{warning}</li>
                        ))}
                      </AlertDescription>
                    </Box>
                  </Alert>
                )}

                {riskAssessment.suggestions.length > 0 && (
                  <Alert status="info" mb={4}>
                    <AlertIcon />
                    <Box>
                      <AlertTitle>{t('strategyWizard.suggestions.title')}</AlertTitle>
                      <AlertDescription as="ul" style={{ margin: 0, paddingLeft: 20 }}>
                        {riskAssessment.suggestions.map((suggestion, i) => (
                          <li key={i}>
                            <strong>{suggestion.title}</strong>: {suggestion.description}
                          </li>
                        ))}
                      </AlertDescription>
                    </Box>
                  </Alert>
                )}

                <Flex justify="center" mt={4}>
                  <Button
                    variant="outline"
                    leftIcon={<FiRefreshCw />}
                    onClick={runRiskAssessment}
                  >
                    {t('strategyWizard.reassess')}
                  </Button>
                </Flex>
              </Box>
            ) : (
              <Flex justify="center" py={8}>
                <Button
                  colorScheme="blue"
                  size="lg"
                  onClick={runRiskAssessment}
                >
                  {t('strategyWizard.startRiskAssessment')}
                </Button>
              </Flex>
            )}
          </Box>
        )

      case 4:
        return (
          <Box>
            <Heading size="md" mb={4}>
              {t('strategyWizard.confirmStrategyConfig')}
            </Heading>

            <Card mb={4} p={4}>
              <SimpleGrid columns={2} spacing={4} mb={4}>
                <Box>
                  <Text color="gray.600" fontSize="sm">{t('strategyWizard.strategyType')}</Text>
                  <Text fontSize="lg" fontWeight="semibold">
                    {t(`strategyWizard.strategyTypes.${strategyType}.name`)}
                  </Text>
                </Box>
                <Box>
                  <Text color="gray.600" fontSize="sm">{t('strategyWizard.tradingPair')}</Text>
                  <Text fontSize="lg" fontWeight="semibold">{symbol}</Text>
                </Box>
                <Box>
                  <Text color="gray.600" fontSize="sm">{t('strategyWizard.tradingDirection')}</Text>
                  <Text fontSize="lg" fontWeight="semibold">
                    {direction === 'LONG' ? t('strategyWizard.directionLong') : direction === 'SHORT' ? t('strategyWizard.directionShort') : t('strategyWizard.directionBoth')}
                  </Text>
                </Box>
                <Box>
                  <Text color="gray.600" fontSize="sm">{t('strategyWizard.capitalInvested')}</Text>
                  <Text fontSize="lg" fontWeight="semibold">{capital} USDT</Text>
                </Box>
              </SimpleGrid>

              <Divider my={4} />

              <Text fontSize="md" fontWeight="semibold" mb={4}>
                {t('strategyWizard.coreParams')}
              </Text>
              <SimpleGrid columns={3} spacing={4}>
                <Box>
                  <Text color="gray.600" fontSize="sm">{t('strategyWizard.leverage')}</Text>
                  <Text>{parameters.leverage}x</Text>
                </Box>
                <Box>
                  <Text color="gray.600" fontSize="sm">{t('strategyWizard.stopLoss')}</Text>
                  <Text>{parameters.stopLoss}%</Text>
                </Box>
                <Box>
                  <Text color="gray.600" fontSize="sm">{t('strategyWizard.takeProfit')}</Text>
                  <Text>{parameters.takeProfit}%</Text>
                </Box>
                <Box>
                  <Text color="gray.600" fontSize="sm">{t('strategyWizard.maxLayers')}</Text>
                  <Text>{parameters.maxLayers}</Text>
                </Box>
                <Box>
                  <Text color="gray.600" fontSize="sm">{t('strategyWizard.priceStep')}</Text>
                  <Text>{parameters.priceStep}%</Text>
                </Box>
                <Box>
                  <Text color="gray.600" fontSize="sm">{t('strategyWizard.trendFilterLabel')}</Text>
                  <Text>{parameters.trendFilter ? t('strategyWizard.enabled') : t('strategyWizard.disabled')}</Text>
                </Box>
              </SimpleGrid>
            </Card>

            {riskAssessment && (
              <Alert
                status={riskAssessment.recommended ? 'success' : 'warning'}
              >
                <AlertIcon />
                {t('strategyWizard.riskScore')}: {riskAssessment.overallScore}/100 -
                {riskAssessment.recommended ? ` ${t('strategyWizard.recommendStart')}` : ` ${t('strategyWizard.recommendOptimize')}`}
              </Alert>
            )}
          </Box>
        )

      default:
        return null
    }
  }

  return (
    <Box maxW="900px" mx="auto" p={6}>
      <Heading size="lg" mb={2}>
        {t('strategyWizard.title')}
      </Heading>
      <Text color="gray.600" mb={6}>
        {t('strategyWizard.subtitle')}
      </Text>

      {/* 步驟指示器 */}
      <Flex mb={8} align="center">
        {steps.map((step, index) => (
          <React.Fragment key={index}>
            <Flex flex="1" align="center">
              <Box
                w={8}
                h={8}
                borderRadius="full"
                bg={index <= activeStep ? 'blue.500' : 'gray.200'}
                color={index <= activeStep ? 'white' : 'gray.600'}
                display="flex"
                alignItems="center"
                justifyContent="center"
                fontWeight="semibold"
              >
                {index + 1}
              </Box>
              <Box ml={2} flex="1">
                <Text fontSize="sm" fontWeight={index <= activeStep ? 'semibold' : 'normal'}>
                  {step.label}
                </Text>
              </Box>
            </Flex>
            {index < steps.length - 1 && (
              <Box flex="1" h={1} bg={index < activeStep ? 'blue.500' : 'gray.200'} mx={2} />
            )}
          </React.Fragment>
        ))}
      </Flex>

      {/* 步驟內容 */}
      <Box mb={6}>
        {renderStepContent(activeStep)}
      </Box>

      {/* 導航按鈕 */}
      <Flex gap={3}>
        <Button
          isDisabled={activeStep === 0}
          onClick={handleBack}
          variant="outline"
        >
          {t('strategyWizard.previousStep')}
        </Button>
        {activeStep === steps.length - 1 ? (
          <Button
            colorScheme="green"
            onClick={handleComplete}
            leftIcon={<FiPlay />}
          >
            {t('strategyWizard.startStrategy')}
          </Button>
        ) : (
          <Button
            colorScheme="blue"
            onClick={handleNext}
          >
            {t('strategyWizard.nextStep')}
          </Button>
        )}
        <Button
          onClick={onCancel}
          variant="ghost"
          ml="auto"
        >
          {t('strategyWizard.cancel')}
        </Button>
      </Flex>
    </Box>
  )
}

export default StrategyWizard
