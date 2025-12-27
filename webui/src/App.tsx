import React from 'react'
import { BrowserRouter, Routes, Route, Link, Navigate, useLocation } from 'react-router-dom'
import { AuthProvider, useAuth } from './contexts/AuthContext'
import Dashboard from './components/Dashboard'
import Positions from './components/Positions'
import Orders from './components/Orders'
import Statistics from './components/Statistics'
import SystemMonitor from './components/SystemMonitor'
import Logs from './components/Logs'
import Slots from './components/Slots'
import StrategyAllocation from './components/StrategyAllocation'
import Reconciliation from './components/Reconciliation'
import RiskMonitor from './components/RiskMonitor'
import Profile from './components/Profile'
import Login from './components/Login'
import FirstTimeSetup from './components/FirstTimeSetup'
import KlineChart from './components/KlineChart'
import Configuration from './components/Configuration'
import FundingRate from './components/FundingRate'
import Footer from './components/Footer'
import { logout } from './services/auth'
import './App.css'

// 受保护的路由组件
const ProtectedRoute: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuthenticated, isLoading, hasPassword } = useAuth()

  if (isLoading) {
    return <div style={{ padding: '40px', textAlign: 'center' }}>加载中...</div>
  }

  // 如果未设置密码，显示首次设置向导
  if (!hasPassword) {
    return <Navigate to="/setup" replace />
  }

  // 如果未登录，重定向到登录页
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return <>{children}</>
}

// 导航链接组件，支持活跃状态
const NavLink: React.FC<{ to: string; children: React.ReactNode }> = ({ to, children }) => {
  const location = useLocation()
  const isActive = location.pathname === to || (to !== '/' && location.pathname.startsWith(to))

  return (
    <Link
      to={to}
      className={`app-nav-link ${isActive ? 'active' : ''}`}
    >
      {children}
    </Link>
  )
}

// 主应用内容
const AppContent: React.FC = () => {
  const { isAuthenticated, hasPassword, isLoading } = useAuth()

  const handleLogout = async () => {
    try {
      await logout()
      window.location.href = '/login'
    } catch (error) {
      console.error('退出登录失败:', error)
    }
  }

  if (isLoading) {
    return <div style={{ padding: '40px', textAlign: 'center' }}>加载中...</div>
  }

  // 根据认证状态决定显示的内容
  // 检查是否正在进行首次设置流程
  const isInSetupFlow = sessionStorage.getItem('setup_step') !== null

  // 如果未设置密码，或正在进行首次设置流程，显示设置页面
  if (!hasPassword || isInSetupFlow) {
    return (
      <Routes>
        <Route path="/setup" element={<FirstTimeSetup />} />
        <Route path="*" element={<Navigate to="/setup" replace />} />
      </Routes>
    )
  }

  // 如果已设置密码但未登录，显示登录页
  if (!isAuthenticated) {
    return (
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="*" element={<Navigate to="/login" replace />} />
      </Routes>
    )
  }

  return (
    <div className="app">
      <header className="app-header">
        <div className="app-header-content">
          <div className="app-header-brand">
            <h1 className="app-header-title">QuantMesh Market Maker</h1>
          </div>
          {isAuthenticated && (
            <div className="app-header-actions">
              <button
                onClick={handleLogout}
                className="app-header-logout-btn"
              >
                退出登录
              </button>
            </div>
          )}
        </div>
      </header>
      <nav className="app-nav">
        <div className="app-nav-container">
          <NavLink to="/">仪表盘</NavLink>
          <NavLink to="/positions">持仓</NavLink>
          <NavLink to="/orders">订单</NavLink>
          <NavLink to="/slots">槽位</NavLink>
          <NavLink to="/strategies">策略配比</NavLink>
          <NavLink to="/statistics">统计</NavLink>
          <NavLink to="/reconciliation">对账</NavLink>
          <NavLink to="/risk">风控监控</NavLink>
          <NavLink to="/system-monitor">系统监控</NavLink>
          <NavLink to="/kline">K线图</NavLink>
          <NavLink to="/funding-rate">资金费率</NavLink>
          <NavLink to="/logs">日志</NavLink>
          <NavLink to="/config">配置</NavLink>
          <NavLink to="/profile">个人资料</NavLink>
        </div>
      </nav>
      <main className="app-main">
        <Routes>
          <Route path="/" element={<ProtectedRoute><Dashboard /></ProtectedRoute>} />
          <Route path="/positions" element={<ProtectedRoute><Positions /></ProtectedRoute>} />
          <Route path="/orders" element={<ProtectedRoute><Orders /></ProtectedRoute>} />
          <Route path="/slots" element={<ProtectedRoute><Slots /></ProtectedRoute>} />
          <Route path="/strategies" element={<ProtectedRoute><StrategyAllocation /></ProtectedRoute>} />
          <Route path="/statistics" element={<ProtectedRoute><Statistics /></ProtectedRoute>} />
          <Route path="/reconciliation" element={<ProtectedRoute><Reconciliation /></ProtectedRoute>} />
          <Route path="/risk" element={<ProtectedRoute><RiskMonitor /></ProtectedRoute>} />
          <Route path="/system-monitor" element={<ProtectedRoute><SystemMonitor /></ProtectedRoute>} />
          <Route path="/kline" element={<ProtectedRoute><KlineChart /></ProtectedRoute>} />
          <Route path="/funding-rate" element={<ProtectedRoute><FundingRate /></ProtectedRoute>} />
          <Route path="/logs" element={<ProtectedRoute><Logs /></ProtectedRoute>} />
          <Route path="/config" element={<ProtectedRoute><Configuration /></ProtectedRoute>} />
          <Route path="/profile" element={<ProtectedRoute><Profile /></ProtectedRoute>} />
          <Route path="/login" element={<Login />} />
          <Route path="/setup" element={<FirstTimeSetup />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
      <Footer />
    </div>
  )
}

function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <AppContent />
      </AuthProvider>
    </BrowserRouter>
  )
}

export default App

