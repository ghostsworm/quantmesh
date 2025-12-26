import React, { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
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
  const [showWebAuthn, setShowWebAuthn] = useState(false)

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
    <div style={{ 
      display: 'flex', 
      justifyContent: 'center', 
      alignItems: 'center', 
      minHeight: '100vh',
      backgroundColor: '#f5f5f5'
    }}>
      <div style={{
        backgroundColor: 'white',
        padding: '40px',
        borderRadius: '8px',
        boxShadow: '0 2px 8px rgba(0,0,0,0.1)',
        width: '100%',
        maxWidth: '400px'
      }}>
        <h2 style={{ textAlign: 'center', marginBottom: '30px' }}>登录</h2>

        {error && (
          <div style={{
            padding: '12px',
            backgroundColor: '#fff2f0',
            border: '1px solid #ffccc7',
            borderRadius: '4px',
            color: '#ff4d4f',
            marginBottom: '20px'
          }}>
            {error}
          </div>
        )}

        <form onSubmit={handlePasswordLogin}>
          <div style={{ marginBottom: '20px' }}>
            <label style={{ display: 'block', marginBottom: '8px', fontWeight: 'bold' }}>
              密码
            </label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              disabled={isLoading}
              style={{
                width: '100%',
                padding: '12px',
                border: '1px solid #d9d9d9',
                borderRadius: '4px',
                fontSize: '14px'
              }}
              placeholder="请输入密码"
            />
          </div>

          <button
            type="submit"
            disabled={isLoading}
            style={{
              width: '100%',
              padding: '12px',
              backgroundColor: '#1890ff',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              fontSize: '16px',
              cursor: isLoading ? 'not-allowed' : 'pointer',
              opacity: isLoading ? 0.6 : 1,
              marginBottom: '16px'
            }}
          >
            {isLoading ? '登录中...' : '密码登录'}
          </button>
        </form>

        {hasWebAuthn && (
          <button
            onClick={handleWebAuthnLogin}
            disabled={isLoading}
            style={{
              width: '100%',
              padding: '12px',
              backgroundColor: '#52c41a',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              fontSize: '16px',
              cursor: isLoading ? 'not-allowed' : 'pointer',
              opacity: isLoading ? 0.6 : 1
            }}
          >
            {isLoading ? '验证中...' : '指纹登录'}
          </button>
        )}

        {!hasWebAuthn && (
          <div style={{
            marginTop: '20px',
            padding: '12px',
            backgroundColor: '#f6ffed',
            border: '1px solid #b7eb8f',
            borderRadius: '4px',
            color: '#389e0d',
            fontSize: '14px',
            textAlign: 'center'
          }}>
            未注册指纹，请先完成首次设置
          </div>
        )}
      </div>
    </div>
  )
}

export default Login

