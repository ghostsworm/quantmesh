import React, { useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import ReactMarkdown from 'react-markdown'
import {
  createMarketInterpretTask,
  pollMarketInterpretUntilComplete,
  MarketInterpretRequest,
} from '../services/api'

interface AIMarketInterpretProps {
  pageType: 'basis' | 'funding'
  symbol: string
  /** 收集当前页面数据快照，传给后端构建 prompt */
  getPageData: () => Record<string, unknown>
}

const AIMarketInterpret: React.FC<AIMarketInterpretProps> = ({
  pageType,
  symbol,
  getPageData,
}) => {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [progress, setProgress] = useState(0)
  const [statusText, setStatusText] = useState('')
  const [result, setResult] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState(true)

  const handleAnalyze = useCallback(async () => {
    setLoading(true)
    setError(null)
    setResult(null)
    setProgress(0)
    setStatusText(t('aiInterpret.creating'))
    setExpanded(true)

    try {
      const pageData = getPageData()
      const request: MarketInterpretRequest = {
        page_type: pageType,
        symbol,
        page_data: pageData,
      }

      // 创建任务
      const taskResp = await createMarketInterpretTask(request)
      setStatusText(t('aiInterpret.analyzing'))

      // 轮询等待结果
      const analysisResult = await pollMarketInterpretUntilComplete(
        taskResp.task_id,
        (prog, status) => {
          setProgress(prog)
          if (status === 'running') {
            setStatusText(t('aiInterpret.analyzing'))
          } else if (status === 'pending') {
            setStatusText(t('aiInterpret.pending'))
          }
        }
      )

      setResult(analysisResult)
      setStatusText('')
    } catch (err) {
      setError(err instanceof Error ? err.message : t('aiInterpret.unknownError'))
      setStatusText('')
    } finally {
      setLoading(false)
      setProgress(100)
    }
  }, [pageType, symbol, getPageData, t])

  return (
    <div className="bg-white rounded-lg shadow p-6">
      {/* 标题行：AI 解读按钮 */}
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-3">
          <h2 className="text-xl font-semibold flex items-center gap-2">
            <span role="img" aria-label="ai">🤖</span>
            {t('aiInterpret.title')}
          </h2>
          {result && (
            <button
              onClick={() => setExpanded(!expanded)}
              className="text-sm text-gray-500 hover:text-gray-700"
            >
              {expanded ? t('aiInterpret.collapse') : t('aiInterpret.expand')}
            </button>
          )}
        </div>
        <button
          onClick={handleAnalyze}
          disabled={loading || !symbol}
          className={`px-5 py-2.5 rounded-lg font-medium text-white transition-all flex items-center gap-2 ${
            loading
              ? 'bg-gray-400 cursor-not-allowed'
              : 'bg-gradient-to-r from-purple-500 to-indigo-600 hover:from-purple-600 hover:to-indigo-700 shadow-md hover:shadow-lg'
          }`}
        >
          {loading ? (
            <>
              <svg className="animate-spin h-4 w-4" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" fill="none" />
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
              </svg>
              {statusText || t('aiInterpret.analyzing')}
            </>
          ) : (
            <>
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
              {t('aiInterpret.button')}
            </>
          )}
        </button>
      </div>

      {/* 进度条 */}
      {loading && (
        <div className="mb-4">
          <div className="flex justify-between text-sm text-gray-500 mb-1">
            <span>{statusText}</span>
            <span>{progress}%</span>
          </div>
          <div className="w-full bg-gray-200 rounded-full h-2">
            <div
              className="bg-gradient-to-r from-purple-500 to-indigo-600 h-2 rounded-full transition-all duration-500"
              style={{ width: `${Math.max(progress, 5)}%` }}
            />
          </div>
          <p className="text-xs text-gray-400 mt-2">
            {t('aiInterpret.hint')}
          </p>
        </div>
      )}

      {/* 错误信息 */}
      {error && (
        <div className="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded-lg mb-4">
          <div className="font-medium">{t('aiInterpret.error')}</div>
          <div className="text-sm mt-1">{error}</div>
        </div>
      )}

      {/* AI 解读结果 */}
      {result && expanded && (
        <div className="prose prose-sm max-w-none border-t pt-4">
          <ReactMarkdown>{result}</ReactMarkdown>
        </div>
      )}

      {/* 空状态提示 */}
      {!loading && !result && !error && (
        <div className="text-center text-gray-400 py-4 text-sm">
          {t('aiInterpret.emptyHint')}
        </div>
      )}
    </div>
  )
}

export default AIMarketInterpret
