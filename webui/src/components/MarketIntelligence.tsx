import React, { useEffect, useState } from 'react'
import { getMarketIntelligence, MarketIntelligenceResponse } from '../services/api'

const MarketIntelligence: React.FC = () => {
  const [data, setData] = useState<MarketIntelligenceResponse>({
    rss_feeds: [],
    fear_greed: null,
    reddit_posts: [],
    polymarket: [],
  })
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [selectedSource, setSelectedSource] = useState<string>('')
  const [activeTab, setActiveTab] = useState<'rss' | 'fear_greed' | 'reddit' | 'polymarket' | 'all'>('all')

  // 获取市场情报数据
  const fetchData = async () => {
    try {
      setLoading(true)
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
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : '获取市场情报失败')
      console.error('Failed to fetch market intelligence:', err)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
    // 每5分钟刷新一次
    const interval = setInterval(fetchData, 5 * 60 * 1000)
    return () => clearInterval(interval)
  }, [searchKeyword, selectedSource])

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    fetchData()
  }

  const getFearGreedColor = (value: number) => {
    if (value >= 75) return '#ef4444' // 极度贪婪 - 红色
    if (value >= 55) return '#f59e0b' // 贪婪 - 橙色
    if (value >= 45) return '#6b7280' // 中性 - 灰色
    if (value >= 25) return '#3b82f6' // 恐惧 - 蓝色
    return '#1d4ed8' // 极度恐惧 - 深蓝色
  }

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleString('zh-CN')
  }

  return (
    <div style={{ padding: '20px' }}>
      <h2>市场情报</h2>

      {/* 搜索栏 */}
      <div style={{ marginBottom: '20px', display: 'flex', gap: '10px', alignItems: 'center', flexWrap: 'wrap' }}>
        <form onSubmit={handleSearch} style={{ display: 'flex', gap: '10px', flex: 1, minWidth: '300px' }}>
          <input
            type="text"
            value={searchKeyword}
            onChange={(e) => setSearchKeyword(e.target.value)}
            placeholder="搜索关键词..."
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
            搜索
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
          <option value="all">全部数据源</option>
          <option value="rss">RSS新闻</option>
          <option value="fear_greed">恐慌贪婪指数</option>
          <option value="reddit">Reddit</option>
          <option value="polymarket">Polymarket</option>
        </select>
      </div>

      {error && (
        <div style={{ padding: '10px', marginBottom: '20px', backgroundColor: '#fee', color: '#c33', borderRadius: '4px' }}>
          错误: {error}
        </div>
      )}

      {loading && data.rss_feeds.length === 0 && !data.fear_greed && data.reddit_posts.length === 0 && data.polymarket.length === 0 ? (
        <div style={{ padding: '40px', textAlign: 'center' }}>
          <p>加载中...</p>
        </div>
      ) : (
        <>
          {/* 标签页 */}
          <div style={{ marginBottom: '20px', display: 'flex', gap: '8px', borderBottom: '2px solid #e5e7eb' }}>
            {(['all', 'rss', 'fear_greed', 'reddit', 'polymarket'] as const).map((tab) => (
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
                {tab === 'all' ? '全部' : tab === 'rss' ? 'RSS新闻' : tab === 'fear_greed' ? '恐慌贪婪' : tab === 'reddit' ? 'Reddit' : 'Polymarket'}
              </button>
            ))}
          </div>

          {/* RSS新闻 */}
          {(activeTab === 'all' || activeTab === 'rss') && (
            <div style={{ marginBottom: '40px' }}>
              <h3>RSS新闻</h3>
              {data.rss_feeds.length === 0 ? (
                <p style={{ color: '#6b7280', padding: '20px', textAlign: 'center' }}>暂无RSS新闻</p>
              ) : (
                data.rss_feeds.map((feed, feedIndex) => (
                  <div key={feedIndex} style={{ marginBottom: '30px' }}>
                    <h4 style={{ marginBottom: '10px', color: '#1f2937' }}>
                      {feed.title}
                      <span style={{ marginLeft: '10px', fontSize: '12px', color: '#6b7280', fontWeight: 'normal' }}>
                        ({feed.items.length} 条)
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
                            <span>来源: {item.source}</span>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                ))
              )}
            </div>
          )}

          {/* 恐慌贪婪指数 */}
          {(activeTab === 'all' || activeTab === 'fear_greed') && (
            <div style={{ marginBottom: '40px' }}>
              <h3>恐慌贪婪指数</h3>
              {!data.fear_greed ? (
                <p style={{ color: '#6b7280', padding: '20px', textAlign: 'center' }}>暂无数据</p>
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
              <h3>Reddit热门帖子</h3>
              {data.reddit_posts.length === 0 ? (
                <p style={{ color: '#6b7280', padding: '20px', textAlign: 'center' }}>暂无Reddit帖子</p>
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
                          <div style={{ fontSize: '12px', color: '#9ca3af' }}>分数</div>
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
                            <span>作者: {post.author}</span>
                            <span>赞同率: {(post.upvote_ratio * 100).toFixed(0)}%</span>
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
              <h3>Polymarket预测市场</h3>
              {data.polymarket.length === 0 ? (
                <p style={{ color: '#6b7280', padding: '20px', textAlign: 'center' }}>暂无Polymarket市场</p>
              ) : (
                <div style={{ backgroundColor: '#fff', borderRadius: '8px', overflow: 'hidden' }}>
                  <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                    <thead>
                      <tr style={{ backgroundColor: '#f3f4f6' }}>
                        <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>问题</th>
                        <th style={{ padding: '12px', textAlign: 'right', borderBottom: '2px solid #e5e7eb' }}>交易量</th>
                        <th style={{ padding: '12px', textAlign: 'right', borderBottom: '2px solid #e5e7eb' }}>流动性</th>
                        <th style={{ padding: '12px', textAlign: 'left', borderBottom: '2px solid #e5e7eb' }}>结束时间</th>
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
                                选项: {market.outcomes.join(', ')}
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
        </>
      )}
    </div>
  )
}

export default MarketIntelligence

