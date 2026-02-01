import React from 'react'
import { Link as RouterLink } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import './Footer.css'

const Footer: React.FC = () => {
  const { t } = useTranslation()
  const year = new Date().getFullYear()

  return (
    <footer className="app-footer">
      <div className="app-footer-content">
        <div className="app-footer-section">
          <p className="app-footer-copyright">
            {t('footer.copyright', { year })}
            <span className="app-footer-version"> v{__APP_VERSION__}</span>
          </p>
          <div className="app-footer-links">
            <RouterLink to="/terms" className="app-footer-link">{t('footer.terms')}</RouterLink>
            <span className="app-footer-separator">|</span>
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

