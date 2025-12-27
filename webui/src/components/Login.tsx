import React, { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
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
  useColorModeValue,
} from '@chakra-ui/react'
import { useAuth } from '../contexts/AuthContext'
import {
  verifyPassword,
  beginWebAuthnLogin,
  finishWebAuthnLogin,
} from '../services/auth'

const Login: React.FC = () => {
  const navigate = useNavigate()
  const { isAuthenticated, hasWebAuthn, refreshAuth } = useAuth()
  const [password, setPassword] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const bgColor = useColorModeValue('gray.50', 'gray.900')
  const cardBg = useColorModeValue('white', 'gray.800')

  useEffect(() => {
    // 如果已经登录，重定向到主页
    if (isAuthenticated) {
      navigate('/')
    }
  }, [isAuthenticated, navigate])

  const handlePasswordLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!password.trim()) {
      setError('请输入密码')
      return
    }

    setIsLoading(true)
    setError(null)

    try {
      await verifyPassword(password)
      // 验证成功后，刷新认证状态
      await refreshAuth()
      navigate('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : '密码错误')
    } finally {
      setIsLoading(false)
    }
  }

  const handleWebAuthnLogin = async () => {
    if (!hasWebAuthn) {
      setError('未注册指纹，请先设置')
      return
    }

    setIsLoading(true)
    setError(null)

    try {
      // 1. 开始 WebAuthn 登录
      const beginResponse = await beginWebAuthnLogin('admin')
      if (!beginResponse.success) {
        throw new Error('WebAuthn 登录失败')
      }

      // 2. 转换 challenge 和 allowCredentials
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

      // 3. 调用浏览器 WebAuthn API
      const credential = await navigator.credentials.get({
        publicKey: publicKeyOptions,
      }) as PublicKeyCredential

      // 4. 转换响应格式
      const response = credential.response as AuthenticatorAssertionResponse
      const credentialResponse = {
        id: credential.id,
        rawId: Array.from(new Uint8Array(credential.rawId)),
        response: {
          authenticatorData: Array.from(new Uint8Array(response.authenticatorData)),
          clientDataJSON: Array.from(new Uint8Array(response.clientDataJSON)),
          signature: Array.from(new Uint8Array(response.signature)),
          userHandle: response.userHandle ? Array.from(new Uint8Array(response.userHandle)) : null,
        },
        type: credential.type,
      }

      // 5. 完成登录（需要密码）
      const passwordForWebAuthn = prompt('请输入密码以完成指纹登录:')
      if (!passwordForWebAuthn) {
        setError('需要密码才能完成指纹登录')
        setIsLoading(false)
        return
      }

      await finishWebAuthnLogin('admin', beginResponse.session_key, credentialResponse, passwordForWebAuthn)
      
      // 刷新认证状态
      await refreshAuth()
      navigate('/')
    } catch (err: any) {
      if (err.name === 'NotAllowedError') {
        setError('用户取消了指纹验证')
      } else {
        setError(err.message || '指纹登录失败')
      }
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
    >
      <Container maxW="md">
        <Box
          bg={cardBg}
          p={8}
          borderRadius="lg"
          boxShadow="lg"
        >
          <VStack spacing={6} align="stretch">
            <Heading size="lg" textAlign="center">
              登录
            </Heading>

            {error && (
              <Alert status="error" borderRadius="md">
                <AlertIcon />
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}

            <form onSubmit={handlePasswordLogin}>
              <VStack spacing={4} align="stretch">
                <FormControl isRequired>
                  <FormLabel>密码</FormLabel>
                  <Input
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder="请输入密码"
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
                  loadingText="登录中..."
                >
                  密码登录
                </Button>
              </VStack>
            </form>

            {hasWebAuthn && (
              <Button
                colorScheme="green"
                size="lg"
                width="full"
                onClick={handleWebAuthnLogin}
                isLoading={isLoading}
                loadingText="验证中..."
              >
                指纹登录
              </Button>
            )}

            {!hasWebAuthn && (
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <AlertDescription>
                  未注册指纹，请先完成首次设置
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
