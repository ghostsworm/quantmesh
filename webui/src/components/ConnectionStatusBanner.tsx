import React, { useEffect, useState, useCallback } from 'react'
import { useLocation } from 'react-router-dom'
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
  const location = useLocation()
  const [isOnline, setIsOnline] = useState(navigator.onLine)
  const [isBackendReachable, setIsBackendReachable] = useState(true)
  const [isChecking, setIsChecking] = useState(false)
  const { isOpen, onOpen, onClose } = useDisclosure()

  // 檢查後端連線
  const checkBackend = useCallback(async () => {
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
  }, [])

  useEffect(() => {
    const handleOnline = () => setIsOnline(true)
    const handleOffline = () => setIsOnline(false)

    window.addEventListener('online', handleOnline)
    window.addEventListener('offline', handleOffline)

    const isLoginPage = location.pathname === '/login'
    // 登入頁已有 /api/version 請求，延遲首檢避免並發多個請求導致排隊變慢
    const initialDelay = isLoginPage ? 3000 : 0
    const initialTimer = setTimeout(() => checkBackend(), initialDelay)
    const interval = setInterval(checkBackend, 10000)

    return () => {
      window.removeEventListener('online', handleOnline)
      window.removeEventListener('offline', handleOffline)
      clearTimeout(initialTimer)
      clearInterval(interval)
    }
  }, [location.pathname, checkBackend])

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
