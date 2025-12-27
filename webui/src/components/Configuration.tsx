import React, { useState, useEffect } from 'react'
import {
  getConfig,
  updateConfig,
  previewConfig,
  getBackups,
  restoreBackup,
  deleteBackup,
  Config,
  ConfigChange,
  ConfigDiff,
  BackupInfo,
} from '../services/config'

const Configuration: React.FC = () => {
  const [config, setConfig] = useState<Config | null>(null)
  const [originalConfig, setOriginalConfig] = useState<Config | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)
  const [showPreview, setShowPreview] = useState(false)
  const [previewDiff, setPreviewDiff] = useState<ConfigDiff | null>(null)
  const [requiresRestart, setRequiresRestart] = useState(false)
  
  // 备份管理
  const [backups, setBackups] = useState<BackupInfo[]>([])
  const [showBackups, setShowBackups] = useState(false)
  const [restoringBackup, setRestoringBackup] = useState<string | null>(null)

  // 加载配置
  const loadConfig = async () => {
    try {
      setLoading(true)
      setError(null)
      const cfg = await getConfig()
      setConfig(cfg)
      setOriginalConfig(JSON.parse(JSON.stringify(cfg))) // 深拷贝
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载配置失败')
    } finally {
      setLoading(false)
    }
  }

  // 加载备份列表
  const loadBackups = async () => {
    try {
      const backupList = await getBackups()
      setBackups(backupList)
    } catch (err) {
      console.error('加载备份列表失败:', err)
    }
  }

  useEffect(() => {
    loadConfig()
    loadBackups()
  }, [])

  // 预览变更
  const handlePreview = async () => {
    if (!config) return

    try {
      setError(null)
      const diff = await previewConfig(config)
      setPreviewDiff(diff)
      setRequiresRestart(diff.requires_restart)
      setShowPreview(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : '预览变更失败')
    }
  }

  // 保存配置
  const handleSave = async () => {
    if (!config) return

    setSaving(true)
    setError(null)
    setSuccess(null)

    try {
      // 先预览
      const diff = await previewConfig(config)
      setPreviewDiff(diff)
      setRequiresRestart(diff.requires_restart)

      // 确认保存
      const result = await updateConfig(config)
      setSuccess(result.message + (result.requires_restart ? ' (需要重启才能生效)' : ''))
      setShowPreview(false)
      
      // 重新加载配置和备份列表
      await loadConfig()
      await loadBackups()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存配置失败')
    } finally {
      setSaving(false)
    }
  }

  // 恢复备份
  const handleRestoreBackup = async (backupId: string) => {
    if (!confirm('确定要恢复此备份吗？当前配置将被覆盖。')) {
      return
    }

    try {
      setRestoringBackup(backupId)
      await restoreBackup(backupId)
      setSuccess('备份恢复成功')
      await loadConfig()
      await loadBackups()
    } catch (err) {
      setError(err instanceof Error ? err.message : '恢复备份失败')
    } finally {
      setRestoringBackup(null)
    }
  }

  // 删除备份
  const handleDeleteBackup = async (backupId: string) => {
    if (!confirm('确定要删除此备份吗？')) {
      return
    }

    try {
      await deleteBackup(backupId)
      setSuccess('备份删除成功')
      await loadBackups()
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除备份失败')
    }
  }

  // 更新配置字段
  const updateConfigField = (path: string, value: any) => {
    if (!config) return

    const keys = path.split('.')
    const newConfig = { ...config }
    let current: any = newConfig

    for (let i = 0; i < keys.length - 1; i++) {
      if (!current[keys[i]]) {
        current[keys[i]] = {}
      }
      current = current[keys[i]]
    }

    current[keys[keys.length - 1]] = value
    setConfig(newConfig)
  }

  // 格式化文件大小
  const formatFileSize = (bytes: number): string => {
    if (bytes < 1024) return bytes + ' B'
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB'
    return (bytes / (1024 * 1024)).toFixed(2) + ' MB'
  }

  // 格式化时间
  const formatTime = (timestamp: string): string => {
    return new Date(timestamp).toLocaleString('zh-CN')
  }

  if (loading) {
    return <div style={{ padding: '40px', textAlign: 'center' }}>加载中...</div>
  }

  if (!config) {
    return <div style={{ padding: '40px', textAlign: 'center', color: 'red' }}>加载配置失败</div>
  }

  return (
    <div style={{ padding: '20px', maxWidth: '1200px', margin: '0 auto' }}>
      <h1>系统配置</h1>

      {error && (
        <div style={{ padding: '10px', marginBottom: '20px', backgroundColor: '#fee', color: '#c33', borderRadius: '4px' }}>
          {error}
        </div>
      )}

      {success && (
        <div style={{ padding: '10px', marginBottom: '20px', backgroundColor: '#efe', color: '#3c3', borderRadius: '4px' }}>
          {success}
        </div>
      )}

      <div style={{ marginBottom: '20px' }}>
        <button
          onClick={() => setShowBackups(!showBackups)}
          style={{ marginRight: '10px', padding: '8px 16px', cursor: 'pointer' }}
        >
          {showBackups ? '隐藏' : '显示'}备份管理
        </button>
        <button
          onClick={handlePreview}
          style={{ marginRight: '10px', padding: '8px 16px', cursor: 'pointer' }}
        >
          预览变更
        </button>
        <button
          onClick={handleSave}
          disabled={saving}
          style={{ padding: '8px 16px', cursor: saving ? 'not-allowed' : 'pointer' }}
        >
          {saving ? '保存中...' : '保存配置'}
        </button>
      </div>

      {/* 备份管理 */}
      {showBackups && (
        <div style={{ marginBottom: '30px', padding: '20px', border: '1px solid #ddd', borderRadius: '4px' }}>
          <h2>备份管理</h2>
          {backups.length === 0 ? (
            <p>暂无备份</p>
          ) : (
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '2px solid #ddd' }}>
                  <th style={{ padding: '8px', textAlign: 'left' }}>备份时间</th>
                  <th style={{ padding: '8px', textAlign: 'left' }}>文件大小</th>
                  <th style={{ padding: '8px', textAlign: 'left' }}>操作</th>
                </tr>
              </thead>
              <tbody>
                {backups.map((backup) => (
                  <tr key={backup.id} style={{ borderBottom: '1px solid #eee' }}>
                    <td style={{ padding: '8px' }}>{formatTime(backup.timestamp)}</td>
                    <td style={{ padding: '8px' }}>{formatFileSize(backup.size)}</td>
                    <td style={{ padding: '8px' }}>
                      <button
                        onClick={() => handleRestoreBackup(backup.id)}
                        disabled={restoringBackup === backup.id}
                        style={{ marginRight: '8px', padding: '4px 8px', cursor: 'pointer' }}
                      >
                        {restoringBackup === backup.id ? '恢复中...' : '恢复'}
                      </button>
                      <button
                        onClick={() => handleDeleteBackup(backup.id)}
                        style={{ padding: '4px 8px', cursor: 'pointer' }}
                      >
                        删除
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* 预览对话框 */}
      {showPreview && previewDiff && (
        <div style={{
          position: 'fixed',
          top: 0,
          left: 0,
          right: 0,
          bottom: 0,
          backgroundColor: 'rgba(0,0,0,0.5)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          zIndex: 1000,
        }}>
          <div style={{
            backgroundColor: 'white',
            padding: '20px',
            borderRadius: '8px',
            maxWidth: '800px',
            maxHeight: '80vh',
            overflow: 'auto',
          }}>
            <h2>配置变更预览</h2>
            {requiresRestart && (
              <div style={{ padding: '10px', marginBottom: '20px', backgroundColor: '#fff3cd', color: '#856404', borderRadius: '4px' }}>
                ⚠️ 部分配置需要重启才能生效
              </div>
            )}
            <div style={{ marginBottom: '20px' }}>
              <h3>变更列表 ({previewDiff.changes.length} 项)</h3>
              <ul style={{ listStyle: 'none', padding: 0 }}>
                {previewDiff.changes.map((change, index) => (
                  <li key={index} style={{ padding: '8px', marginBottom: '8px', backgroundColor: '#f5f5f5', borderRadius: '4px' }}>
                    <strong>{change.path}</strong>
                    <span style={{ marginLeft: '10px', color: change.type === 'added' ? 'green' : change.type === 'deleted' ? 'red' : 'blue' }}>
                      [{change.type}]
                    </span>
                    {change.requires_restart && (
                      <span style={{ marginLeft: '10px', color: 'orange' }}>需要重启</span>
                    )}
                    <div style={{ marginTop: '4px', fontSize: '0.9em', color: '#666' }}>
                      {change.old_value !== undefined && (
                        <div>旧值: {JSON.stringify(change.old_value)}</div>
                      )}
                      {change.new_value !== undefined && (
                        <div>新值: {JSON.stringify(change.new_value)}</div>
                      )}
                    </div>
                  </li>
                ))}
              </ul>
            </div>
            <div style={{ textAlign: 'right' }}>
              <button
                onClick={() => setShowPreview(false)}
                style={{ marginRight: '10px', padding: '8px 16px', cursor: 'pointer' }}
              >
                关闭
              </button>
              <button
                onClick={handleSave}
                disabled={saving}
                style={{ padding: '8px 16px', cursor: saving ? 'not-allowed' : 'pointer' }}
              >
                {saving ? '保存中...' : '确认保存'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 配置编辑表单 */}
      <div style={{ display: 'grid', gap: '20px' }}>
        {/* 交易配置 */}
        <section style={{ padding: '20px', border: '1px solid #ddd', borderRadius: '4px' }}>
          <h2>交易配置</h2>
          <div style={{ display: 'grid', gap: '10px' }}>
            <label>
              交易对:
              <input
                type="text"
                value={config.trading?.symbol || ''}
                onChange={(e) => updateConfigField('trading.symbol', e.target.value)}
                style={{ marginLeft: '10px', padding: '4px', width: '200px' }}
              />
            </label>
            <label>
              价格间隔:
              <input
                type="number"
                step="0.01"
                value={config.trading?.price_interval || 0}
                onChange={(e) => updateConfigField('trading.price_interval', parseFloat(e.target.value))}
                style={{ marginLeft: '10px', padding: '4px', width: '200px' }}
              />
            </label>
            <label>
              订单金额:
              <input
                type="number"
                step="0.01"
                value={config.trading?.order_quantity || 0}
                onChange={(e) => updateConfigField('trading.order_quantity', parseFloat(e.target.value))}
                style={{ marginLeft: '10px', padding: '4px', width: '200px' }}
              />
            </label>
            <label>
              买单窗口大小:
              <input
                type="number"
                value={config.trading?.buy_window_size || 0}
                onChange={(e) => updateConfigField('trading.buy_window_size', parseInt(e.target.value))}
                style={{ marginLeft: '10px', padding: '4px', width: '200px' }}
              />
            </label>
            <label>
              卖单窗口大小:
              <input
                type="number"
                value={config.trading?.sell_window_size || 0}
                onChange={(e) => updateConfigField('trading.sell_window_size', parseInt(e.target.value))}
                style={{ marginLeft: '10px', padding: '4px', width: '200px' }}
              />
            </label>
          </div>
        </section>

        {/* 系统配置 */}
        <section style={{ padding: '20px', border: '1px solid #ddd', borderRadius: '4px' }}>
          <h2>系统配置</h2>
          <div style={{ display: 'grid', gap: '10px' }}>
            <label>
              日志级别:
              <select
                value={config.system?.log_level || 'INFO'}
                onChange={(e) => updateConfigField('system.log_level', e.target.value)}
                style={{ marginLeft: '10px', padding: '4px', width: '200px' }}
              >
                <option value="DEBUG">DEBUG</option>
                <option value="INFO">INFO</option>
                <option value="WARN">WARN</option>
                <option value="ERROR">ERROR</option>
                <option value="FATAL">FATAL</option>
              </select>
            </label>
            <label>
              系统时区:
              <input
                type="text"
                placeholder="例如: Asia/Shanghai"
                value={config.system?.timezone || ''}
                onChange={(e) => updateConfigField('system.timezone', e.target.value)}
                style={{ marginLeft: '10px', padding: '4px', width: '200px' }}
              />
            </label>
            <label>
              <input
                type="checkbox"
                checked={config.system?.cancel_on_exit || false}
                onChange={(e) => updateConfigField('system.cancel_on_exit', e.target.checked)}
                style={{ marginRight: '8px' }}
              />
              退出时撤销所有订单
            </label>
            <label>
              <input
                type="checkbox"
                checked={config.system?.close_positions_on_exit || false}
                onChange={(e) => updateConfigField('system.close_positions_on_exit', e.target.checked)}
                style={{ marginRight: '8px' }}
              />
              退出时平仓
            </label>
          </div>
        </section>

        {/* 风控配置 */}
        <section style={{ padding: '20px', border: '1px solid #ddd', borderRadius: '4px' }}>
          <h2>风控配置</h2>
          <div style={{ display: 'grid', gap: '10px' }}>
            <label>
              <input
                type="checkbox"
                checked={config.risk_control?.enabled || false}
                onChange={(e) => updateConfigField('risk_control.enabled', e.target.checked)}
                style={{ marginRight: '8px' }}
              />
              启用风控
            </label>
            <label>
              成交量倍数:
              <input
                type="number"
                step="0.1"
                value={config.risk_control?.volume_multiplier || 0}
                onChange={(e) => updateConfigField('risk_control.volume_multiplier', parseFloat(e.target.value))}
                style={{ marginLeft: '10px', padding: '4px', width: '200px' }}
              />
            </label>
            <label>
              最大杠杆:
              <input
                type="number"
                value={config.risk_control?.max_leverage || 0}
                onChange={(e) => updateConfigField('risk_control.max_leverage', parseInt(e.target.value))}
                style={{ marginLeft: '10px', padding: '4px', width: '200px' }}
              />
            </label>
          </div>
        </section>
      </div>
    </div>
  )
}

export default Configuration

