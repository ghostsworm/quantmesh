import React, { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../contexts/AuthContext'
import {
  setPassword,
  beginWebAuthnRegistration,
  finishWebAuthnRegistration,
} from '../services/auth'

const FirstTimeSetup: React.FC = () => {
  const navigate = useNavigate()
  const { refreshAuth } = useAuth()
  const [step, setStep] = useState<'password' | 'webauthn'>('password')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [deviceName, setDeviceName] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSetPassword = async (e: React.FormEvent) => {
    e.preventDefault()
    
    if (!password.trim()) {
      setError('请输入密码')
      return
    }

    if (password.length < 6) {
      setError('密码长度至少为6位')
      return
    }

    if (password !== confirmPassword) {
      setError('两次输入的密码不一致')
      return
    }

    setIsLoading(true)
    setError(null)

    try {
      await setPassword(password)
      setStep('webauthn')
    } catch (err) {
      setError(err instanceof Error ? err.message : '设置密码失败')
    } finally {
      setIsLoading(false)
    }
  }

  const handleRegisterWebAuthn = async () => {
    if (!deviceName.trim()) {
      setError('请输入设备名称')
      return
    }

    setIsLoading(true)
    setError(null)

    try {
      // 1. 开始注册
      const beginResponse = await beginWebAuthnRegistration(deviceName)
      if (!beginResponse.success) {
        throw new Error('WebAuthn 注册失败')
      }

      // 2. 转换选项格式
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
      
      if (publicKeyOptions.user && publicKeyOptions.user.id) {
        if (typeof publicKeyOptions.user.id === 'string') {
          publicKeyOptions.user.id = base64URLToArrayBuffer(publicKeyOptions.user.id)
        }
      }

      if (publicKeyOptions.challenge && typeof publicKeyOptions.challenge === 'string') {
        publicKeyOptions.challenge = base64URLToArrayBuffer(publicKeyOptions.challenge)
      }

      // 3. 调用浏览器 WebAuthn API
      const credential = await navigator.credentials.create({
        publicKey: publicKeyOptions,
      }) as PublicKeyCredential

      // 4. 转换响应格式
      const response = credential.response as AuthenticatorAttestationResponse
      const credentialResponse = {
        id: credential.id,
        rawId: Array.from(new Uint8Array(credential.rawId)),
        response: {
          attestationObject: Array.from(new Uint8Array(response.attestationObject)),
          clientDataJSON: Array.from(new Uint8Array(response.clientDataJSON)),
        },
        type: credential.type,
      }

      // 5. 完成注册
      await finishWebAuthnRegistration(
        beginResponse.session_key,
        deviceName,
        credentialResponse
      )

      // 刷新认证状态
      await refreshAuth()
      navigate('/')
    } catch (err: any) {
      if (err.name === 'NotAllowedError') {
        setError('用户取消了指纹验证')
      } else {
        setError(err.message || '指纹注册失败')
      }
      setIsLoading(false)
    }
  }

  const skipWebAuthn = () => {
    // 跳过指纹注册，直接进入系统
    refreshAuth()
    navigate('/')
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
        maxWidth: '500px'
      }}>
        <h2 style={{ textAlign: 'center', marginBottom: '30px' }}>
          {step === 'password' ? '首次设置 - 设置密码' : '首次设置 - 注册指纹'}
        </h2>

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

        {step === 'password' ? (
          <form onSubmit={handleSetPassword}>
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
                placeholder="请输入密码（至少6位）"
              />
            </div>

            <div style={{ marginBottom: '20px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: 'bold' }}>
                确认密码
              </label>
              <input
                type="password"
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
                disabled={isLoading}
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
                placeholder="请再次输入密码"
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
                opacity: isLoading ? 0.6 : 1
              }}
            >
              {isLoading ? '设置中...' : '下一步'}
            </button>
          </form>
        ) : (
          <div>
            <div style={{ marginBottom: '20px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: 'bold' }}>
                设备名称
              </label>
              <input
                type="text"
                value={deviceName}
                onChange={(e) => setDeviceName(e.target.value)}
                disabled={isLoading}
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
                placeholder="例如：Chrome on MacBook"
              />
              <div style={{ marginTop: '8px', fontSize: '12px', color: '#8c8c8c' }}>
                为这个设备起一个名称，方便识别
              </div>
            </div>

            <button
              onClick={handleRegisterWebAuthn}
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
                opacity: isLoading ? 0.6 : 1,
                marginBottom: '12px'
              }}
            >
              {isLoading ? '注册中...' : '注册指纹'}
            </button>

            <button
              onClick={skipWebAuthn}
              disabled={isLoading}
              style={{
                width: '100%',
                padding: '12px',
                backgroundColor: 'transparent',
                color: '#8c8c8c',
                border: '1px solid #d9d9d9',
                borderRadius: '4px',
                fontSize: '14px',
                cursor: isLoading ? 'not-allowed' : 'pointer'
              }}
            >
              稍后注册
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

export default FirstTimeSetup

