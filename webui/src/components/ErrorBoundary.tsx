import React from 'react'
import i18n from 'i18next'

interface Props {
  children: React.ReactNode
}

interface State {
  hasError: boolean
  error: Error | null
  componentStack: string | null
}

/**
 * 应用级错误边界。
 *
 * 在它出现之前，登录后任何一个组件渲染抛错都会让 React 卸载整棵树，
 * 用户看到的是一片白屏（曾被误以为「没有 dashboard」）。
 * 现在改为捕获并显示可读的错误信息 + 刷新按钮，错误同时打到 console，
 * 方便用户把现场反馈给我们。
 *
 * 刻意只用内联样式、不依赖 Chakra/主题，避免主题或 Provider 自身出错时
 * 错误界面也跟着崩。
 */
export default class ErrorBoundary extends React.Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, error: null, componentStack: null }
  }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    // 白屏时这条是最关键的线索，务必落到 console
    console.error('[ErrorBoundary] 渲染异常:', error, info?.componentStack)
    this.setState({ componentStack: info?.componentStack ?? null })
  }

  private handleReload = () => {
    window.location.reload()
  }

  private t(key: string, fallback: string): string {
    try {
      return i18n.t(key, { defaultValue: fallback }) as string
    } catch {
      return fallback
    }
  }

  render() {
    if (!this.state.hasError) {
      return this.props.children
    }

    const { error, componentStack } = this.state
    const details = [error?.stack || error?.message || String(error), componentStack]
      .filter(Boolean)
      .join('\n\n')

    return (
      <div
        role="alert"
        style={{
          minHeight: '100vh',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          padding: '24px',
          background: '#0f1115',
          color: '#e6e6e6',
          fontFamily: 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif'
        }}
      >
        <div
          style={{
            maxWidth: '640px',
            width: '100%',
            background: '#1a1d24',
            border: '1px solid #2c313c',
            borderRadius: '12px',
            padding: '28px 32px'
          }}
        >
          <h1 style={{ fontSize: '20px', fontWeight: 600, margin: '0 0 12px' }}>
            {this.t('error.render_crash_title', '页面加载出错了')}
          </h1>
          <p style={{ fontSize: '14px', lineHeight: 1.7, color: '#aab1bd', margin: '0 0 20px' }}>
            {this.t(
              'error.render_crash_desc',
              '界面渲染时发生异常，通常和配置未初始化或数据缺失有关。可先尝试刷新；若反复出现，请把下面的错误信息反馈给我们。'
            )}
          </p>
          <button
            type="button"
            onClick={this.handleReload}
            style={{
              appearance: 'none',
              border: 'none',
              borderRadius: '8px',
              background: '#3b82f6',
              color: '#fff',
              fontSize: '14px',
              fontWeight: 600,
              padding: '10px 18px',
              cursor: 'pointer'
            }}
          >
            {this.t('error.render_crash_reload', '刷新页面')}
          </button>
          {details && (
            <details style={{ marginTop: '20px' }}>
              <summary style={{ cursor: 'pointer', fontSize: '13px', color: '#aab1bd' }}>
                {this.t('error.render_crash_details', '错误详情')}
              </summary>
              <pre
                style={{
                  marginTop: '12px',
                  maxHeight: '280px',
                  overflow: 'auto',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  fontSize: '12px',
                  lineHeight: 1.6,
                  color: '#c9d1d9',
                  background: '#0f1115',
                  border: '1px solid #2c313c',
                  borderRadius: '8px',
                  padding: '14px'
                }}
              >
                {details}
              </pre>
            </details>
          )}
        </div>
      </div>
    )
  }
}
