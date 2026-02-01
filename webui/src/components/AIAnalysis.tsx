import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Box,
  Heading,
  SimpleGrid,
  Card,
  CardHeader,
  CardBody,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  Button,
  Badge,
  Text,
  Spinner,
  Center,
  useToast,
  VStack,
  HStack,
  Divider,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
} from '@chakra-ui/react'
import {
  getAIAnalysisStatus,
  getAIMarketAnalysis,
  getAIParameterOptimization,
  getAIRiskAnalysis,
  getAISentimentAnalysis,
  getAIPolymarketSignal,
  triggerAIAnalysis,
  AIMarketAnalysisResponse,
  AIParameterOptimizationResponse,
  AIRiskAnalysisResponse,
  AISentimentAnalysisResponse,
  AIPolymarketSignalResponse,
  AIAnalysisStatus,
} from '../services/api'

const AIAnalysis: React.FC = () => {
  const { t } = useTranslation()
  const [status, setStatus] = useState<AIAnalysisStatus | null>(null)
  const [marketAnalysis, setMarketAnalysis] = useState<AIMarketAnalysisResponse | null>(null)
  const [parameterOptimization, setParameterOptimization] = useState<AIParameterOptimizationResponse | null>(null)
  const [riskAnalysis, setRiskAnalysis] = useState<AIRiskAnalysisResponse | null>(null)
  const [sentimentAnalysis, setSentimentAnalysis] = useState<AISentimentAnalysisResponse | null>(null)
  const [polymarketSignal, setPolymarketSignal] = useState<AIPolymarketSignalResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [triggering, setTriggering] = useState<string | null>(null)
  const toast = useToast()

  const fetchData = async () => {
    try {
      setLoading(true)
      const [statusData, marketData, paramData, riskData, sentimentData, polymarketData] = await Promise.allSettled([
        getAIAnalysisStatus(),
        getAIMarketAnalysis().catch(() => null),
        getAIParameterOptimization().catch(() => null),
        getAIRiskAnalysis().catch(() => null),
        getAISentimentAnalysis().catch(() => null),
        getAIPolymarketSignal().catch(() => null),
      ])

      if (statusData.status === 'fulfilled') {
        setStatus(statusData.value)
      }
      if (marketData.status === 'fulfilled' && marketData.value) {
        setMarketAnalysis(marketData.value)
      }
      if (paramData.status === 'fulfilled' && paramData.value) {
        setParameterOptimization(paramData.value)
      }
      if (riskData.status === 'fulfilled' && riskData.value) {
        setRiskAnalysis(riskData.value)
      }
      if (sentimentData.status === 'fulfilled' && sentimentData.value) {
        setSentimentAnalysis(sentimentData.value)
      }
      if (polymarketData.status === 'fulfilled' && polymarketData.value) {
        setPolymarketSignal(polymarketData.value)
      }
    } catch (error) {
      console.error('Failed to fetch AI analysis data:', error)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
    const interval = setInterval(fetchData, 10000) // 每10秒刷新
    return () => clearInterval(interval)
  }, [])

  const handleTrigger = async (module: string) => {
    try {
      setTriggering(module)
      await triggerAIAnalysis(module)
      toast({
        title: t('aiAnalysis.analysisTriggered'),
        description: t('aiAnalysis.analysisRunning', { module }),
        status: 'success',
        duration: 3000,
        isClosable: true,
      })
      // 等待2秒后刷新數據
      setTimeout(() => {
        fetchData()
      }, 2000)
    } catch (error) {
      toast({
        title: t('aiAnalysis.triggerFailed'),
        description: error instanceof Error ? error.message : 'Unknown error',
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setTriggering(null)
    }
  }

  const getRiskColor = (level: string) => {
    switch (level.toLowerCase()) {
      case 'low':
        return 'green'
      case 'medium':
        return 'yellow'
      case 'high':
        return 'orange'
      case 'critical':
        return 'red'
      default:
        return 'gray'
    }
  }

  const getSignalColor = (signal: string) => {
    switch (signal.toLowerCase()) {
      case 'buy':
        return 'green'
      case 'sell':
        return 'red'
      case 'hold':
        return 'gray'
      default:
        return 'gray'
    }
  }

  if (loading && !status) {
    return (
      <Center h="400px">
        <Spinner size="xl" />
      </Center>
    )
  }

  if (!status || !status.enabled) {
    return (
      <Alert status="warning">
        <AlertIcon />
        <AlertTitle>AI功能未啟用</AlertTitle>
        <AlertDescription>请在配置中啟用AI功能</AlertDescription>
      </Alert>
    )
  }

  return (
    <Box p={6}>
      <Heading size="lg" mb={6}>
        AI分析中心
      </Heading>

      {/* 概览卡片 */}
      <SimpleGrid columns={{ base: 1, md: 2, lg: 5 }} spacing={4} mb={6}>
        {Object.entries(status.modules).map(([module, info]) => (
          <Card key={module}>
            <CardBody>
              <Stat>
                <StatLabel>{module.replace('_', ' ').toUpperCase()}</StatLabel>
                <StatNumber>
                  <Badge colorScheme={info.enabled ? 'green' : 'gray'}>
                    {info.enabled ? t('aiAnalysis.enabled') : t('aiAnalysis.disabled')}
                  </Badge>
                </StatNumber>
                <StatHelpText>
                  {info.has_data ? t('aiAnalysis.hasData') : t('aiAnalysis.noData')}
                </StatHelpText>
              </Stat>
            </CardBody>
          </Card>
        ))}
      </SimpleGrid>

      {/* 市场分析 */}
      <Card mb={6}>
        <CardHeader>
          <HStack justify="space-between">
            <Heading size="md">{t('aiAnalysis.marketAnalysis')}</Heading>
            <Button
              size="sm"
              colorScheme="blue"
              onClick={() => handleTrigger('market')}
              isLoading={triggering === 'market'}
            >
              {t('aiAnalysis.triggerAnalysis')}
            </Button>
          </HStack>
        </CardHeader>
        <CardBody>
          {marketAnalysis && marketAnalysis.analysis ? (
            <VStack align="stretch" spacing={4}>
              <SimpleGrid columns={{ base: 1, md: 3 }} spacing={4}>
                <Stat>
                  <StatLabel>{t('aiAnalysis.trend')}</StatLabel>
                  <StatNumber>
                    <Badge colorScheme={getSignalColor(marketAnalysis.analysis.trend)}>
                      {marketAnalysis.analysis.trend.toUpperCase()}
                    </Badge>
                  </StatNumber>
                </Stat>
                <Stat>
                  <StatLabel>{t('aiAnalysis.signal')}</StatLabel>
                  <StatNumber>
                    <Badge colorScheme={getSignalColor(marketAnalysis.analysis.signal)}>
                      {marketAnalysis.analysis.signal.toUpperCase()}
                    </Badge>
                  </StatNumber>
                </Stat>
                <Stat>
                  <StatLabel>{t('aiAnalysis.confidence')}</StatLabel>
                  <StatNumber>{(marketAnalysis.analysis.confidence * 100).toFixed(1)}%</StatNumber>
                </Stat>
              </SimpleGrid>
              <Divider />
              <Text>{marketAnalysis.analysis.reasoning}</Text>
              <Text fontSize="sm" color="gray.500">
                {t('aiAnalysis.lastUpdate')} {new Date(marketAnalysis.last_update).toLocaleString()}
              </Text>
            </VStack>
          ) : (
            <Text color="gray.500">{t('aiAnalysis.noData')}</Text>
          )}
        </CardBody>
      </Card>

      <Card mb={6}>
        <CardHeader>
          <HStack justify="space-between">
            <Heading size="md">{t('aiAnalysis.parameterOptimization')}</Heading>
            <Button
              size="sm"
              colorScheme="blue"
              onClick={() => handleTrigger('parameter')}
              isLoading={triggering === 'parameter'}
            >
              {t('aiAnalysis.triggerOptimization')}
            </Button>
          </HStack>
        </CardHeader>
        <CardBody>
          {parameterOptimization && parameterOptimization.optimization ? (
            <VStack align="stretch" spacing={4}>
              <SimpleGrid columns={{ base: 1, md: 4 }} spacing={4}>
                <Stat>
                  <StatLabel>{t('aiAnalysis.priceInterval')}</StatLabel>
                  <StatNumber>{parameterOptimization.optimization.recommended_params.price_interval.toFixed(2)}</StatNumber>
                </Stat>
                <Stat>
                  <StatLabel>{t('aiAnalysis.buyWindow')}</StatLabel>
                  <StatNumber>{parameterOptimization.optimization.recommended_params.buy_window_size}</StatNumber>
                </Stat>
                <Stat>
                  <StatLabel>{t('aiAnalysis.sellWindow')}</StatLabel>
                  <StatNumber>{parameterOptimization.optimization.recommended_params.sell_window_size}</StatNumber>
                </Stat>
                <Stat>
                  <StatLabel>{t('aiAnalysis.orderAmount')}</StatLabel>
                  <StatNumber>{parameterOptimization.optimization.recommended_params.order_quantity.toFixed(2)}</StatNumber>
                </Stat>
              </SimpleGrid>
              <Divider />
              <HStack>
                <Text>{t('aiAnalysis.expectedImprovement')}</Text>
                <Badge colorScheme="green">
                  {parameterOptimization.optimization.expected_improvement.toFixed(2)}%
                </Badge>
                <Text>置信度:</Text>
                <Badge>{(parameterOptimization.optimization.confidence * 100).toFixed(1)}%</Badge>
              </HStack>
              <Text>{parameterOptimization.optimization.reasoning}</Text>
              <Text fontSize="sm" color="gray.500">
                {t('aiAnalysis.lastUpdate')} {new Date(parameterOptimization.last_update).toLocaleString()}
              </Text>
            </VStack>
          ) : (
            <Text color="gray.500">{t('aiAnalysis.noData')}</Text>
          )}
        </CardBody>
      </Card>

      <Card mb={6}>
        <CardHeader>
          <HStack justify="space-between">
            <Heading size="md">{t('aiAnalysis.riskAnalysis')}</Heading>
            <Button
              size="sm"
              colorScheme="blue"
              onClick={() => handleTrigger('risk')}
              isLoading={triggering === 'risk'}
            >
              {t('aiAnalysis.triggerAnalysis')}
            </Button>
          </HStack>
        </CardHeader>
        <CardBody>
          {riskAnalysis && riskAnalysis.analysis ? (
            <VStack align="stretch" spacing={4}>
              <HStack>
                <Text>{t('aiAnalysis.riskLevel')}</Text>
                <Badge colorScheme={getRiskColor(riskAnalysis.analysis.risk_level)} size="lg">
                  {riskAnalysis.analysis.risk_level.toUpperCase()}
                </Badge>
                <Text>{t('aiAnalysis.riskScore')}</Text>
                <Badge>{(riskAnalysis.analysis.risk_score * 100).toFixed(1)}</Badge>
              </HStack>
              {riskAnalysis.analysis.warnings && riskAnalysis.analysis.warnings.length > 0 && (
                <Box>
                  <Text fontWeight="bold" mb={2}>{t('aiAnalysis.warning')}</Text>
                  <VStack align="stretch" spacing={2}>
                    {riskAnalysis.analysis.warnings.map((warning, idx) => (
                      <Alert key={idx} status="warning" size="sm">
                        <AlertIcon />
                        {warning}
                      </Alert>
                    ))}
                  </VStack>
                </Box>
              )}
              {riskAnalysis.analysis.recommendations && riskAnalysis.analysis.recommendations.length > 0 && (
                <Box>
                  <Text fontWeight="bold" mb={2}>{t('aiAnalysis.suggestion')}</Text>
                  <VStack align="stretch" spacing={2}>
                    {riskAnalysis.analysis.recommendations.map((rec, idx) => (
                      <Text key={idx} pl={4}>• {rec}</Text>
                    ))}
                  </VStack>
                </Box>
              )}
              <Text>{riskAnalysis.analysis.reasoning}</Text>
              <Text fontSize="sm" color="gray.500">
                {t('aiAnalysis.lastUpdate')} {new Date(riskAnalysis.last_update).toLocaleString()}
              </Text>
            </VStack>
          ) : (
            <Text color="gray.500">{t('aiAnalysis.noData')}</Text>
          )}
        </CardBody>
      </Card>

      <Card mb={6}>
        <CardHeader>
          <HStack justify="space-between">
            <Heading size="md">{t('aiAnalysis.sentimentAnalysis')}</Heading>
            <Button
              size="sm"
              colorScheme="blue"
              onClick={() => handleTrigger('sentiment')}
              isLoading={triggering === 'sentiment'}
            >
              {t('aiAnalysis.triggerAnalysis')}
            </Button>
          </HStack>
        </CardHeader>
        <CardBody>
          {sentimentAnalysis && sentimentAnalysis.analysis ? (
            <VStack align="stretch" spacing={4}>
              <HStack>
                <Text>{t('aiAnalysis.sentimentScore')}</Text>
                <Badge colorScheme={sentimentAnalysis.analysis.sentiment_score > 0 ? 'green' : 'red'} size="lg">
                  {sentimentAnalysis.analysis.sentiment_score.toFixed(2)}
                </Badge>
                <Text>{t('aiAnalysis.trend')}:</Text>
                <Badge>{sentimentAnalysis.analysis.trend.toUpperCase()}</Badge>
              </HStack>
              {sentimentAnalysis.analysis.key_factors && sentimentAnalysis.analysis.key_factors.length > 0 && (
                <Box>
                  <Text fontWeight="bold" mb={2}>{t('aiAnalysis.keyFactors')}</Text>
                  <VStack align="stretch" spacing={2}>
                    {sentimentAnalysis.analysis.key_factors.map((factor, idx) => (
                      <Text key={idx} pl={4}>• {factor}</Text>
                    ))}
                  </VStack>
                </Box>
              )}
              <Text>{sentimentAnalysis.analysis.reasoning}</Text>
              <Text fontSize="sm" color="gray.500">
                {t('aiAnalysis.lastUpdate')} {new Date(sentimentAnalysis.last_update).toLocaleString()}
              </Text>
            </VStack>
          ) : (
            <Text color="gray.500">{t('aiAnalysis.noData')}</Text>
          )}
        </CardBody>
      </Card>

      <Card mb={6}>
        <CardHeader>
          <HStack justify="space-between">
            <Heading size="md">{t('aiAnalysis.polymarketSignal')}</Heading>
            <Button
              size="sm"
              colorScheme="blue"
              onClick={() => handleTrigger('polymarket')}
              isLoading={triggering === 'polymarket'}
            >
              {t('aiAnalysis.triggerAnalysis')}
            </Button>
          </HStack>
        </CardHeader>
        <CardBody>
          {polymarketSignal && polymarketSignal.analysis ? (
            <VStack align="stretch" spacing={4}>
              <HStack>
                <Text>{t('aiAnalysis.signal')}:</Text>
                <Badge colorScheme={getSignalColor(polymarketSignal.analysis.signal)} size="lg">
                  {polymarketSignal.analysis.signal.toUpperCase()}
                </Badge>
                <Text>{t('aiAnalysis.strength')}</Text>
                <Badge>{(polymarketSignal.analysis.strength * 100).toFixed(1)}%</Badge>
                <Text>{t('aiAnalysis.confidence')}:</Text>
                <Badge>{(polymarketSignal.analysis.confidence * 100).toFixed(1)}%</Badge>
              </HStack>
              <Text>{polymarketSignal.analysis.reasoning}</Text>
              <Text fontSize="sm" color="gray.500">
                {t('aiAnalysis.lastUpdate')} {new Date(polymarketSignal.last_update).toLocaleString()}
              </Text>
            </VStack>
          ) : (
            <Text color="gray.500">{t('aiAnalysis.noData')}</Text>
          )}
        </CardBody>
      </Card>
    </Box>
  )
}

export default AIAnalysis

