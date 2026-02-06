import React, { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../contexts/AuthContext'
import {
  setPassword as setPasswordAPI,
  beginWebAuthnRegistration,
  finishWebAuthnRegistration,
} from '../services/auth'
import { useTranslation } from 'react-i18next'
import LanguageSelector from './LanguageSelector'

const FirstTimeSetup: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { refreshAuth } = useAuth()
  const [step, setStep] = useState<'password' | 'webauthn'>(() => {
    // 從 sessionStorage 恢複設置流程状態
    return (sessionStorage.getItem('setup_step') as 'password' | 'webauthn') || 'password'
  })
  const [password, setPasswordInput] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [deviceName, setDeviceName] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showPassword, setShowPassword] = useState(false)
  const [showConfirmPassword, setShowConfirmPassword] = useState(false)

  const handleSetPassword = async (e: React.FormEvent) => {
    e.preventDefault()
    
    console.log('🔐 handleSetPassword 被調用')
    console.log('🔐 密碼长度:', password.length)
    
    if (!password.trim()) {
      setError(t('firstTimeSetup.enterPassword'))
      return
    }

    if (password.length < 6) {
      setError(t('firstTimeSetup.passwordMinLength'))
      return
    }

    if (password !== confirmPassword) {
      setError(t('firstTimeSetup.passwordMismatch'))
      return
    }

    setIsLoading(true)
    setError(null)

    try {
      console.log('🔐 准备調用 setPassword API...')
      await setPasswordAPI(password)
      console.log('🔐 setPassword API 調用成功')
      // 等待一小段時间确保 Cookie 被浏览器处理
      await new Promise(resolve => setTimeout(resolve, 100))
      // 設置密碼后自动登錄，刷新认证状態
      console.log('🔐 准备刷新认证状態...')
      await refreshAuth()
      console.log('🔐 认证状態刷新完成')
      // 標記正在進行首次設置流程
      sessionStorage.setItem('setup_step', 'webauthn')
      setStep('webauthn')
    } catch (err) {
      console.error('🔐 設置密碼失败:', err)
      // 失败時清理流程標記並回到密碼步骤
      sessionStorage.removeItem('setup_step')
      setStep('password')
      setError(err instanceof Error ? err.message : t('firstTimeSetup.setPasswordFailed'))
    } finally {
      setIsLoading(false)
    }
  }

  const handleRegisterWebAuthn = async () => {
    if (!deviceName.trim()) {
      setError(t('firstTimeSetup.enterDeviceName'))
      return
    }

    setIsLoading(true)
    setError(null)

    try {
      // 1. 开始注册
      const beginResponse = await beginWebAuthnRegistration(deviceName)
      if (!beginResponse.success) {
        throw new Error(t('firstTimeSetup.webauthnRegFailed'))
      }

      // 2. 轉换选项格式
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

      // 3. 調用浏览器 WebAuthn API
      const credential = await navigator.credentials.create({
        publicKey: publicKeyOptions,
      }) as PublicKeyCredential

      console.log('[WebAuthn] 浏览器凭证創建成功:', {
        id: credential.id,
        type: credential.type,
        rawIdLength: credential.rawId.byteLength,
      })

      // 4. 轉换响应格式 - 將 ArrayBuffer 轉换為 base64url 字符串
      const response = credential.response as AuthenticatorAttestationResponse
      
      // 辅助函數：將 ArrayBuffer 轉换為 base64url 字符串
      const arrayBufferToBase64URL = (buffer: ArrayBuffer): string => {
        const bytes = new Uint8Array(buffer)
        let binary = ''
        for (let i = 0; i < bytes.length; i++) {
          binary += String.fromCharCode(bytes[i])
        }
        const base64 = btoa(binary)
        // 轉换為 base64url：替换 + 為 -，/ 為 _，移除填充 =
        return base64.replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
      }

      const credentialResponse = {
        id: credential.id,
        rawId: arrayBufferToBase64URL(credential.rawId),
        response: {
          attestationObject: arrayBufferToBase64URL(response.attestationObject),
          clientDataJSON: arrayBufferToBase64URL(response.clientDataJSON),
        },
        type: credential.type,
      }

      console.log('[WebAuthn] 准备发送注册完成请求:', {
        sessionKey: beginResponse.session_key,
        deviceName,
        responseId: credentialResponse.id,
        rawIdLength: credentialResponse.rawId.length,
        attestationObjectLength: credentialResponse.response.attestationObject.length,
        clientDataJSONLength: credentialResponse.response.clientDataJSON.length,
      })

      // 5. 完成注册
      await finishWebAuthnRegistration(
        beginResponse.session_key,
        deviceName,
        credentialResponse
      )

      // 清除設置流程標記
      sessionStorage.removeItem('setup_step')
      // 刷新认证状態
      await refreshAuth()
      // 检查是否需要配置交易所（首次配置向導）
      sessionStorage.setItem('wizard_step', 'pending')
      navigate('/wizard')
    } catch (err: any) {
      if (err.name === 'NotAllowedError') {
        setError(t('firstTimeSetup.userCancelledWebauthn'))
      } else {
        setError(err.message || t('firstTimeSetup.webauthnRegFailed'))
      }
      setIsLoading(false)
    }
  }

  const skipWebAuthn = () => {
    // 清除設置流程標記
    sessionStorage.removeItem('setup_step')
    // 刷新认证状態
    refreshAuth()
    // 检查是否需要配置交易所（首次配置向導）
    sessionStorage.setItem('wizard_step', 'pending')
    navigate('/wizard')
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
        <h2 style={{ textAlign: 'center', marginBottom: '10px' }}>
          {step === 'password' ? t('firstTimeSetup.stepSetPassword') : t('firstTimeSetup.stepRegisterWebauthn')}
        </h2>
        <div style={{ 
          display: 'flex',
          justifyContent: 'flex-end',
          marginBottom: '10px'
        }}>
          <LanguageSelector />
        </div>
        <div style={{ 
          textAlign: 'center', 
          marginBottom: '20px', 
          fontSize: '12px', 
          color: '#999',
          fontFamily: 'monospace'
        }}>
          {t('firstTimeSetup.versionInfo', { version: __APP_VERSION__, buildTime: __BUILD_TIME__ })}
        </div>

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
                {t('firstTimeSetup.password')}
              </label>
              <div style={{ position: 'relative' }}>
                <input
                  type={showPassword ? 'text' : 'password'}
                  value={password}
                  onChange={(e) => setPasswordInput(e.target.value)}
                  disabled={isLoading}
                  style={{
                    width: '100%',
                    padding: '12px',
                    paddingRight: '40px',
                    border: '1px solid #d9d9d9',
                    borderRadius: '4px',
                    fontSize: '14px'
                  }}
                  placeholder={t('firstTimeSetup.passwordPlaceholder')}
                />
                <button
                  type="button"
                  onClick={() => setShowPassword(!showPassword)}
                  disabled={isLoading}
                  style={{
                    position: 'absolute',
                    right: '8px',
                    top: '50%',
                    transform: 'translateY(-50%)',
                    background: 'none',
                    border: 'none',
                    cursor: isLoading ? 'not-allowed' : 'pointer',
                    padding: '4px',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    opacity: isLoading ? 0.5 : 1
                  }}
                  aria-label={showPassword ? t('firstTimeSetup.hidePassword') : t('firstTimeSetup.showPassword')}
                >
                  {showPassword ? (
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path>
                      <line x1="1" y1="1" x2="23" y2="23"></line>
                    </svg>
                  ) : (
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                      <circle cx="12" cy="12" r="3"></circle>
                    </svg>
                  )}
                </button>
              </div>
            </div>

            <div style={{ marginBottom: '20px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: 'bold' }}>
                {t('firstTimeSetup.confirmPassword')}
              </label>
              <div style={{ position: 'relative' }}>
                <input
                  type={showConfirmPassword ? 'text' : 'password'}
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  disabled={isLoading}
                  style={{
                    width: '100%',
                    padding: '12px',
                    paddingRight: '40px',
                    border: '1px solid #d9d9d9',
                    borderRadius: '4px',
                    fontSize: '14px'
                  }}
                  placeholder={t('firstTimeSetup.confirmPasswordPlaceholder')}
                />
                <button
                  type="button"
                  onClick={() => setShowConfirmPassword(!showConfirmPassword)}
                  disabled={isLoading}
                  style={{
                    position: 'absolute',
                    right: '8px',
                    top: '50%',
                    transform: 'translateY(-50%)',
                    background: 'none',
                    border: 'none',
                    cursor: isLoading ? 'not-allowed' : 'pointer',
                    padding: '4px',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    opacity: isLoading ? 0.5 : 1
                  }}
                  aria-label={showConfirmPassword ? t('firstTimeSetup.hidePassword') : t('firstTimeSetup.showPassword')}
                >
                  {showConfirmPassword ? (
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path>
                      <line x1="1" y1="1" x2="23" y2="23"></line>
                    </svg>
                  ) : (
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                      <circle cx="12" cy="12" r="3"></circle>
                    </svg>
                  )}
                </button>
              </div>
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
              {isLoading ? t('firstTimeSetup.settingUp') : t('firstTimeSetup.nextStep')}
            </button>
          </form>
        ) : (
          <div>
            <div style={{ marginBottom: '20px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: 'bold' }}>
                {t('firstTimeSetup.deviceName')}
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
                placeholder={t('firstTimeSetup.deviceNamePlaceholder')}
              />
              <div style={{ marginTop: '8px', fontSize: '12px', color: '#8c8c8c' }}>
                {t('firstTimeSetup.deviceNameHint')}
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
              {isLoading ? t('firstTimeSetup.registering') : t('firstTimeSetup.registerWebauthn')}
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
              {t('firstTimeSetup.registerLater')}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

export default FirstTimeSetup

