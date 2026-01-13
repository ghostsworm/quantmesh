import React, { useEffect, useState } from 'react'
import {
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Box,
  Collapse,
  useDisclosure,
  Button,
  HStack,
  Spacer,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import { RepeatIcon } from '@chakra-ui/icons'

const ConnectionStatusBanner: React.FC = () => {
  const { t } = useTranslation()
  const [isOnline, setIsOnline] = useState(navigator.onLine)
  const [isBackendReachable, setIsBackendReachable] = useState(true)
  const [isChecking, setIsChecking] = useState(false)
  const { isOpen, onOpen, onClose } = useDisclosure()

  // 检查后端连接
  const checkBackend = async () => {
    setIsChecking(true)
    try {
      const controller = new AbortController()
      const timeoutId = setTimeout(() => controller.abort(), 3000)
      
      const response = await fetch('/api/version', { 
        signal: controller.signal,
        cache: 'no-store'
      })
      clearTimeout(timeoutId)
      setIsBackendReachable(response.ok)
    } catch (error) {
      setIsBackendReachable(false)
    } finally {
      setIsChecking(false)
    }
  }

  useEffect(() => {
    const handleOnline = () => setIsOnline(true)
    const handleOffline = () => setIsOnline(false)

    window.addEventListener('online', handleOnline)
    window.addEventListener('offline', handleOffline)

    const interval = setInterval(checkBackend, 10000) // 自动检查间隔拉长到10秒
    checkBackend()

    return () => {
      window.removeEventListener('online', handleOnline)
      window.removeEventListener('offline', handleOffline)
      clearInterval(interval)
    }
  }, [])

  useEffect(() => {
    if (!isOnline || !isBackendReachable) {
      onOpen()
    } else {
      onClose()
    }
  }, [isOnline, isBackendReachable, onOpen, onClose])

  return (
    <Box position="fixed" top={0} left={0} right={0} zIndex={2000}>
      <Collapse in={isOpen} animateOpacity>
        {!isOnline ? (
          <Alert status="error" variant="solid" py={2}>
            <AlertIcon boxSize="20px" />
            <HStack w="full" spacing={4}>
              <Box>
                <AlertTitle fontSize="sm">{t('connection.offlineTitle')}</AlertTitle>
                <AlertDescription fontSize="xs">
                  {t('connection.offlineDesc')}
                </AlertDescription>
              </Box>
              <Spacer />
              <Button
                size="xs"
                leftIcon={<RepeatIcon />}
                onClick={checkBackend}
                isLoading={isChecking}
                variant="outline"
                colorScheme="whiteAlpha"
              >
                {t('common.retry')}
              </Button>
            </HStack>
          </Alert>
        ) : !isBackendReachable ? (
          <Alert status="warning" variant="solid" bg="orange.500" py={2}>
            <AlertIcon boxSize="20px" />
            <HStack w="full" spacing={4}>
              <Box>
                <AlertTitle fontSize="sm">{t('connection.backendDisconnectedTitle')}</AlertTitle>
                <AlertDescription fontSize="xs">
                  {t('connection.backendDisconnectedDesc')}
                </AlertDescription>
              </Box>
              <Spacer />
              <Button
                size="xs"
                leftIcon={<RepeatIcon />}
                onClick={checkBackend}
                isLoading={isChecking}
                variant="outline"
                colorScheme="whiteAlpha"
              >
                {t('common.retry')}
              </Button>
            </HStack>
          </Alert>
        ) : null}
      </Collapse>
    </Box>
  )
}

export default ConnectionStatusBanner
