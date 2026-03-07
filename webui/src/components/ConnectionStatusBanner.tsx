import React, { useEffect, useState, useCallback, useRef } from 'react'
import {
  Box,
  HStack,
  Text,
  IconButton,
  Tooltip,
  ScaleFade,
} from '@chakra-ui/react'
import { keyframes } from '@emotion/react'
import { useTranslation } from 'react-i18next'
import { RepeatIcon, CloseIcon } from '@chakra-ui/icons'
import { getInitialBackendProbeDelayMs } from '../utils/appRuntimeGuards'

const pulse = keyframes`
  0% { opacity: 1; }
  50% { opacity: 0.4; }
  100% { opacity: 1; }
`

const ConnectionStatusBanner: React.FC = () => {
  const { t } = useTranslation()
  const [isOnline, setIsOnline] = useState(navigator.onLine)
  const [isBackendReachable, setIsBackendReachable] = useState(true)
  const [dismissed, setDismissed] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const reconnectAttemptRef = useRef(0)
  const heartbeatRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const initialConnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const isUnmountedRef = useRef(false)
  const initialProbeDelayMs = getInitialBackendProbeDelayMs(window.location.pathname)

  const clearHeartbeat = useCallback(() => {
    if (heartbeatRef.current) {
      clearInterval(heartbeatRef.current)
      heartbeatRef.current = null
    }
  }, [])

  const clearReconnectTimer = useCallback(() => {
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current)
      reconnectTimerRef.current = null
    }
  }, [])

  const clearInitialConnectTimer = useCallback(() => {
    if (initialConnectTimerRef.current) {
      clearTimeout(initialConnectTimerRef.current)
      initialConnectTimerRef.current = null
    }
  }, [])

  const connectWebSocket = useCallback(() => {
    if (isUnmountedRef.current) return

    // 清理旧连接
    if (wsRef.current) {
      wsRef.current.onopen = null
      wsRef.current.onclose = null
      wsRef.current.onerror = null
      wsRef.current.onmessage = null
      if (wsRef.current.readyState === WebSocket.OPEN || wsRef.current.readyState === WebSocket.CONNECTING) {
        wsRef.current.close()
      }
      wsRef.current = null
    }

    try {
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const host = window.location.host
      const ws = new WebSocket(`${protocol}//${host}/ws`)
      wsRef.current = ws

      ws.onopen = () => {
        if (isUnmountedRef.current) return
        setIsBackendReachable(true)
        setDismissed(false) // 恢复时重置 dismissed 以便下次断连能显示
        reconnectAttemptRef.current = 0

        // 心跳：每 2 秒 ping 一次
        clearHeartbeat()
        heartbeatRef.current = setInterval(() => {
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: 'ping' }))
          }
        }, 2000)
      }

      ws.onclose = () => {
        if (isUnmountedRef.current) return
        setIsBackendReachable(false)
        clearHeartbeat()
        scheduleReconnect()
      }

      ws.onerror = () => {
        // onclose 会紧随 onerror 触发，不需要在这里做额外处理
      }

      ws.onmessage = (event) => {
        try {
          const payload = JSON.parse(event.data)
          if (payload?.type === 'pong') {
            // 心跳正常
          }
        } catch {
          // 忽略解析错误
        }
      }
    } catch {
      if (!isUnmountedRef.current) {
        setIsBackendReachable(false)
        scheduleReconnect()
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clearHeartbeat])

  const scheduleReconnect = useCallback(() => {
    clearReconnectTimer()
    if (isUnmountedRef.current) return

    // 指数退避：1s, 2s, 4s, 8s ... 最大 15s
    const delay = Math.min(1000 * Math.pow(2, reconnectAttemptRef.current), 15000)
    reconnectAttemptRef.current += 1

    reconnectTimerRef.current = setTimeout(() => {
      connectWebSocket()
    }, delay)
  }, [clearReconnectTimer, connectWebSocket])

  // 网络状态监听
  useEffect(() => {
    const handleOnline = () => {
      setIsOnline(true)
      // 网络恢复时立即尝试重连
      reconnectAttemptRef.current = 0
      clearInitialConnectTimer()
      connectWebSocket()
    }
    const handleOffline = () => setIsOnline(false)

    window.addEventListener('online', handleOnline)
    window.addEventListener('offline', handleOffline)

    return () => {
      window.removeEventListener('online', handleOnline)
      window.removeEventListener('offline', handleOffline)
    }
  }, [clearInitialConnectTimer, connectWebSocket])

  // WebSocket 连接管理
  useEffect(() => {
    isUnmountedRef.current = false

    if (initialProbeDelayMs > 0) {
      initialConnectTimerRef.current = setTimeout(() => {
        initialConnectTimerRef.current = null
        connectWebSocket()
      }, initialProbeDelayMs)
    } else {
      connectWebSocket()
    }

    return () => {
      isUnmountedRef.current = true
      clearInitialConnectTimer()
      clearHeartbeat()
      clearReconnectTimer()
      if (wsRef.current) {
        wsRef.current.onopen = null
        wsRef.current.onclose = null
        wsRef.current.onerror = null
        wsRef.current.onmessage = null
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [clearHeartbeat, clearInitialConnectTimer, clearReconnectTimer, connectWebSocket, initialProbeDelayMs])

  const handleRetry = useCallback(() => {
    reconnectAttemptRef.current = 0
    clearInitialConnectTimer()
    connectWebSocket()
  }, [clearInitialConnectTimer, connectWebSocket])

  const hasIssue = !isOnline || !isBackendReachable
  const showBanner = hasIssue && !dismissed

  const statusText = !isOnline
    ? t('connection.offlineShort')
    : t('connection.backendDisconnectedShort')

  const tooltipText = !isOnline
    ? t('connection.offlineDesc')
    : t('connection.backendDisconnectedDesc')

  return (
    <ScaleFade in={showBanner} unmountOnExit>
      <Tooltip label={tooltipText} placement="top-start" hasArrow>
        <Box
          position="fixed"
          bottom={{ base: '70px', md: '16px' }}
          right="16px"
          zIndex={2000}
          bg={!isOnline ? 'red.500' : 'orange.500'}
          color="white"
          borderRadius="full"
          px={3}
          py={1.5}
          boxShadow="lg"
          cursor="default"
          maxW="320px"
        >
          <HStack spacing={2}>
            <Box
              w="6px"
              h="6px"
              borderRadius="full"
              bg="white"
              animation={`${pulse} 1.5s ease-in-out infinite`}
              flexShrink={0}
            />
            <Text fontSize="xs" fontWeight="600" noOfLines={1}>
              {statusText}
            </Text>
            <IconButton
              aria-label={t('common.retry')}
              icon={<RepeatIcon />}
              size="xs"
              variant="ghost"
              color="white"
              _hover={{ bg: 'whiteAlpha.300' }}
              minW="auto"
              h="auto"
              p={1}
              onClick={handleRetry}
            />
            <IconButton
              aria-label={t('common.close')}
              icon={<CloseIcon boxSize="8px" />}
              size="xs"
              variant="ghost"
              color="white"
              _hover={{ bg: 'whiteAlpha.300' }}
              minW="auto"
              h="auto"
              p={1}
              onClick={() => setDismissed(true)}
            />
          </HStack>
        </Box>
      </Tooltip>
    </ScaleFade>
  )
}

export default ConnectionStatusBanner
