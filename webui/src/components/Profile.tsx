import React, { useState, useEffect } from 'react'
import { useAuth } from '../contexts/AuthContext'
import {
  changePassword,
  listWebAuthnCredentials,
  beginWebAuthnRegistration,
  finishWebAuthnRegistration,
  deleteWebAuthnCredential,
  WebAuthnCredential,
} from '../services/auth'
import './Profile.css'

const Profile: React.FC = () => {
  const { refreshAuth } = useAuth()
  const [activeTab, setActiveTab] = useState<'password' | 'webauthn'>('password')
  
  // 密码修改相关
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const [passwordSuccess, setPasswordSuccess] = useState<string | null>(null)
  const [passwordLoading, setPasswordLoading] = useState(false)

  // WebAuthn 相关
  const [credentials, setCredentials] = useState<WebAuthnCredential[]>([])
  const [deviceName, setDeviceName] = useState('')
  const [webauthnError, setWebauthnError] = useState<string | null>(null)
  const [webauthnSuccess, setWebauthnSuccess] = useState<string | null>(null)
  const [webauthnLoading, setWebauthnLoading] = useState(false)

  // 加载 WebAuthn 凭证列表
  const loadCredentials = async () => {
    try {
      const response = await listWebAuthnCredentials()
      setCredentials(response.credentials || [])
    } catch (err) {
      console.error('加载凭证失败:', err)
    }
  }

  useEffect(() => {
    if (activeTab === 'webauthn') {
      loadCredentials()
    }
  }, [activeTab])

  // 修改密码
  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault()
    setPasswordError(null)
    setPasswordSuccess(null)

    if (!currentPassword || !newPassword || !confirmPassword) {
      setPasswordError('请填写所有字段')
      return
    }

    if (newPassword.length < 6) {
      setPasswordError('新密码长度至少为6位')
      return
    }

    if (newPassword !== confirmPassword) {
      setPasswordError('两次输入的新密码不一致')
      return
    }

    setPasswordLoading(true)
    try {
      await changePassword(currentPassword, newPassword)
      setPasswordSuccess('密码修改成功')
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
    } catch (err) {
      setPasswordError(err instanceof Error ? err.message : '密码修改失败')
    } finally {
      setPasswordLoading(false)
    }
  }

  // 注册新的 WebAuthn 凭证
  const handleRegisterWebAuthn = async () => {
    if (!deviceName.trim()) {
      setWebauthnError('请输入设备名称')
      return
    }

    setWebauthnLoading(true)
    setWebauthnError(null)
    setWebauthnSuccess(null)

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

      console.log('[WebAuthn] 浏览器凭证创建成功:', {
        id: credential.id,
        type: credential.type,
        rawIdLength: credential.rawId.byteLength,
      })

      // 4. 转换响应格式 - 将 ArrayBuffer 转换为 base64url 字符串
      const response = credential.response as AuthenticatorAttestationResponse
      
      // 辅助函数：将 ArrayBuffer 转换为 base64url 字符串
      const arrayBufferToBase64URL = (buffer: ArrayBuffer): string => {
        const bytes = new Uint8Array(buffer)
        let binary = ''
        for (let i = 0; i < bytes.length; i++) {
          binary += String.fromCharCode(bytes[i])
        }
        const base64 = btoa(binary)
        // 转换为 base64url：替换 + 为 -，/ 为 _，移除填充 =
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

      setWebauthnSuccess('指纹注册成功')
      setDeviceName('')
      await loadCredentials()
    } catch (err: any) {
      if (err.name === 'NotAllowedError') {
        setWebauthnError('用户取消了指纹验证')
      } else {
        setWebauthnError(err.message || '指纹注册失败')
      }
    } finally {
      setWebauthnLoading(false)
    }
  }

  // 删除 WebAuthn 凭证
  const handleDeleteCredential = async (credentialId: string, deviceName: string) => {
    if (!confirm(`确定要删除设备 "${deviceName}" 的凭证吗？`)) {
      return
    }

    try {
      await deleteWebAuthnCredential(credentialId)
      setWebauthnSuccess('凭证已删除')
      await loadCredentials()
    } catch (err) {
      setWebauthnError(err instanceof Error ? err.message : '删除凭证失败')
    }
  }

  return (
    <div className="profile-container">
      <h2>个人资料</h2>

      <div className="profile-tabs">
        <button
          className={`tab-button ${activeTab === 'password' ? 'active' : ''}`}
          onClick={() => setActiveTab('password')}
        >
          修改密码
        </button>
        <button
          className={`tab-button ${activeTab === 'webauthn' ? 'active' : ''}`}
          onClick={() => setActiveTab('webauthn')}
        >
          指纹管理
        </button>
      </div>

      <div className="profile-content">
        {activeTab === 'password' && (
          <div className="password-section">
            <h3>修改密码</h3>
            
            {passwordError && (
              <div className="alert alert-error">{passwordError}</div>
            )}
            {passwordSuccess && (
              <div className="alert alert-success">{passwordSuccess}</div>
            )}

            <form onSubmit={handleChangePassword}>
              <div className="form-group">
                <label>当前密码</label>
                <input
                  type="password"
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                  disabled={passwordLoading}
                  placeholder="请输入当前密码"
                />
              </div>

              <div className="form-group">
                <label>新密码</label>
                <input
                  type="password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  disabled={passwordLoading}
                  placeholder="请输入新密码（至少6位）"
                />
              </div>

              <div className="form-group">
                <label>确认新密码</label>
                <input
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  disabled={passwordLoading}
                  placeholder="请再次输入新密码"
                />
              </div>

              <button
                type="submit"
                className="btn btn-primary"
                disabled={passwordLoading}
              >
                {passwordLoading ? '修改中...' : '修改密码'}
              </button>
            </form>
          </div>
        )}

        {activeTab === 'webauthn' && (
          <div className="webauthn-section">
            <h3>指纹管理</h3>

            {webauthnError && (
              <div className="alert alert-error">{webauthnError}</div>
            )}
            {webauthnSuccess && (
              <div className="alert alert-success">{webauthnSuccess}</div>
            )}

            <div className="register-webauthn">
              <h4>注册新设备</h4>
              <div className="form-group">
                <label>设备名称</label>
                <input
                  type="text"
                  value={deviceName}
                  onChange={(e) => setDeviceName(e.target.value)}
                  disabled={webauthnLoading}
                  placeholder="例如：Chrome on MacBook"
                />
                <small>为这个设备起一个名称，方便识别</small>
              </div>
              <button
                className="btn btn-primary"
                onClick={handleRegisterWebAuthn}
                disabled={webauthnLoading}
              >
                {webauthnLoading ? '注册中...' : '注册指纹'}
              </button>
            </div>

            <div className="credentials-list">
              <h4>已注册的设备</h4>
              {credentials.length === 0 ? (
                <p className="empty-message">暂无已注册的设备</p>
              ) : (
                <table className="credentials-table">
                  <thead>
                    <tr>
                      <th>设备名称</th>
                      <th>注册时间</th>
                      <th>最后使用</th>
                      <th>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {credentials.map((cred) => {
                      // 格式化日期，处理可能的无效日期
                      const formatDate = (dateStr: string | undefined): string => {
                        if (!dateStr) return '未使用'
                        try {
                          const date = new Date(dateStr)
                          if (isNaN(date.getTime())) {
                            return '无效日期'
                          }
                          return date.toLocaleString('zh-CN', {
                            year: 'numeric',
                            month: '2-digit',
                            day: '2-digit',
                            hour: '2-digit',
                            minute: '2-digit',
                            second: '2-digit',
                          })
                        } catch (e) {
                          return '无效日期'
                        }
                      }

                      return (
                        <tr key={cred.id}>
                          <td>{cred.device_name || '未命名设备'}</td>
                          <td>{formatDate(cred.created_at)}</td>
                          <td>{formatDate(cred.last_used_at)}</td>
                          <td>
                            <button
                              className="btn btn-danger btn-sm"
                              onClick={() => handleDeleteCredential(cred.credential_id, cred.device_name || '未命名设备')}
                            >
                              删除
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
      </div>
    </div>
  )
}

export default Profile

