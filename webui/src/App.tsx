import React, { useEffect, useState } from 'react'
import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { ChakraProvider, Box, Flex, Heading, Button, Container, Link as ChakraLink, Spinner, Center, Select, Badge, Text, HStack } from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import { AuthProvider, useAuth } from './contexts/AuthContext'
import { SymbolProvider, useSymbol } from './contexts/SymbolContext'
import { lightTheme, darkTheme } from './theme'
import Dashboard from './components/Dashboard'
import GlobalDashboard from './components/GlobalDashboard'
import SymbolSelector from './components/SymbolSelector'
import StatusBar from './components/StatusBar'
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
import MarketIntelligence from './components/MarketIntelligence'
import AIAnalysis from './components/AIAnalysis'
import AIPromptManager from './components/AIPromptManager'
import Footer from './components/Footer'
import Sidebar from './components/Sidebar'
import { logout } from './services/auth'
import { getSymbols, getFundingRateCurrent, SymbolInfo } from './services/api'
import './App.css'

// 受保护的路由组件
const ProtectedRoute: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuthenticated, isLoading, hasPassword } = useAuth()

  if (isLoading) {
    return (
      <Center h="200px">
        <Spinner size="xl" />
      </Center>
    )
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
    <ChakraLink
      as={RouterLink}
      to={to}
      px={4}
      py={2}
      borderRadius="md"
      fontSize="sm"
      fontWeight={isActive ? 'semibold' : 'medium'}
      color={isActive ? 'blue.600' : 'gray.700'}
      bg={isActive ? 'blue.50' : 'transparent'}
      _hover={{
        bg: isActive ? 'blue.100' : 'gray.100',
        textDecoration: 'none',
        transform: 'translateY(-1px)',
      }}
      transition="all 0.2s"
      whiteSpace="nowrap"
    >
      {children}
    </ChakraLink>
  )
}

// 主应用内容
const AppContent: React.FC = () => {
  const { isAuthenticated, hasPassword, isLoading } = useAuth()
  const { isGlobalView } = useSymbol()

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

  // 如果未设置密码，或正在进行首次设置流程，显示设置页面（使用亮色主题）
  if (!hasPassword || isInSetupFlow) {
    return (
      <Box bg="gray.50" minH="100vh">
        <Routes>
          <Route path="/setup" element={<FirstTimeSetup />} />
          <Route path="*" element={<Navigate to="/setup" replace />} />
        </Routes>
      </Box>
    )
  }

  // 如果已设置密码但未登录，显示登录页（使用亮色主题）
  if (!isAuthenticated) {
    return (
      <Box bg="gray.50" minH="100vh">
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="*" element={<Navigate to="/login" replace />} />
        </Routes>
      </Box>
    )
  }

  return (
    <Box minH="100vh" display="flex" flexDirection="column">
      {/* Status Bar */}
      <StatusBar />

      {/* Header */}
      <Box
        position="sticky"
        top={0}
        zIndex={100}
        bg="white"
        borderBottom="1px"
        borderColor="gray.200"
        boxShadow="sm"
      >
        <Container maxW="full" px={6}>
          <Flex h="16" alignItems="center" justifyContent="space-between">
            <HStack spacing={4}>
              <Heading size="md" fontWeight="black" color="blue.600" letterSpacing="tight">
                QuantMesh
              </Heading>
              <Badge colorScheme="blue" variant="outline" fontSize="xs">
                Market Maker
              </Badge>
            </HStack>
            
            {/* Symbol Selector in Center */}
            <Box flex="1" display="flex" justifyContent="center">
              <SymbolSelector />
            </Box>

            {isAuthenticated && (
              <Button
                variant="ghost"
                colorScheme="red"
                size="sm"
                onClick={handleLogout}
                fontWeight="medium"
              >
                退出登录
              </Button>
            )}
          </Flex>
        </Container>
      </Box>

      <Flex flex="1" overflow="hidden">
        {/* Sidebar */}
        <Sidebar />

        {/* Main Content */}
        <Box 
          flex="1" 
          ml={{ base: 0, md: '240px' }} 
          py={isGlobalView ? 0 : 6}
          bg={isGlobalView ? 'gray.900' : 'gray.50'}
          minH="calc(100vh - 64px)"
        >
          <Container maxW={isGlobalView ? "full" : "container.xl"} px={isGlobalView ? 0 : 6}>
            <Routes>
              <Route path="/" element={
                <ProtectedRoute>
                  {isGlobalView ? <GlobalDashboard /> : <Dashboard />}
                </ProtectedRoute>
              } />
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
              <Route path="/market-intelligence" element={<ProtectedRoute><MarketIntelligence /></ProtectedRoute>} />
              <Route path="/ai-analysis" element={<ProtectedRoute><AIAnalysis /></ProtectedRoute>} />
              <Route path="/ai-prompts" element={<ProtectedRoute><AIPromptManager /></ProtectedRoute>} />
              <Route path="/logs" element={<ProtectedRoute><Logs /></ProtectedRoute>} />
              <Route path="/config" element={<ProtectedRoute><Configuration /></ProtectedRoute>} />
              <Route path="/profile" element={<ProtectedRoute><Profile /></ProtectedRoute>} />
              <Route path="/login" element={<Login />} />
              <Route path="/setup" element={<FirstTimeSetup />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </Container>
          <Footer />
        </Box>
      </Flex>
    </Box>
  )
}

// Theme wrapper component
const ThemedApp: React.FC = () => {
  const { isGlobalView } = useSymbol()
  const currentTheme = isGlobalView ? darkTheme : lightTheme

  return (
    <ChakraProvider theme={currentTheme}>
      <AppContent />
    </ChakraProvider>
  )
}

function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <SymbolProvider>
          <ThemedApp />
        </SymbolProvider>
      </AuthProvider>
    </BrowserRouter>
  )
}

export default App

