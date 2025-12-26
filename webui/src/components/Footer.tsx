import React from 'react'
import './Footer.css'

const Footer: React.FC = () => {
  return (
    <footer className="app-footer">
      <div className="app-footer-content">
        <div className="app-footer-section">
          <p className="app-footer-copyright">
            © {new Date().getFullYear()} QuantMesh Market Maker. All rights reserved.
          </p>
        </div>
        <div className="app-footer-section">
          <div className="app-footer-disclaimer">
            <p className="app-footer-disclaimer-title">免责声明</p>
            <p className="app-footer-disclaimer-text">
              本系统仅供量化交易研究和学习使用。加密货币交易存在高风险，可能导致本金损失。
              使用本系统进行交易前，请充分了解相关风险，并确保您具备相应的风险承受能力。
              本系统不对任何交易损失承担责任。投资有风险，入市需谨慎。
            </p>
          </div>
        </div>
      </div>
    </footer>
  )
}

export default Footer

