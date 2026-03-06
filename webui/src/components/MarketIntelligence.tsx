import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  getMarketIntelligence,
  MarketIntelligenceResponse,
  getMacroEvents,
  getMacroImpact,
  MacroEventsResponse,
  MacroImpactResponse,
} from '../services/api'
import { useConfig } from '../contexts/ConfigContext'
import { formatDateTime } from '../utils/dateFormat'

const MarketIntelligence: React.FC = () => {
  const { t, i18n } = useTranslation()
  const { timezone } = useConfig()
  const [data, setData] = useState<MarketIntelligenceResponse>({
    rss_feeds: [],
    fear_greed: null,
    reddit_posts: [],
    polymarket: [],
  })
  const [macroEvents, setMacroEvents] = useState<MacroEventsResponse | null>(null)
  const [macroImpact, setMacroImpact] = useState<MacroImpactResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [isEmptyData, setIsEmptyData] = useState(false)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [selectedSource, setSelectedSource] = useState<string>('')
  const [activeTab, setActiveTab] = useState<'rss' | 'fear_greed' | 'reddit' | 'polymarket' | 'macro' | 'all'>('all')

  // 獲取市場情报數據
  const fetchData = async () => {
    try {
      setLoading(true)
      setError(null)
      setIsEmptyData(false)
      const params: any = {
        limit: 50,
      }
      if (searchKeyword) {
        params.keyword = searchKeyword
      }
      if (selectedSource && selectedSource !== 'all') {
        params.source = selectedSource
      }
      const response = await getMarketIntelligence(params)
      setData(response)
      
      // 检查數據是否為空
      const isEmpty = (
        response.rss_feeds.length === 0 &&
        response.fear_greed === null &&
        response.reddit_posts.length === 0 &&
        response.polymarket.length === 0
      )
      
      if (isEmpty) {
        setIsEmptyData(true)
        setError(t('marketIntelligence.noDataHint'))
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : t('marketIntelligence.fetchFailed')
      setError(t('marketIntelligence.fetchError', { error: errorMessage }))
      setIsEmptyData(false)
      console.error('Failed to fetch market intelligence:', err)
    } finally {
      setLoading(false)
    }
  }

  const fetchMacroData = async () => {
    try {
      const [eventsRes, impactRes] = await Promise.all([
        getMacroEvents().catch(() => ({ events: [], last_fetched: null, enabled: false })),
        getMacroImpact().catch(() => ({ composite_risk_score: 0, event_count: 0, high_impact_count: 0, assessments: [], last_fetched: null, enabled: false })),
      ])
      setMacroEvents(eventsRes)
      setMacroImpact(impactRes)
    } catch {
      setMacroEvents(null)
      setMacroImpact(null)
    }
  }

  useEffect(() => {
    fetchData()
    fetchMacroData()
    const interval = setInterval(fetchData, 10 * 60 * 1000)
    const macroInterval = setInterval(fetchMacroData, 5 * 60 * 1000)
    return () => {
      clearInterval(interval)
      clearInterval(macroInterval)
    }
  }, [searchKeyword, selectedSource])

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    fetchData()
  }

  const getFearGreedColor = (value: number) => {
    if (value >= 75) return '#ef4444'
    if (value >= 55) return '#f59e0b'
    if (value >= 45) return '#6b7280'
    if (value >= 25) return '#3b82f6'
    return '#1d4ed8'
  }

  const formatDate = (dateStr: string) => {
    return formatDateTime(dateStr, timezone, i18n.language)
  }

  return (
    <div style={{ padding: '20px' }}>
      <h2>{t('marketIntelligence.title')}</h2>

      {/* 搜索欄 */}
      <div style={{ marginBottom: '20px', display: 'flex', gap: '10px', alignItems: 'center', flexWrap: 'wrap' }}>
        <form onSubmit={handleSearch} style={{ display: 'flex', gap: '10px', flex: 1, minWidth: '300px' }}>
          <input
            type="text"
            value={searchKeyword}
            onChange={(e) => setSearchKeyword(e.target.value)}
            placeholder={t('marketIntelligence.searchPlaceholder')}
            style={{
              flex: 1,
              padding: '8px 12px',
              borderRadius: '6px',
              border: '1px solid #d1d5db',
              fontSize: '14px',
            }}
          />
          <button
            type="submit"
            style={{
              padding: '8px 16px',
              backgroundColor: '#3b82f6',
              color: 'white',
              border: 'none',
              borderRadius: '6px',
              cursor: 'pointer',
              fontSize: '14px',
            }}
          >
            {t('marketIntelligence.search')}
          </button>
        </form>
        <select
          value={selectedSource}
          onChange={(e) => setSelectedSource(e.target.value)}
          style={{
            padding: '8px 12px',
            borderRadius: '6px',
            border: '1px solid #d1d5db',
            fontSize: '14px',
          }}
        >
          <option value="all">{t('marketIntelligence.allSources')}</option>
          <option value="rss">{t('marketIntelligence.rssNews')}</option>
          <option value="fear_greed">{t('marketIntelligence.fearGreedIndex')}</option>
          <option value="reddit">Reddit</option>
          <option value="polymarket">Polymarket</option>
        </select>
        <button
          onClick={fetchData}
          disabled={loading}
          style={{
            padding: '8px 16px',
            backgroundColor: loading ? '#9ca3af' : '#10b981',
            color: 'white',
            border: 'none',
            borderRadius: '6px',
            cursor: loading ? 'not-allowed' : 'pointer',
            fontSize: '14px',
            display: 'flex',
            alignItems: 'center',
            gap: '6px',
          }}
          title={t('marketIntelligence.refreshTitle')}
        >
          <span style={{ fontSize: '16px' }}>🔄</span>
          {loading ? t('marketIntelligence.refreshing') : t('marketIntelligence.refresh')}
        </button>
      </div>

      {error && (
        <div style={{ 
          padding: '12px 16px', 
          marginBottom: '20px', 
          backgroundColor: isEmptyData ? '#fef3c7' : '#fee', 
          color: isEmptyData ? '#92400e' : '#c33', 
          borderRadius: '6px',
          border: `1px solid ${isEmptyData ? '#fbbf24' : '#f87171'}`,
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
        }}>
          <span style={{ fontSize: '18px' }}>{isEmptyData ? '⚠️' : '❌'}</span>
          <span>{error}</span>
        </div>
      )}

      {loading && data.rss_feeds.length === 0 && !data.fear_greed && data.reddit_posts.length === 0 && data.polymarket.length === 0 ? (
        <div style={{ padding: '40px', textAlign: 'center' }}>
          <p>{t('marketIntelligence.loading')}</p>
        </div>
      ) : (
        <>
          {/* 標签页 */}
          <div style={{ marginBottom: '20px', display: 'flex', gap: '8px', borderBottom: '2px solid #e5e7eb', flexWrap: 'wrap' }}>
            {(['all', 'rss', 'fear_greed', 'reddit', 'polymarket', 'macro'] as const).map((tab) => (
              <button
                key={tab}
                onClick={() => setActiveTab(tab)}
                style={{
                  padding: '10px 16px',
                  border: 'none',
                  backgroundColor: activeTab === tab ? '#3b82f6' : 'transparent',
                  color: activeTab === tab ? 'white' : '#6b7280',
                  cursor: 'pointer',
                  borderBottom: activeTab === tab ? '2px solid #3b82f6' : '2px solid transparent',
                  marginBottom: '-2px',
                  fontSize: '14px',
                  fontWeight: activeTab === tab ? '600' : '400',
                }}
              >
                {tab === 'all' ? t('marketIntelligence.tabAll') : tab === 'rss' ? t('marketIntelligence.rssNews') : tab === 'fear_greed' ? t('marketIntelligence.tabFearGreed') : tab === 'reddit' ? 'Reddit' : tab === 'polymarket' ? 'Polymarket' : t('marketIntelligence.tabMacro')}
              </button>
            ))}
          </div>

          {/* RSS新闻 */}
          {(activeTab === 'all' || activeTab === 'rss') && (
            <div style={{ marginBottom: '40px' }}>
              <h3>{t('marketIntelligence.rssNews')}</h3>
              {data.rss_feeds.length === 0 ? (
                <p style={{ color: '#6b7280', padding: '20px', textAlign: 'center' }}>{t('marketIntelligence.noRssNews')}</p>
              ) : (
                data.rss_feeds.map((feed, feedIndex) => (
                  <div key={feedIndex} style={{ marginBottom: '30px' }}>
                    <h4 style={{ marginBottom: '10px', color: '#1f2937' }}>
                      {feed.title}
                      <span style={{ marginLeft: '10px', fontSize: '12px', color: '#6b7280', fontWeight: 'normal' }}>
                        ({t('marketIntelligence.itemCount', { count: feed.items.length })})
                      </span>
                    </h4>
                    <div style={{ backgroundColor: '#fff', borderRadius: '8px', overflow: 'hidden' }}>
                      {feed.items.map((item, itemIndex) => (
                        <div
                          key={itemIndex}
                          style={{
                            padding: '16px',
                            borderBottom: itemIndex < feed.items.length - 1 ? '1px solid #e5e7eb' : 'none',
                          }}
                        >
                          <a
                            href={item.link}
                            target="_blank"
                            rel="noopener noreferrer"
                            style={{
                              fontSize: '16px',
                              fontWeight: '600',
                              color: '#3b82f6',
                              textDecoration: 'none',
                              display: 'block',
                              marginBottom: '8px',
                            }}
                          >
                            {item.title}
                          </a>
                          <p style={{ color: '#6b7280', fontSize: '14px', marginBottom: '8px', lineHeight: '1.5' }}>
                            {item.description.length > 200 ? item.description.substring(0, 200) + '...' : item.description}
                          </p>
                          <div style={{ display: 'flex', gap: '12px', fontSize: '12px', color: '#9ca3af' }}>
                            <span>{formatDate(item.pub_date)}</span>
                            <span>{t('marketIntelligence.source')}: {item.source}</span>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                ))
              )}
            </div>
          )}

          {/* 恐慌贪婪指數 */}
          {(activeTab === 'all' || activeTab === 'fear_greed') && (
            <div style={{ marginBottom: '40px' }}>
              <h3>{t('marketIntelligence.fearGreedIndex')}</h3>
              {!data.fear_greed ? (
                <p style={{ color: '#6b7280', padding: '20px', textAlign: 'center' }}>{t('marketIntelligence.noFearGreedData')}</p>
              ) : (
                <div
                  style={{
                    backgroundColor: '#fff',
                    borderRadius: '8px',
                    padding: '24px',
                    border: `2px solid ${getFearGreedColor(data.fear_greed.value)}`,
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '16px' }}>
                    <div>
                      <div style={{ fontSize: '32px', fontWeight: 'bold', color: getFearGreedColor(data.fear_greed.value) }}>
                        {data.fear_greed.value}
                      </div>
                      <div style={{ fontSize: '18px', color: '#6b7280', marginTop: '4px' }}>
                        {data.fear_greed.classification}
                      </div>
                    </div>
                    <div style={{ fontSize: '14px', color: '#9ca3af' }}>
                      {formatDate(data.fear_greed.timestamp)}
                    </div>
                  </div>
                  <div style={{ height: '8px', backgroundColor: '#e5e7eb', borderRadius: '4px', overflow: 'hidden' }}>
                    <div
                      style={{
                        height: '100%',
                        width: `${data.fear_greed.value}%`,
                        backgroundColor: getFearGreedColor(data.fear_greed.value),
                        transition: 'width 0.3s',
                      }}
                    />
                  </div>
                </div>
              )}
            </div>
          )}

          {/* Reddit帖子 */}
          {(activeTab === 'all' || activeTab === 'reddit') && (
            <div style={{ marginBottom: '40px' }}>
              <h3>{t('marketIntelligence.redditHotPosts')}</h3>
              {data.reddit_posts.length === 0 ? (
                <p style={{ color: '#6b7280', padding: '20px', textAlign: 'center' }}>{t('marketIntelligence.noRedditPosts')}</p>
              ) : (
                <div style={{ backgroundColor: '#fff', borderRadius: '8px', overflow: 'hidden' }}>
                  {data.reddit_posts.map((post, index) => (
                    <div
                      key={index}
                      style={{
                        padding: '16px',
                        borderBottom: index < data.reddit_posts.length - 1 ? '1px solid #e5e7eb' : 'none',
                      }}
                    >
                      <div style={{ display: 'flex', alignItems: 'start', gap: '12px' }}>
                        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', minWidth: '60px' }}>
                          <div style={{ fontSize: '18px', fontWeight: '600', color: '#3b82f6' }}>
                            {post.score > 0 ? '+' : ''}{post.score}
                          </div>
                          <div style={{ fontSize: '12px', color: '#9ca3af' }}>{t('marketIntelligence.score')}</div>
                        </div>
                        <div style={{ flex: 1 }}>
                          <a
                            href={post.url}
                            target="_blank"
                            rel="noopener noreferrer"
                            style={{
                              fontSize: '16px',
                              fontWeight: '600',
                              color: '#1f2937',
                              textDecoration: 'none',
                              display: 'block',
                              marginBottom: '8px',
                            }}
                          >
                            {post.title}
                          </a>
                          {post.content && (
                            <p style={{ color: '#6b7280', fontSize: '14px', marginBottom: '8px', lineHeight: '1.5' }}>
                              {post.content.length > 300 ? post.content.substring(0, 300) + '...' : post.content}
                            </p>
                          )}
                          <div style={{ display: 'flex', gap: '12px', fontSize: '12px', color: '#9ca3af', flexWrap: 'wrap' }}>
                            <span>r/{post.subreddit}</span>
                            <span>{t('marketIntelligence.author')}: {post.author}</span>
                            <span>{t('marketIntelligence.upvoteRatio')}: {(post.upvote_ratio * 100).toFixed(0)}%</span>
                            <span>{formatDate(post.created_at)}</span>
                          </div>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Polymarket市场 */}
          {(activeTab === 'all' || activeTab === 'polymarket') && (
            <div style={{ marginBottom: '40px' }}>
              <h3>{t('marketIntelligence.polymarketTitle')}</h3>
              {data.polymarket.length === 0 ? (
                <p style={{ color: '#6b7280', padding: '20px', textAlign: 'center' }}>{t('marketIntelligence.noPolymarket')}</p>
              ) : (
                <div style={{ backgroundColor: '#fff', borderRadius: '8px', overflow: 'hidden' }}>
                  <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                    <thead>
                      <tr style={{ backgroundColor: '#f3f4f6' }}>
                        <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>{t('marketIntelligence.question')}</th>
                        <th style={{ padding: '12px', textAlign: 'right', borderBottom: '2px solid #e5e7eb' }}>{t('marketIntelligence.volume')}</th>
                        <th style={{ padding: '12px', textAlign: 'right', borderBottom: '2px solid #e5e7eb' }}>{t('marketIntelligence.liquidity')}</th>
                        <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>{t('marketIntelligence.endDate')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.polymarket.map((market, index) => (
                        <tr key={index} style={{ borderBottom: '1px solid #e5e7eb' }}>
                          <td style={{ padding: '12px' }}>
                            <div style={{ fontWeight: '500', marginBottom: '4px' }}>{market.question}</div>
                            {market.description && (
                              <div style={{ fontSize: '12px', color: '#6b7280' }}>
                                {market.description.length > 100 ? market.description.substring(0, 100) + '...' : market.description}
                              </div>
                            )}
                            {market.outcomes.length > 0 && (
                              <div style={{ fontSize: '12px', color: '#9ca3af', marginTop: '4px' }}>
                                {t('marketIntelligence.options')}: {market.outcomes.join(', ')}
                              </div>
                            )}
                          </td>
                          <td style={{ padding: '12px', textAlign: 'right', color: '#6b7280' }}>
                            ${market.volume.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                          </td>
                          <td style={{ padding: '12px', textAlign: 'right', color: '#6b7280' }}>
                            ${market.liquidity.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                          </td>
                          <td style={{ padding: '12px', fontSize: '12px', color: '#9ca3af' }}>
                            {formatDate(market.end_date)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}

          {/* 宏觀事件預測 */}
          {(activeTab === 'all' || activeTab === 'macro') && (
            <div style={{ marginBottom: '40px' }}>
              <h3>{t('marketIntelligence.macroTitle')}</h3>
              {!macroEvents?.enabled && !macroImpact?.enabled ? (
                <p style={{ color: '#6b7280', padding: '20px', textAlign: 'center' }}>{t('marketIntelligence.macroDisabled')}</p>
              ) : (
                <>
                  {macroImpact != null && macroImpact.enabled && (
                    <div style={{ marginBottom: '20px', padding: '16px', backgroundColor: '#f0fdf4', borderRadius: '8px', border: '1px solid #bbf7d0' }}>
                      <div style={{ display: 'flex', gap: '24px', flexWrap: 'wrap' }}>
                        <span><strong>{t('marketIntelligence.macroRiskScore')}:</strong> {macroImpact.composite_risk_score.toFixed(1)}</span>
                        <span><strong>{t('marketIntelligence.macroEventCount')}:</strong> {macroImpact.event_count}</span>
                        <span><strong>{t('marketIntelligence.macroHighImpact')}:</strong> {macroImpact.high_impact_count}</span>
                        {macroImpact.last_fetched && (
                          <span style={{ fontSize: '12px', color: '#6b7280' }}>{t('marketIntelligence.macroLastFetched')}: {formatDate(macroImpact.last_fetched)}</span>
                        )}
                      </div>
                    </div>
                  )}
                  {macroEvents != null && macroEvents.enabled && macroEvents.events.length > 0 ? (
                    <div style={{ backgroundColor: '#fff', borderRadius: '8px', overflow: 'hidden' }}>
                      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                        <thead>
                          <tr style={{ backgroundColor: '#f3f4f6' }}>
                            <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>{t('marketIntelligence.macroQuestion')}</th>
                            <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>{t('marketIntelligence.macroCategory')}</th>
                            <th style={{ padding: '12px', textAlign: 'right', borderBottom: '2px solid #e5e7eb' }}>{t('marketIntelligence.macroProbability')}</th>
                            <th style={{ padding: '12px', textAlign: 'right', borderBottom: '2px solid #e5e7eb' }}>{t('marketIntelligence.macroDelta')}</th>
                            <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>{t('marketIntelligence.macroLink')}</th>
                          </tr>
                        </thead>
                        <tbody>
                          {macroEvents.events.map((evt) => (
                            <tr key={evt.id} style={{ borderBottom: '1px solid #e5e7eb' }}>
                              <td style={{ padding: '12px' }}>
                                <div style={{ fontWeight: '500' }}>{evt.title}</div>
                              </td>
                              <td style={{ padding: '12px', fontSize: '12px', color: '#6b7280' }}>{evt.category_label || evt.category}</td>
                              <td style={{ padding: '12px', textAlign: 'right' }}>{(evt.probability * 100).toFixed(0)}%</td>
                              <td style={{ padding: '12px', textAlign: 'right', color: evt.probability_delta > 0 ? '#ef4444' : evt.probability_delta < 0 ? '#22c55e' : '#6b7280' }}>
                                {evt.probability_delta > 0 ? '+' : ''}{(evt.probability_delta * 100).toFixed(0)}%
                              </td>
                              <td style={{ padding: '12px' }}>
                                {evt.source_url && (
                                  <a href={evt.source_url} target="_blank" rel="noopener noreferrer" style={{ color: '#3b82f6', fontSize: '12px' }}>
                                    Polymarket
                                  </a>
                                )}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  ) : (
                    <p style={{ color: '#6b7280', padding: '20px', textAlign: 'center' }}>{t('marketIntelligence.macroNoData')}</p>
                  )}
                </>
              )}
            </div>
          )}
        </>
      )}
    </div>
  )
}

export default MarketIntelligence
