import React, { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useToast } from '@chakra-ui/react'
import { saveInitialConfig, SetupInitRequest } from '../services/setup'
import { DEFAULT_APP_TOAST_OPTIONS } from '../constants/appToast'
import {
  CONFIG_SETUP_SYMBOL_ORDER,
  CONFIG_SETUP_SYMBOL_PRESETS,
  getPresetForSymbol,
} from '../config/configSetupSymbolPresets'

const defaultSymbol = 'BTCUSDT'
const defaultPreset = CONFIG_SETUP_SYMBOL_PRESETS[defaultSymbol]

const ConfigSetup: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const toast = useToast()
  const [formData, setFormData] = useState<SetupInitRequest>({
    exchange: 'bitget',
    api_key: '',
    secret_key: '',
    passphrase: '',
    symbol: defaultSymbol,
    price_interval: defaultPreset.price_interval,
    order_quantity: defaultPreset.order_quantity,
    min_order_value: defaultPreset.min_order_value,
    buy_window_size: defaultPreset.buy_window_size,
    sell_window_size: defaultPreset.sell_window_size,
    testnet: false,
    fee_rate: 0.0002,
  })
  const [isLoading, setIsLoading] = useState(false)
  const [success, setSuccess] = useState(false)

  // 需要 passphrase 的交易所
  const exchangesRequiringPassphrase = ['bitget', 'okx', 'kucoin']

  const parseNum = (raw: string): number => {
    const v = parseFloat(raw)
    return Number.isFinite(v) ? v : 0
  }

  const handleChange = (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => {
    const { name, value, type } = e.target
    const checked = (e.target as HTMLInputElement).checked

    setFormData(prev => ({
      ...prev,
      [name]: type === 'checkbox'
        ? checked
        : name === 'price_interval' || name === 'order_quantity' || name === 'min_order_value' || name === 'fee_rate'
          ? parseNum(value)
          : name === 'buy_window_size' || name === 'sell_window_size'
            ? parseInt(value, 10) || 0
            : value,
    }))
  }

  const handleSymbolChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const sym = e.target.value
    const preset = getPresetForSymbol(sym)
    setFormData(prev => ({
      ...prev,
      symbol: sym,
      ...(preset
        ? {
            price_interval: preset.price_interval,
            order_quantity: preset.order_quantity,
            min_order_value: preset.min_order_value,
            buy_window_size: preset.buy_window_size,
            sell_window_size: preset.sell_window_size,
          }
        : {}),
    }))
  }

  const handleSkip = () => {
    // 標記本次登錄已跳過配置
    sessionStorage.setItem('config_setup_skipped', 'true')
    // 跳轉到主页
    navigate('/')
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSuccess(false)

    const showValidationToast = (title: string) => {
      toast({
        ...DEFAULT_APP_TOAST_OPTIONS,
        title,
        status: 'warning',
        duration: 4000,
      })
    }

    // 驗证必填字段 - 允許只配置一個交易所
    if (!formData.exchange) {
      showValidationToast(t('configSetup.selectExchange'))
      return
    }
    if (!formData.api_key.trim()) {
      showValidationToast(t('configSetup.enterApiKey'))
      return
    }
    if (!formData.secret_key.trim()) {
      showValidationToast(t('configSetup.enterSecretKey'))
      return
    }
    if (exchangesRequiringPassphrase.includes(formData.exchange) && !formData.passphrase?.trim()) {
      showValidationToast(t('configSetup.exchangeRequiresPassphrase'))
      return
    }
    if (!formData.symbol.trim()) {
      showValidationToast(t('configSetup.enterSymbol'))
      return
    }
    if (formData.price_interval <= 0) {
      showValidationToast(t('configSetup.priceIntervalMustBeGreaterThanZero'))
      return
    }
    if (formData.order_quantity <= 0) {
      showValidationToast(t('configSetup.orderAmountMustBeGreaterThanZero'))
      return
    }
    if (formData.buy_window_size <= 0) {
      showValidationToast(t('configSetup.buyWindowSizeMustBeGreaterThanZero'))
      return
    }

    setIsLoading(true)

    try {
      const response = await saveInitialConfig(formData)
      if (response.success) {
        setSuccess(true)
        sessionStorage.removeItem('config_setup_skipped')
        toast({
          ...DEFAULT_APP_TOAST_OPTIONS,
          title: t('configSetup.saved'),
          description: t('configSetup.saveSuccess'),
          status: 'success',
          duration: 5000,
        })
        setTimeout(() => {
          window.location.reload()
        }, 3000)
      } else {
        toast({
          ...DEFAULT_APP_TOAST_OPTIONS,
          title: t('configSetup.saveFailed'),
          description: response.message || undefined,
          status: 'error',
          duration: 6000,
        })
      }
    } catch (err) {
      toast({
        ...DEFAULT_APP_TOAST_OPTIONS,
        title: t('configSetup.saveFailed'),
        description: err instanceof Error ? err.message : String(err),
        status: 'error',
        duration: 6000,
      })
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
              <select
                name="symbol"
                value={formData.symbol}
                onChange={handleSymbolChange}
                disabled={isLoading}
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
              >
                {CONFIG_SETUP_SYMBOL_ORDER.map(sym => (
                  <option key={sym} value={sym}>
                    {t(`configSetup.pairSymbolLabels.${sym}`, { defaultValue: sym })}
                  </option>
                ))}
              </select>
              <div style={{ marginTop: '4px', fontSize: '12px', color: '#8c8c8c' }}>
                {t('configSetup.symbolSelectHint')}
              </div>
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
                step="0.0001"
                min="0.0001"
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
                step="0.01"
                min="0.01"
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
                step="0.01"
                min="0.01"
                style={{
                  width: '100%',
                  padding: '12px',
                  border: '1px solid #d9d9d9',
                  borderRadius: '4px',
                  fontSize: '14px'
                }}
                placeholder={t('configSetup.minOrderValuePlaceholder')}
              />
              <div style={{ marginTop: '4px', fontSize: '12px', color: '#8c8c8c' }}>
                {t('configSetup.minOrderValueDecimalsHint')}
              </div>
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

          <div style={{ display: 'flex', gap: '12px', marginTop: '24px' }}>
            <button
              type="button"
              onClick={handleSkip}
              disabled={isLoading || success}
              style={{
                flex: 1,
                padding: '12px',
                backgroundColor: 'transparent',
                color: '#8c8c8c',
                border: '1px solid #d9d9d9',
                borderRadius: '4px',
                fontSize: '16px',
                cursor: (isLoading || success) ? 'not-allowed' : 'pointer',
                opacity: (isLoading || success) ? 0.6 : 1
              }}
            >
              {t('configSetup.skip')}
            </button>
            <button
              type="submit"
              disabled={isLoading || success}
              style={{
                flex: 2,
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
          </div>
        </form>
      </div>
    </div>
  )
}

export default ConfigSetup

