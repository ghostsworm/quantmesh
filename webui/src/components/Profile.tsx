import React, { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../contexts/AuthContext'
import {
  changePassword,
  listWebAuthnCredentials,
  beginWebAuthnRegistration,
  finishWebAuthnRegistration,
  deleteWebAuthnCredential,
  generatePasswordRecoveryCode,
  WebAuthnCredential,
} from '../services/auth'
import './Profile.css'

const Profile: React.FC = () => {
  const { t, i18n } = useTranslation()
  const { refreshAuth } = useAuth()
  const [activeTab, setActiveTab] = useState<'password' | 'webauthn' | 'recovery'>('password')
  
  // 密碼修改相关
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const [passwordSuccess, setPasswordSuccess] = useState<string | null>(null)
  const [passwordLoading, setPasswordLoading] = useState(false)
  const [recoveryCode, setRecoveryCode] = useState('')
  const [recoveryError, setRecoveryError] = useState<string | null>(null)
  const [recoverySuccess, setRecoverySuccess] = useState<string | null>(null)
  const [recoveryLoading, setRecoveryLoading] = useState(false)

  // WebAuthn 相关
  const [credentials, setCredentials] = useState<WebAuthnCredential[]>([])
  const [deviceName, setDeviceName] = useState('')
  const [webauthnError, setWebauthnError] = useState<string | null>(null)
  const [webauthnSuccess, setWebauthnSuccess] = useState<string | null>(null)
  const [webauthnLoading, setWebauthnLoading] = useState(false)

  // 加載 WebAuthn 凭证列表
  const loadCredentials = async () => {
    try {
      const response = await listWebAuthnCredentials()
      setCredentials(response.credentials || [])
    } catch (err) {
      console.error('加載凭证失败:', err)
    }
  }

  useEffect(() => {
    if (activeTab === 'webauthn') {
      loadCredentials()
    }
  }, [activeTab])

  // 修改密碼
  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault()
    setPasswordError(null)
    setPasswordSuccess(null)

    if (!currentPassword || !newPassword || !confirmPassword) {
      setPasswordError(t('profile.fillAllFields'))
      return
    }

    if (newPassword.length < 6) {
      setPasswordError(t('profile.passwordMinLength'))
      return
    }

    if (newPassword !== confirmPassword) {
      setPasswordError(t('profile.passwordMismatch'))
      return
    }

    setPasswordLoading(true)
    try {
      await changePassword(currentPassword, newPassword)
      setPasswordSuccess(t('profile.passwordChangeSuccess'))
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
    } catch (err) {
      setPasswordError(err instanceof Error ? err.message : t('profile.passwordChangeFailed'))
    } finally {
      setPasswordLoading(false)
    }
  }

  const handleGenerateRecoveryCode = async () => {
    if (!confirm(t('profile.confirmGenerateRecoveryCode'))) {
      return
    }

    setRecoveryLoading(true)
    setRecoveryError(null)
    setRecoverySuccess(null)
    setRecoveryCode('')
    try {
      const response = await generatePasswordRecoveryCode()
      setRecoveryCode(response.recovery_code)
      setRecoverySuccess(t('profile.recoveryCodeGenerated'))
    } catch (err) {
      setRecoveryError(err instanceof Error ? err.message : t('profile.recoveryCodeGenerateFailed'))
    } finally {
      setRecoveryLoading(false)
    }
  }

  // 注册新的 WebAuthn 凭证
  const handleRegisterWebAuthn = async () => {
    if (!deviceName.trim()) {
      setWebauthnError(t('profile.enterDeviceName'))
      return
    }

    setWebauthnLoading(true)
    setWebauthnError(null)
    setWebauthnSuccess(null)

    try {
      // 1. 开始注册
      const beginResponse = await beginWebAuthnRegistration(deviceName)
      if (!beginResponse.success) {
        throw new Error(t('profile.webauthnRegisterFailed'))
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

      setWebauthnSuccess(t('profile.fingerprintRegisterSuccess'))
      setDeviceName('')
      await loadCredentials()
    } catch (err: any) {
      if (err.name === 'NotAllowedError') {
        setWebauthnError(t('profile.userCancelledFingerprint'))
      } else {
        setWebauthnError(err.message || t('profile.fingerprintRegisterFailed'))
      }
    } finally {
      setWebauthnLoading(false)
    }
  }

  // 刪除 WebAuthn 凭证
  const handleDeleteCredential = async (credentialId: string, deviceName: string) => {
    if (!confirm(t('profile.confirmDeleteCredential', { deviceName }))) {
      return
    }

    try {
      await deleteWebAuthnCredential(credentialId)
      setWebauthnSuccess(t('profile.credentialDeleted'))
      await loadCredentials()
    } catch (err) {
      setWebauthnError(err instanceof Error ? err.message : t('profile.deleteCredentialFailed'))
    }
  }

  return (
    <div className="profile-container">
      <h2>{t('profile.title')}</h2>

      <div className="profile-tabs">
        <button
          className={`tab-button ${activeTab === 'password' ? 'active' : ''}`}
          onClick={() => setActiveTab('password')}
        >
          {t('profile.changePassword')}
        </button>
        <button
          className={`tab-button ${activeTab === 'webauthn' ? 'active' : ''}`}
          onClick={() => setActiveTab('webauthn')}
        >
          {t('profile.fingerprintManagement')}
        </button>
        <button
          className={`tab-button ${activeTab === 'recovery' ? 'active' : ''}`}
          onClick={() => setActiveTab('recovery')}
        >
          {t('profile.passwordRecovery')}
        </button>
      </div>

      <div className="profile-content">
        {activeTab === 'password' && (
          <div className="password-section">
            <h3>{t('profile.changePassword')}</h3>
            
            {passwordError && (
              <div className="alert alert-error">{passwordError}</div>
            )}
            {passwordSuccess && (
              <div className="alert alert-success">{passwordSuccess}</div>
            )}

            <form onSubmit={handleChangePassword}>
              <div className="form-group">
                <label>{t('profile.currentPassword')}</label>
                <input
                  type="password"
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                  disabled={passwordLoading}
                  placeholder={t('profile.enterCurrentPassword')}
                />
              </div>

              <div className="form-group">
                <label>{t('profile.newPassword')}</label>
                <input
                  type="password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  disabled={passwordLoading}
                  placeholder={t('profile.enterNewPassword')}
                />
              </div>

              <div className="form-group">
                <label>{t('profile.confirmNewPassword')}</label>
                <input
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  disabled={passwordLoading}
                  placeholder={t('profile.reenterNewPassword')}
                />
              </div>

              <button
                type="submit"
                className="btn btn-primary"
                disabled={passwordLoading}
              >
                {passwordLoading ? t('profile.changing') : t('profile.changePassword')}
              </button>
            </form>
          </div>
        )}

        {activeTab === 'webauthn' && (
          <div className="webauthn-section">
            <h3>{t('profile.fingerprintManagement')}</h3>

            {webauthnError && (
              <div className="alert alert-error">{webauthnError}</div>
            )}
            {webauthnSuccess && (
              <div className="alert alert-success">{webauthnSuccess}</div>
            )}

            <div className="register-webauthn">
              <h4>{t('profile.registerNewDevice')}</h4>
              <div className="form-group">
                <label>{t('profile.deviceName')}</label>
                <input
                  type="text"
                  value={deviceName}
                  onChange={(e) => setDeviceName(e.target.value)}
                  disabled={webauthnLoading}
                  placeholder={t('profile.deviceNamePlaceholder')}
                />
                <small>{t('profile.deviceNameHint')}</small>
              </div>
              <button
                className="btn btn-primary"
                onClick={handleRegisterWebAuthn}
                disabled={webauthnLoading}
              >
                {webauthnLoading ? t('profile.registering') : t('profile.registerFingerprint')}
              </button>
            </div>

            <div className="credentials-list">
              <h4>{t('profile.registeredDevices')}</h4>
              {credentials.length === 0 ? (
                <p className="empty-message">{t('profile.noRegisteredDevices')}</p>
              ) : (
                <table className="credentials-table">
                  <thead>
                    <tr>
                      <th>{t('profile.deviceName')}</th>
                      <th>{t('profile.registerTime')}</th>
                      <th>{t('profile.lastUsed')}</th>
                      <th>{t('profile.actions')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {credentials.map((cred) => {
                      // 格式化日期，处理可能的無效日期
                      const formatDate = (dateStr: string | undefined): string => {
                        if (!dateStr) return t('profile.notUsed')
                        try {
                          const date = new Date(dateStr)
                          if (isNaN(date.getTime())) {
                            return t('profile.invalidDate')
                          }
                          return date.toLocaleString(i18n.language, {
                            year: 'numeric',
                            month: '2-digit',
                            day: '2-digit',
                            hour: '2-digit',
                            minute: '2-digit',
                            second: '2-digit',
                          })
                        } catch (e) {
                          return t('profile.invalidDate')
                        }
                      }

                      return (
                        <tr key={cred.id}>
                          <td>{cred.device_name || t('profile.unnamedDevice')}</td>
                          <td>{formatDate(cred.created_at)}</td>
                          <td>{formatDate(cred.last_used_at)}</td>
                          <td>
                            <button
                              className="btn btn-danger btn-sm"
                              onClick={() => handleDeleteCredential(cred.credential_id, cred.device_name || t('profile.unnamedDevice'))}
                            >
                              {t('profile.delete')}
                            </button>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        )}

        {activeTab === 'recovery' && (
          <div className="recovery-section">
            <h3>{t('profile.passwordRecovery')}</h3>

            {recoveryError && (
              <div className="alert alert-error">{recoveryError}</div>
            )}
            {recoverySuccess && (
              <div className="alert alert-success">{recoverySuccess}</div>
            )}

            <div className="recovery-card">
              <p>{t('profile.recoveryCodeDesc')}</p>
              <button
                className="btn btn-primary"
                onClick={handleGenerateRecoveryCode}
                disabled={recoveryLoading}
              >
                {recoveryLoading ? t('profile.generatingRecoveryCode') : t('profile.generateRecoveryCode')}
              </button>
            </div>

            {recoveryCode && (
              <div className="recovery-code-box">
                <label>{t('profile.recoveryCode')}</label>
                <code>{recoveryCode}</code>
                <p>{t('profile.recoveryCodeSaveHint')}</p>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

export default Profile
