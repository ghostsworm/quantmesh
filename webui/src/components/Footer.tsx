import React, { useState, useEffect } from 'react'
import { Link as RouterLink } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import './Footer.css'

// 从 package.json 导入版本号（在构建时会被 Vite 替换）
const FRONTEND_VERSION = __APP_VERSION__ || 'unknown'

const Footer: React.FC = () => {
  const { t } = useTranslation()
  const year = new Date().getFullYear()
  const [backendVersion, setBackendVersion] = useState<string>('loading...')

  useEffect(() => {
    // 获取后端版本号
    const fetchBackendVersion = async () => {
      try {
        const response = await fetch('/api/version')
        if (response.ok) {
          const data = await response.json()
          setBackendVersion(data.backend_version || data.version || 'unknown')
        } else {
          setBackendVersion('unknown')
        }
      } catch (error) {
        console.error('Failed to fetch backend version:', error)
        setBackendVersion('unknown')
      }
    }

    fetchBackendVersion()
  }, [])

  return (
    <footer className="app-footer">
      <div className="app-footer-content">
        <div className="app-footer-section">
          <p className="app-footer-copyright">
            {t('footer.copyright', { year })}
          </p>
          <p className="app-footer-version">
            {t('footer.versionInfo', { frontend: FRONTEND_VERSION, backend: backendVersion })}
          </p>
          <div className="app-footer-links">
            <RouterLink to="/terms" className="app-footer-link">{t('footer.terms')}</RouterLink>
            <RouterLink to="/privacy" className="app-footer-link">{t('footer.privacy')}</RouterLink>
          </div>
        </div>
        <div className="app-footer-section">
          <div className="app-footer-disclaimer">
            <p className="app-footer-disclaimer-title">{t('footer.disclaimerTitle')}</p>
            <p className="app-footer-disclaimer-text">{t('footer.disclaimerText')}</p>
          </div>
        </div>
      </div>
    </footer>
  )
}

export default Footer

