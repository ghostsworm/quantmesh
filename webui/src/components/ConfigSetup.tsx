import React, { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { saveInitialConfig, SetupInitRequest } from '../services/setup'

const ConfigSetup: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [formData, setFormData] = useState<SetupInitRequest>({
    exchange: 'bitget',
    api_key: '',
    secret_key: '',
    passphrase: '',
    symbol: 'ETHUSDT',
    price_interval: 2,
    order_quantity: 30,
    min_order_value: 20,
    buy_window_size: 10,
    sell_window_size: 10,
    testnet: false,
    fee_rate: 0.0002,
  })
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)

  // 需要 passphrase 的交易所
  const exchangesRequiringPassphrase = ['bitget', 'okx', 'kucoin']

  const handleChange = (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => {
    const { name, value, type } = e.target
    const checked = (e.target as HTMLInputElement).checked

    setFormData(prev => ({
      ...prev,
      [name]: type === 'checkbox' ? checked : (name === 'price_interval' || name === 'order_quantity' || name === 'min_order_value' || name === 'fee_rate' ? parseFloat(value) || 0 : (name === 'buy_window_size' || name === 'sell_window_size' ? parseInt(value) || 0 : value)),
    }))
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setSuccess(false)

    // 验证必填字段
    if (!formData.exchange) {
      setError(t('configSetup.selectExchange'))
      return
    }
    if (!formData.api_key.trim()) {
      setError(t('configSetup.enterApiKey'))
      return
    }
    if (!formData.secret_key.trim()) {
      setError(t('configSetup.enterSecretKey'))
      return
    }
    if (exchangesRequiringPassphrase.includes(formData.exchange) && !formData.passphrase?.trim()) {
      setError(t('configSetup.exchangeRequiresPassphrase'))
      return
    }
    if (!formData.symbol.trim()) {
      setError(t('configSetup.enterSymbol'))
      return
    }
    if (formData.price_interval <= 0) {
      setError(t('configSetup.priceIntervalMustBeGreaterThanZero'))
      return
    }
    if (formData.order_quantity <= 0) {
      setError(t('configSetup.orderAmountMustBeGreaterThanZero'))
      return
    }
    if (formData.buy_window_size <= 0) {
      setError(t('configSetup.buyWindowSizeMustBeGreaterThanZero'))
      return
    }

    setIsLoading(true)

    try {
      const response = await saveInitialConfig(formData)
      if (response.success) {
        setSuccess(true)
        // 3秒后刷新页面
        setTimeout(() => {
          window.location.reload()
        }, 3000)
      } else {
        setError(response.message || t('configSetup.saveFailed'))
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t('configSetup.saveFailed'))
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <div style={{ 
      display: 'flex', 
      justifyContent: 'center', 
      alignItems: 'center', 
      minHeight: '100vh',
      backgroundColor: '#f5f5f5',
      padding: '20px'
    }}>
      <div style={{
        backgroundColor: 'white',
        padding: '40px',
        borderRadius: '8px',
        boxShadow: '0 2px 8px rgba(0,0,0,0.1)',
        width: '100%',
        maxWidth: '600px'
      }}>
        <h2 style={{ textAlign: 'center', marginBottom: '30px', color: '#1890ff' }}>
          {t('configSetup.title')}
        </h2>
        <p style={{ textAlign: 'center', marginBottom: '30px', color: '#8c8c8c', fontSize: '14px' }}>
          {t('configSetup.subtitle')}
        </p>

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

        {success && (
          <div style={{
            padding: '12px',
            backgroundColor: '#f6ffed',
            border: '1px solid #b7eb8f',
            borderRadius: '4px',
            color: '#52c41a',
            marginBottom: '20px'
          }}>
            {t('configSetup.saveSuccess')}
          </div>
        )}

        <form onSubmit={handleSubmit}>
          {/* 交易所配置 */}
          <div style={{ marginBottom: '24px' }}>
            <h3 style={{ marginBottom: '16px', fontSize: '16px', fontWeight: 'bold' }}>
              {t('configSetup.exchangeConfig')}
            </h3>
            
            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: '500' }}>
                {t('configSetup.exchange')} <span style={{ color: '#ff4d4f' }}>{t('configSetup.required')}</span>
              </label>
              <select
                name="exchange"
                value={formData.exchange}
                onChange={handleChange}
                disabled={isLoading}
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
              >
                <option value="binance">Binance</option>
                <option value="bitget">Bitget</option>
                <option value="bybit">Bybit</option>
                <option value="gate">Gate.io</option>
                <option value="okx">OKX</option>
                <option value="huobi">Huobi (HTX)</option>
                <option value="kucoin">KuCoin</option>
                <option value="kraken">Kraken</option>
                <option value="bitfinex">Bitfinex</option>
                <option value="mexc">MEXC</option>
                <option value="bingx">BingX</option>
                <option value="deribit">Deribit</option>
                <option value="bitmex">BitMEX</option>
                <option value="phemex">Phemex</option>
                <option value="woox">WOO X</option>
                <option value="coinex">CoinEx</option>
                <option value="bitrue">Bitrue</option>
                <option value="xtcom">XT.COM</option>
                <option value="btcc">BTCC</option>
                <option value="ascendex">AscendEX</option>
                <option value="poloniex">Poloniex</option>
                <option value="cryptocom">Crypto.com</option>
              </select>
            </div>

            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: '500' }}>
                {t('configSetup.apiKey')} <span style={{ color: '#ff4d4f' }}>{t('configSetup.required')}</span>
              </label>
              <input
                type="text"
                name="api_key"
                value={formData.api_key}
                onChange={handleChange}
                disabled={isLoading}
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
                placeholder={t('configSetup.apiKeyPlaceholder')}
              />
            </div>

            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: '500' }}>
                {t('configSetup.secretKey')} <span style={{ color: '#ff4d4f' }}>{t('configSetup.required')}</span>
              </label>
              <input
                type="password"
                name="secret_key"
                value={formData.secret_key}
                onChange={handleChange}
                disabled={isLoading}
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
                placeholder={t('configSetup.secretKeyPlaceholder')}
              />
            </div>

            {exchangesRequiringPassphrase.includes(formData.exchange) && (
              <div style={{ marginBottom: '16px' }}>
                <label style={{ display: 'block', marginBottom: '8px', fontWeight: '500' }}>
                  {t('configSetup.passphrase')} <span style={{ color: '#ff4d4f' }}>{t('configSetup.required')}</span>
                </label>
                <input
                  type="password"
                  name="passphrase"
                  value={formData.passphrase}
                  onChange={handleChange}
                  disabled={isLoading}
                  style={{
                    width: '100%',
                    padding: '12px',
                    border: '1px solid #d9d9d9',
                    borderRadius: '4px',
                    fontSize: '14px'
                  }}
                  placeholder={t('configSetup.passphrasePlaceholder')}
                />
              </div>
            )}

            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  name="testnet"
                  checked={formData.testnet}
                  onChange={handleChange}
                  disabled={isLoading}
                  style={{ marginRight: '8px' }}
                />
                <span style={{ fontSize: '14px' }}>{t('configSetup.useTestnet')}</span>
              </label>
            </div>

            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: '500' }}>
                {t('configSetup.feeRateOptional')}
              </label>
              <input
                type="number"
                name="fee_rate"
                value={formData.fee_rate}
                onChange={handleChange}
                disabled={isLoading}
                step="0.0001"
                min="0"
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
                placeholder={t('configSetup.feeRatePlaceholder')}
              />
              <div style={{ marginTop: '4px', fontSize: '12px', color: '#8c8c8c' }}>
                {t('configSetup.feeRateExample')}
              </div>
            </div>
          </div>

          {/* 交易配置 */}
          <div style={{ marginBottom: '24px' }}>
            <h3 style={{ marginBottom: '16px', fontSize: '16px', fontWeight: 'bold' }}>
              {t('configSetup.tradingConfig')}
            </h3>

            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: '500' }}>
                {t('configSetup.symbol')} <span style={{ color: '#ff4d4f' }}>{t('configSetup.required')}</span>
              </label>
              <input
                type="text"
                name="symbol"
                value={formData.symbol}
                onChange={handleChange}
                disabled={isLoading}
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
                placeholder={t('configSetup.symbolPlaceholder')}
              />
            </div>

            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: '500' }}>
                {t('configSetup.priceInterval')} <span style={{ color: '#ff4d4f' }}>{t('configSetup.required')}</span>
              </label>
              <input
                type="number"
                name="price_interval"
                value={formData.price_interval}
                onChange={handleChange}
                disabled={isLoading}
                step="0.01"
                min="0.01"
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
                placeholder={t('configSetup.priceIntervalPlaceholder')}
              />
              <div style={{ marginTop: '4px', fontSize: '12px', color: '#8c8c8c' }}>
                {t('configSetup.priceIntervalSuggestion')}
              </div>
            </div>

            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: '500' }}>
                {t('configSetup.orderAmount')} <span style={{ color: '#ff4d4f' }}>{t('configSetup.required')}</span>
              </label>
              <input
                type="number"
                name="order_quantity"
                value={formData.order_quantity}
                onChange={handleChange}
                disabled={isLoading}
                step="1"
                min="1"
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
                placeholder={t('configSetup.orderAmountPlaceholder')}
              />
              <div style={{ marginTop: '4px', fontSize: '12px', color: '#8c8c8c' }}>
                {t('configSetup.orderAmountDesc')}
              </div>
            </div>

            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: '500' }}>
                {t('configSetup.minOrderValue')}
              </label>
              <input
                type="number"
                name="min_order_value"
                value={formData.min_order_value}
                onChange={handleChange}
                disabled={isLoading}
                step="1"
                min="1"
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
                placeholder={t('configSetup.minOrderValuePlaceholder')}
              />
            </div>

            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: '500' }}>
                {t('configSetup.buyWindowSize')} <span style={{ color: '#ff4d4f' }}>{t('configSetup.required')}</span>
              </label>
              <input
                type="number"
                name="buy_window_size"
                value={formData.buy_window_size}
                onChange={handleChange}
                disabled={isLoading}
                step="1"
                min="1"
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
                placeholder={t('configSetup.buyWindowSizePlaceholder')}
              />
              <div style={{ marginTop: '4px', fontSize: '12px', color: '#8c8c8c' }}>
                {t('configSetup.buyWindowSizeDesc')}
              </div>
            </div>

            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: '500' }}>
                {t('configSetup.sellWindowSize')}
              </label>
              <input
                type="number"
                name="sell_window_size"
                value={formData.sell_window_size}
                onChange={handleChange}
                disabled={isLoading}
                step="1"
                min="1"
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
                placeholder={t('configSetup.sellWindowSizePlaceholder')}
              />
              <div style={{ marginTop: '4px', fontSize: '12px', color: '#8c8c8c' }}>
                {t('configSetup.sellWindowSizeDesc')}
              </div>
            </div>
          </div>

          <button
            type="submit"
            disabled={isLoading || success}
            style={{
              width: '100%',
              padding: '12px',
              backgroundColor: success ? '#52c41a' : '#1890ff',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              fontSize: '16px',
              cursor: (isLoading || success) ? 'not-allowed' : 'pointer',
              opacity: (isLoading || success) ? 0.6 : 1
            }}
          >
            {isLoading ? t('configSetup.saving') : success ? t('configSetup.saved') : t('configSetup.saveConfig')}
          </button>
        </form>
      </div>
    </div>
  )
}

export default ConfigSetup

