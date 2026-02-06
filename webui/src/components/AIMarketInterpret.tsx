import React, { useState, useCallback, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import ReactMarkdown from 'react-markdown'
import {
  createMarketInterpretTask,
  pollMarketInterpretUntilComplete,
  getLatestMarketInterpret,
  listMarketInterpretHistory,
  MarketInterpretRequest,
  MarketInterpretHistoryItem,
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
  const [historyItems, setHistoryItems] = useState<MarketInterpretHistoryItem[]>([])
  const [historyExpanded, setHistoryExpanded] = useState(false)
  const [selectedHistoryId, setSelectedHistoryId] = useState<string | null>(null)
  const [restored, setRestored] = useState(false)

  const resumePolling = useCallback((taskId: string) => {
    setStatusText(t('aiInterpret.analyzing'))
    pollMarketInterpretUntilComplete(
      taskId,
      (prog, status) => {
        setProgress(prog)
        if (status === 'running') {
          setStatusText(t('aiInterpret.analyzing'))
        } else if (status === 'pending') {
          setStatusText(t('aiInterpret.pending'))
        }
      }
    )
      .then((analysisResult) => {
        setResult(analysisResult)
        setStatusText('')
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : t('aiInterpret.unknownError'))
        setStatusText('')
      })
      .finally(() => {
        setLoading(false)
        setProgress(100)
      })
  }, [t])

  const handleAnalyze = useCallback(async () => {
    setLoading(true)
    setError(null)
    setResult(null)
    setSelectedHistoryId(null)
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

      const taskResp = await createMarketInterpretTask(request)
      setStatusText(t('aiInterpret.analyzing'))

      await pollMarketInterpretUntilComplete(
        taskResp.task_id,
        (prog, status) => {
          setProgress(prog)
          if (status === 'running') {
            setStatusText(t('aiInterpret.analyzing'))
          } else if (status === 'pending') {
            setStatusText(t('aiInterpret.pending'))
          }
        }
      ).then((analysisResult) => {
        setResult(analysisResult)
        setStatusText('')
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : t('aiInterpret.unknownError'))
      setStatusText('')
    } finally {
      setLoading(false)
      setProgress(100)
    }
  }, [pageType, symbol, getPageData, t])

  // 挂载时恢复当前页面类型下「最新一条」解读（进行中或已完成）
  useEffect(() => {
    if (restored) return
    setRestored(true)
    getLatestMarketInterpret(pageType)
      .then((latest) => {
        if (!latest || !latest.task_id) return
        if (latest.status === 'completed' && latest.result) {
          setResult(latest.result)
          setExpanded(true)
          return
        }
        if (latest.status === 'failed' && latest.error) {
          setError(latest.error)
          return
        }
        if (latest.status === 'pending' || latest.status === 'running') {
          setLoading(true)
          setProgress(latest.progress || 0)
          setExpanded(true)
          resumePolling(latest.task_id)
        }
      })
      .catch(() => {})
  }, [pageType, restored, resumePolling])

  const loadHistory = useCallback(() => {
    if (!historyExpanded) {
      setHistoryExpanded(true)
      listMarketInterpretHistory(pageType, 20)
        .then((res) => setHistoryItems(res.items || []))
        .catch(() => setHistoryItems([]))
    }
  }, [pageType, historyExpanded])

  const showHistoryItem = (item: MarketInterpretHistoryItem) => {
    setSelectedHistoryId(item.task_id)
    if (item.status === 'completed' && item.result) {
      setResult(item.result)
      setError(null)
      setExpanded(true)
    } else if (item.status === 'failed' && item.error) {
      setError(item.error)
      setResult(null)
    } else {
      setResult(null)
      setError(null)
    }
  }

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

      {/* 历史解读列表 */}
      <div className="mt-4 border-t pt-4">
        <button
          type="button"
          onClick={loadHistory}
          className="text-sm text-gray-500 hover:text-gray-700 flex items-center gap-1"
        >
          {historyExpanded ? '▼' : '▶'} {t('aiInterpret.historyTitle')}
        </button>
        {historyExpanded && (
          <ul className="mt-2 space-y-1 max-h-48 overflow-y-auto">
            {historyItems.length === 0 && (
              <li className="text-sm text-gray-400">{t('aiInterpret.noHistory')}</li>
            )}
            {historyItems.map((item) => (
              <li key={item.task_id}>
                <button
                  type="button"
                  onClick={() => showHistoryItem(item)}
                  className={`text-left w-full text-sm px-2 py-1.5 rounded truncate ${
                    selectedHistoryId === item.task_id
                      ? 'bg-purple-100 text-purple-800'
                      : 'hover:bg-gray-100 text-gray-700'
                  }`}
                >
                  <span className="text-gray-500">
                    {new Date(item.created_at).toLocaleString()}
                  </span>
                  {' · '}
                  <span>{item.symbol}</span>
                  {' · '}
                  <span className={item.status === 'completed' ? 'text-green-600' : item.status === 'failed' ? 'text-red-600' : 'text-gray-500'}>
                    {t(`aiInterpret.status.${item.status}`)}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

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
