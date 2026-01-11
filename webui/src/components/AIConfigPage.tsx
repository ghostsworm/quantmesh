import React, { useState, useEffect } from 'react'
import {
  Box,
  Container,
  Heading,
  VStack,
  Text,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Button,
  useToast,
  Spinner,
  Center,
} from '@chakra-ui/react'
import { StarIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import AIConfigWizard from './AIConfigWizard'
import { getConfig } from '../services/config'

const AIConfigPage: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const [isWizardOpen, setIsWizardOpen] = useState(false)
  const [loading, setLoading] = useState(true)
  const [exchange, setExchange] = useState('binance')
  const [symbols, setSymbols] = useState<string[]>([])

  // 加载当前配置获取交易所和币种
  useEffect(() => {
    const loadConfig = async () => {
      try {
        const config = await getConfig()
        if (config?.app?.current_exchange) {
          setExchange(config.app.current_exchange)
        }
        if (config?.trading?.symbols) {
          const symbolList = config.trading.symbols.map((s: any) => s.symbol).filter(Boolean)
          setSymbols(symbolList)
        }
      } catch (err) {
        console.error('Failed to load config:', err)
      } finally {
        setLoading(false)
      }
    }
    loadConfig()
  }, [])

  const handleSuccess = () => {
    setIsWizardOpen(false)
    toast({
      title: 'AI 配置已应用',
      description: '配置已成功保存，请重启服务使配置生效',
      status: 'success',
      duration: 5000,
    })
  }

  if (loading) {
    return (
      <Container maxW="4xl" py={8}>
        <Center py={12}>
          <Spinner size="xl" />
        </Center>
      </Container>
    )
  }

  return (
    <Container maxW="4xl" py={8}>
      <VStack spacing={6} align="stretch">
        <Box>
          <Heading size="lg" mb={2}>
            AI 智能配置助手
          </Heading>
          <Text color="gray.600">
            根据您的资金和风险偏好，AI 将为您生成最优的网格交易参数和资金分配方案
          </Text>
        </Box>

        <Alert status="info" borderRadius="md">
          <AlertIcon />
          <Box>
            <AlertTitle>使用说明</AlertTitle>
            <AlertDescription fontSize="sm">
              在打开 AI 配置助手时，您需要输入 Gemini API Key。
              AI 配置助手将根据您提供的资金、风险偏好和交易币种，自动生成最优的网格交易参数。
            </AlertDescription>
          </Box>
        </Alert>

        {symbols.length === 0 && (
          <Alert status="warning" borderRadius="md">
            <AlertIcon />
            <Box>
              <AlertTitle>未配置交易币种</AlertTitle>
              <AlertDescription fontSize="sm">
                请先在配置管理中添加交易币种，然后再使用 AI 配置助手。
              </AlertDescription>
            </Box>
          </Alert>
        )}

        <Box
          p={8}
          bg="white"
          borderRadius="lg"
          boxShadow="sm"
          border="1px solid"
          borderColor="gray.200"
        >
          <VStack spacing={4}>
            <StarIcon boxSize={12} color="purple.500" />
            <Heading size="md">开始 AI 配置</Heading>
            <Text textAlign="center" color="gray.600" maxW="md">
              点击下方按钮打开 AI 配置助手，输入您的资金和风险偏好，
              AI 将为您生成最优的配置方案。
            </Text>
            <Button
              leftIcon={<StarIcon />}
              colorScheme="purple"
              size="lg"
              onClick={() => setIsWizardOpen(true)}
              isDisabled={symbols.length === 0}
            >
              打开 AI 配置助手
            </Button>
          </VStack>
        </Box>

        <Alert status="warning" borderRadius="md">
          <AlertIcon />
          <Box>
            <AlertTitle>注意事项</AlertTitle>
            <AlertDescription fontSize="sm">
              • AI 生成的配置仅供参考，请根据实际情况调整
              <br />
              • 应用配置后需要重启服务才能生效
              <br />
              • 建议在应用配置前备份当前配置
            </AlertDescription>
          </Box>
        </Alert>
      </VStack>

      <AIConfigWizard
        isOpen={isWizardOpen}
        onClose={() => setIsWizardOpen(false)}
        onSuccess={handleSuccess}
        exchange={exchange}
        symbols={symbols}
      />
    </Container>
  )
}

export default AIConfigPage
