import React from 'react'
import { Link as RouterLink } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import './Footer.css'

// 从 package.json 导入版本号（在构建时会被 Vite 替换）
const APP_VERSION = __APP_VERSION__ || '3.62.1-rc1'

const Footer: React.FC = () => {
  const { t } = useTranslation()
  const year = new Date().getFullYear()

  return (
    <footer className="app-footer">
      <div className="app-footer-content">
        <div className="app-footer-section">
          <p className="app-footer-copyright">
            {t('footer.copyright', { year, version: APP_VERSION })}
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

