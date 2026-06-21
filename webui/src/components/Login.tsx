import React, { useState, useEffect, useCallback, useRef } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router-dom'
import {
  Box,
  Container,
  VStack,
  Heading,
  FormControl,
  FormLabel,
  Input,
  Button,
  Alert,
  AlertIcon,
  AlertDescription,
  Text,
  HStack,
  Flex,
  Link,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../contexts/AuthContext'
import {
  verifyPassword,
  beginWebAuthnLogin,
  finishWebAuthnLogin,
  recoverPassword,
} from '../services/auth'
import { trackUserLogin } from '../services/telemetry'
import LanguageSelector from './LanguageSelector'

const Login: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { isAuthenticated, hasWebAuthn, refreshAuth } = useAuth()
  const [password, setPassword] = useState('')
  const [showRecovery, setShowRecovery] = useState(false)
  const [recoveryCode, setRecoveryCode] = useState('')
  const [recoveryPassword, setRecoveryPassword] = useState('')
  const [recoveryConfirmPassword, setRecoveryConfirmPassword] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [version, setVersion] = useState<string>('')

  const bgColor = 'gray.50'
  const cardBg = 'white'

  // 用于防止重复导航的 ref
  const hasNavigatedRef = useRef(false)

  // 獲取版本号
  useEffect(() => {
    const fetchVersion = async () => {
      try {
        const response = await fetch('/api/version')
        if (response.ok) {
          const data = await response.json()
          setVersion(data.version || '')
        }
      } catch (err) {
        console.error('Failed to fetch version:', err)
      }
    }
    fetchVersion()
  }, [])

  useEffect(() => {
    // 如果已經登錄，重定向到主页（只执行一次）
    if (isAuthenticated && !hasNavigatedRef.current) {
      hasNavigatedRef.current = true
      navigate('/', { replace: true })
    }
    // 当退出登录时重置标记
    if (!isAuthenticated) {
      hasNavigatedRef.current = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAuthenticated])

  const handlePasswordLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!password.trim()) {
      setError(t('login.enterPassword'))
      return
    }

    setIsLoading(true)
    setError(null)
    setSuccess(null)

    try {
      await verifyPassword(password)
      // 追踪登录事件
      trackUserLogin('password')
      // 驗证成功后，清除配置跳過標記，确保每次登錄都显示配置页面
      sessionStorage.removeItem('config_setup_skipped')
      // 刷新认证状態 - 这会触发 useEffect 中的导航逻辑
      await refreshAuth()
      // 不在这里调用 navigate，让 useEffect 来处理
    } catch (err) {
      setError(err instanceof Error ? err.message : t('login.passwordError'))
    } finally {
      setIsLoading(false)
    }
  }

  const handleWebAuthnLogin = async () => {
    if (!hasWebAuthn) {
      setError(t('login.webauthnNotRegistered'))
      return
    }

    if (!window.isSecureContext || !navigator.credentials?.get) {
      setError(t('login.webauthnNotSecureContext'))
      return
    }

    setIsLoading(true)
    setError(null)
    setSuccess(null)

    try {
      // 1. 开始 WebAuthn 登錄
      const beginResponse = await beginWebAuthnLogin('admin')
      if (!beginResponse.success) {
        throw new Error(t('login.webauthnLoginError'))
      }

      // 2. 轉换 challenge 和 allowCredentials
      const base64URLToArrayBuffer = (base64URL: string): ArrayBuffer => {
        const base64 = base64URL.replace(/-/g, '+').replace(/_/g, '/')
        const padded = base64 + '='.repeat((4 - base64.length % 4) % 4)
        const binary = atob(padded)
        const bytes = new Uint8Array(binary.length)
        for (let i = 0; i < binary.length; i++) {
          bytes[i] = binary.charCodeAt(i)
        }
        return bytes.buffer
      }

      const arrayBufferToBase64URL = (buffer: ArrayBuffer): string => {
        const bytes = new Uint8Array(buffer)
        let binary = ''
        bytes.forEach((byte) => {
          binary += String.fromCharCode(byte)
        })
        return btoa(binary)
          .replace(/\+/g, '-')
          .replace(/\//g, '_')
          .replace(/=+$/g, '')
      }

      const publicKeyOptions: any = { ...beginResponse.options }
      
      if (publicKeyOptions.challenge && typeof publicKeyOptions.challenge === 'string') {
        publicKeyOptions.challenge = base64URLToArrayBuffer(publicKeyOptions.challenge)
      }

      if (publicKeyOptions.allowCredentials && Array.isArray(publicKeyOptions.allowCredentials)) {
        publicKeyOptions.allowCredentials = publicKeyOptions.allowCredentials.map((cred: any) => ({
          ...cred,
          id: typeof cred.id === 'string' ? base64URLToArrayBuffer(cred.id) : cred.id,
        }))
      }

      // 3. 調用浏览器 WebAuthn API
      const credential = await navigator.credentials.get({
        publicKey: publicKeyOptions,
      }) as PublicKeyCredential

      // 4. 轉换响应格式
      const response = credential.response as AuthenticatorAssertionResponse
      const credentialResponse = {
        id: credential.id,
        rawId: arrayBufferToBase64URL(credential.rawId),
        response: {
          authenticatorData: arrayBufferToBase64URL(response.authenticatorData),
          clientDataJSON: arrayBufferToBase64URL(response.clientDataJSON),
          signature: arrayBufferToBase64URL(response.signature),
          userHandle: response.userHandle ? arrayBufferToBase64URL(response.userHandle) : null,
        },
        type: credential.type,
      }

      // 5. 完成免密登錄
      await finishWebAuthnLogin('admin', beginResponse.session_key, credentialResponse)

      // 追踪登录事件
      trackUserLogin('webauthn')
      // 清除配置跳過標記，确保每次登錄都显示配置页面
      sessionStorage.removeItem('config_setup_skipped')
      // 刷新认证状態 - 这会触发 useEffect 中的导航逻辑
      await refreshAuth()
      // 不在这里调用 navigate，让 useEffect 来处理
    } catch (err: any) {
      if (err.name === 'NotAllowedError') {
        setError(t('login.userCancelled'))
      } else {
        setError(err.message || t('login.webauthnLoginFailed'))
      }
      setIsLoading(false)
    }
  }

  const handlePasswordRecovery = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setSuccess(null)

    if (!recoveryCode.trim() || !recoveryPassword || !recoveryConfirmPassword) {
      setError(t('login.recoveryFillAllFields'))
      return
    }
    if (recoveryPassword.length < 6) {
      setError(t('login.recoveryPasswordMinLength'))
      return
    }
    if (recoveryPassword !== recoveryConfirmPassword) {
      setError(t('login.recoveryPasswordMismatch'))
      return
    }

    setIsLoading(true)
    try {
      await recoverPassword(recoveryCode, recoveryPassword)
      setRecoveryCode('')
      setRecoveryPassword('')
      setRecoveryConfirmPassword('')
      setShowRecovery(false)
      setSuccess(t('login.recoverySuccess'))
    } catch (err) {
      setError(err instanceof Error ? err.message : t('login.recoveryFailed'))
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <Box
      minH="100vh"
      display="flex"
      alignItems="center"
      justifyContent="center"
      bg={bgColor}
      position="relative"
    >
      {/* 顶部：语言选擇器 */}
      <Box
        position="absolute"
        top={4}
        right={4}
        zIndex={10}
      >
        <LanguageSelector />
      </Box>

      {/* Bottom: version and Terms/Privacy links */}
      <Box
        position="absolute"
        bottom={4}
        left="50%"
        transform="translateX(-50%)"
        zIndex={10}
        textAlign="center"
      >
        <HStack spacing={2} justify="center" flexWrap="wrap">
          {version && (
            <Text fontSize="sm" color="gray.500">
              {t('common.version', { version })}
            </Text>
          )}
          {version && (
            <Text fontSize="sm" color="gray.400">|</Text>
          )}
          <Link as={RouterLink} to="/terms" fontSize="sm" color="gray.500" _hover={{ color: 'blue.500' }}>
            {t('footer.terms')}
          </Link>
          <Text fontSize="sm" color="gray.400">|</Text>
          <Link as={RouterLink} to="/privacy" fontSize="sm" color="gray.500" _hover={{ color: 'blue.500' }}>
            {t('footer.privacy')}
          </Link>
        </HStack>
      </Box>

      <Container maxW="md">
        <Box
          bg={cardBg}
          p={8}
          borderRadius="lg"
          boxShadow="lg"
        >
          <VStack spacing={6} align="stretch">
            <Heading size="lg" textAlign="center">
              {t('login.title')}
            </Heading>

            {error && (
              <Alert status="error" borderRadius="md">
                <AlertIcon />
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
            {success && (
              <Alert status="success" borderRadius="md">
                <AlertIcon />
                <AlertDescription>{success}</AlertDescription>
              </Alert>
            )}

            {!showRecovery ? (
            <form onSubmit={handlePasswordLogin}>
              <VStack spacing={4} align="stretch">
                <FormControl isRequired>
                  <FormLabel>{t('login.password')}</FormLabel>
                  <Input
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder={t('login.passwordPlaceholder')}
                    size="lg"
                    isDisabled={isLoading}
                  />
                </FormControl>

                <Button
                  type="submit"
                  colorScheme="blue"
                  size="lg"
                  width="full"
                  isLoading={isLoading}
                  loadingText={t('login.loading')}
                >
                  {t('login.passwordLogin')}
                </Button>

                <Button
                  type="button"
                  variant="link"
                  colorScheme="blue"
                  onClick={() => {
                    setError(null)
                    setSuccess(null)
                    setShowRecovery(true)
                  }}
                  isDisabled={isLoading}
                >
                  {t('login.forgotPassword')}
                </Button>
              </VStack>
            </form>
            ) : (
              <form onSubmit={handlePasswordRecovery}>
                <VStack spacing={4} align="stretch">
                  <FormControl isRequired>
                    <FormLabel>{t('login.recoveryCode')}</FormLabel>
                    <Input
                      type="text"
                      value={recoveryCode}
                      onChange={(e) => setRecoveryCode(e.target.value)}
                      placeholder={t('login.recoveryCodePlaceholder')}
                      size="lg"
                      isDisabled={isLoading}
                    />
                  </FormControl>

                  <FormControl isRequired>
                    <FormLabel>{t('login.newPassword')}</FormLabel>
                    <Input
                      type="password"
                      value={recoveryPassword}
                      onChange={(e) => setRecoveryPassword(e.target.value)}
                      placeholder={t('login.newPasswordPlaceholder')}
                      size="lg"
                      isDisabled={isLoading}
                    />
                  </FormControl>

                  <FormControl isRequired>
                    <FormLabel>{t('login.confirmNewPassword')}</FormLabel>
                    <Input
                      type="password"
                      value={recoveryConfirmPassword}
                      onChange={(e) => setRecoveryConfirmPassword(e.target.value)}
                      placeholder={t('login.confirmNewPasswordPlaceholder')}
                      size="lg"
                      isDisabled={isLoading}
                    />
                  </FormControl>

                  <Button
                    type="submit"
                    colorScheme="blue"
                    size="lg"
                    width="full"
                    isLoading={isLoading}
                    loadingText={t('login.recovering')}
                  >
                    {t('login.resetPassword')}
                  </Button>

                  <Button
                    type="button"
                    variant="link"
                    colorScheme="gray"
                    onClick={() => {
                      setError(null)
                      setSuccess(null)
                      setShowRecovery(false)
                    }}
                    isDisabled={isLoading}
                  >
                    {t('login.backToPasswordLogin')}
                  </Button>
                </VStack>
              </form>
            )}

            {hasWebAuthn && (
              <Button
                colorScheme="green"
                size="lg"
                width="full"
                onClick={handleWebAuthnLogin}
                isLoading={isLoading}
                loadingText={t('login.verifying')}
              >
                {t('login.webauthnLogin')}
              </Button>
            )}

            {!hasWebAuthn && (
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <AlertDescription>
                  {t('login.webauthnNotRegisteredMessage')}
                </AlertDescription>
              </Alert>
            )}
          </VStack>
        </Box>
      </Container>
    </Box>
  )
}

export default Login
