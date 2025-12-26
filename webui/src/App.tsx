import React from 'react'
import { BrowserRouter, Routes, Route, Link, Navigate } from 'react-router-dom'
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
        <h1>QuantMesh Market Maker</h1>
        {isAuthenticated && (
          <button
            onClick={handleLogout}
            style={{
              padding: '8px 16px',
              backgroundColor: '#ff4d4f',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
              fontSize: '14px'
            }}
          >
            退出登录
          </button>
        )}
      </header>
      <nav className="app-nav">
        <Link to="/">仪表盘</Link>
        <Link to="/positions">持仓</Link>
        <Link to="/orders">订单</Link>
        <Link to="/slots">槽位</Link>
        <Link to="/strategies">策略配比</Link>
        <Link to="/statistics">统计</Link>
                <Link to="/reconciliation">对账</Link>
                <Link to="/risk">风控监控</Link>
                <Link to="/system-monitor">系统监控</Link>
                <Link to="/kline">K线图</Link>
                <Link to="/logs">日志</Link>
                <Link to="/profile">个人资料</Link>
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
                  <Route path="/logs" element={<ProtectedRoute><Logs /></ProtectedRoute>} />
                  <Route path="/profile" element={<ProtectedRoute><Profile /></ProtectedRoute>} />
          <Route path="/login" element={<Login />} />
          <Route path="/setup" element={<FirstTimeSetup />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
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

