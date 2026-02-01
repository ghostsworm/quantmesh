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

  // 加載當前配置獲取交易所和币种
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
      title: t('aiConfig.configApplied'),
      description: t('aiConfig.configAppliedDesc'),
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
            {t('aiConfig.title')}
          </Heading>
          <Text color="gray.600">
            {t('aiConfig.subtitle')}
          </Text>
        </Box>

        <Alert status="info" borderRadius="md">
          <AlertIcon />
          <Box>
            <AlertTitle>{t('aiConfig.usageTitle')}</AlertTitle>
            <AlertDescription fontSize="sm">
              {t('aiConfig.usageDesc')}
            </AlertDescription>
          </Box>
        </Alert>

        {symbols.length === 0 && (
          <Alert status="warning" borderRadius="md">
            <AlertIcon />
            <Box>
              <AlertTitle>{t('aiConfig.noSymbolsTitle')}</AlertTitle>
              <AlertDescription fontSize="sm">
                {t('aiConfig.noSymbolsDesc')}
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
            <Heading size="md">{t('aiConfig.startConfig')}</Heading>
            <Text textAlign="center" color="gray.600" maxW="md">
              {t('aiConfig.startConfigDesc')}
            </Text>
            <Button
              leftIcon={<StarIcon />}
              colorScheme="purple"
              size="lg"
              onClick={() => setIsWizardOpen(true)}
              isDisabled={symbols.length === 0}
            >
              {t('aiConfig.openAssistant')}
            </Button>
          </VStack>
        </Box>

        <Alert status="warning" borderRadius="md">
          <AlertIcon />
          <Box>
            <AlertTitle>{t('aiConfig.notesTitle')}</AlertTitle>
            <AlertDescription fontSize="sm">
              • {t('aiConfig.notes1')}
              <br />
              • {t('aiConfig.notes2')}
              <br />
              • {t('aiConfig.notes3')}
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
