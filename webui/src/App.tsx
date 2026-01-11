import React, { useEffect, useState } from 'react'
import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { 
  ChakraProvider, 
  Box, 
  Flex, 
  Heading, 
  Button, 
  Container, 
  Spinner, 
  Center, 
  Badge, 
  Text, 
  HStack,
  IconButton,
  Drawer,
  DrawerOverlay,
  DrawerContent,
  DrawerCloseButton,
  useDisclosure,
} from '@chakra-ui/react'
import { HamburgerIcon } from '@chakra-ui/icons'
import { motion, AnimatePresence } from 'framer-motion'
import { AuthProvider, useAuth } from './contexts/AuthContext'
import { SymbolProvider, useSymbol } from './contexts/SymbolContext'
import { lightTheme } from './theme'
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
import FirstTimeWizard from './components/FirstTimeWizard'
import ConfigSetup from './components/ConfigSetup'
import KlineChart from './components/KlineChart'
import { checkSetupStatus } from './services/setup'
import Configuration from './components/Configuration'
import FundingRate from './components/FundingRate'
import BasisMonitor from './components/BasisMonitor'
import MarketIntelligence from './components/MarketIntelligence'
import AIAnalysis from './components/AIAnalysis'
import AIPromptManager from './components/AIPromptManager'
import AIConfigPage from './components/AIConfigPage'
import EventCenter from './components/EventCenter'
import StrategyMarket from './components/StrategyMarket'
import CapitalManagement from './components/CapitalManagement'
import ProfitManagement from './components/ProfitManagement'
import Footer from './components/Footer'
import Sidebar from './components/Sidebar'
import MobileNav from './components/MobileNav'
import LanguageSelector from './components/LanguageSelector'
import { logout } from './services/auth'
import { useTranslation } from 'react-i18next'
import { useResponsive } from './hooks/useResponsive'
import './App.css'

const PageWrapper: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const location = useLocation()
  return (
    <motion.div
      key={location.pathname}
      initial={{ opacity: 0, y: 10 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -10 }}
      transition={{ duration: 0.3, ease: 'easeOut' }}
      style={{ width: '100%', height: '100%' }}
    >
      {children}
    </motion.div>
  )
}

// 受保护的路由组件
const ProtectedRoute: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuthenticated, isLoading, hasPassword } = useAuth()

  if (isLoading) {
    return (
      <Center h="200px">
        <Spinner size="xl" thickness="4px" color="blue.500" />
      </Center>
    )
  }

  if (!hasPassword) return <Navigate to="/setup" replace />
  if (!isAuthenticated) return <Navigate to="/login" replace />

  return <PageWrapper>{children}</PageWrapper>
}

// 主应用内容
const AppContent: React.FC = () => {
  const location = useLocation()
  const { isAuthenticated, hasPassword, isLoading } = useAuth()
  const { isGlobalView } = useSymbol()
  const { isOpen, onOpen, onClose } = useDisclosure()
  const { t } = useTranslation()
  const [needsConfig, setNeedsConfig] = useState<boolean | null>(null)
  const [configLoading, setConfigLoading] = useState(true)
  const { isMobile } = useResponsive()
  
  const headerBg = 'rgba(255, 255, 255, 0.8)'
  const borderColor = 'gray.100'
  const contentBg = isGlobalView ? 'gray.50' : 'white'

  // 检查配置状态 - 每次登录时都检查
  useEffect(() => {
    const checkConfig = async () => {
      try {
        const status = await checkSetupStatus()
        // 如果配置不完整，且本次登录未跳过配置，则显示配置页面
        const skipped = sessionStorage.getItem('config_setup_skipped') === 'true'
        setNeedsConfig(status.needs_setup && !skipped)
        
        // 如果配置已完成，清除 wizard_step 标记，避免反复跳转
        if (!status.needs_setup) {
          sessionStorage.removeItem('wizard_step')
        }
      } catch (error) {
        console.error('检查配置状态失败:', error)
        // 如果检查失败，假设需要配置
        const skipped = sessionStorage.getItem('config_setup_skipped') === 'true'
        setNeedsConfig(!skipped)
      } finally {
        setConfigLoading(false)
      }
    }
    
    // 只在已认证时检查配置
    if (isAuthenticated) {
      checkConfig()
    } else {
      setConfigLoading(false)
    }
  }, [isAuthenticated])

  const handleLogout = async () => {
    try {
      await logout()
      window.location.href = '/login'
    } catch (error) {
      console.error(t('app.logoutError'), error)
    }
  }

  if (isLoading || configLoading) {
    return (
      <Center h="100vh">
        <Spinner size="xl" thickness="4px" color="blue.500" />
      </Center>
    )
  }

  // 如果配置不完整，显示配置引导页面
  if (needsConfig) {
    return (
      <Box bg="gray.50" minH="100vh">
        <Routes>
          <Route path="/config-setup" element={<ConfigSetup />} />
          <Route path="*" element={<Navigate to="/config-setup" replace />} />
        </Routes>
      </Box>
    )
  }

  const isInSetupFlow = sessionStorage.getItem('setup_step') !== null
  const isWizardPending = sessionStorage.getItem('wizard_step') === 'pending'

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

  // 检查是否正在访问独立页面路径（不需要侧边栏的页面）
  const isStandalonePage = location.pathname === '/wizard' || location.pathname === '/setup' || location.pathname === '/config-setup'

  // 处理向导页面的独立显示
  if (isAuthenticated && location.pathname === '/wizard') {
    return (
      <Box bg="gray.50" minH="100vh">
        <Routes>
          <Route path="/wizard" element={<FirstTimeWizard />} />
          <Route path="*" element={<Navigate to="/wizard" replace />} />
        </Routes>
      </Box>
    )
  }

  // 如果密码已设置但需要配置向导，且已登录，自动跳转到向导
  // 但只有在配置确实未完成时才跳转（避免反复跳转）
  if (isAuthenticated && isWizardPending && needsConfig !== false) {
    // 如果配置状态未知（还在加载），等待加载完成
    if (needsConfig === null) {
      return (
        <Center h="100vh">
          <Spinner size="xl" thickness="4px" color="blue.500" />
        </Center>
      )
    }
    
    // 如果配置需要设置，才跳转到向导
    if (needsConfig === true) {
      return <Navigate to="/wizard" replace />
    }
    
    // 如果配置已完成但 wizard_step 还在，清除它
    if (needsConfig === false) {
      sessionStorage.removeItem('wizard_step')
    }
  }

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
      {/* Header */}
      <Box
        position="sticky"
        top={0}
        zIndex={100}
        bg={headerBg}
        backdropFilter="blur(20px)"
        borderBottom="1px"
        borderColor={borderColor}
      >
        <Container maxW="full" px={{ base: 4, md: 6 }}>
          <Flex h="14" alignItems="center" justifyContent="space-between">
            <HStack spacing={{ base: 2, md: 4 }}>
              <IconButton
                display={{ base: 'flex', md: 'none' }}
                aria-label="Open menu"
                icon={<HamburgerIcon />}
                variant="ghost"
                onClick={onOpen}
              />
              <Heading size="sm" fontWeight="800" color="blue.600" letterSpacing="tighter">
                QuantMesh
              </Heading>
              <Badge 
                display={{ base: 'none', sm: 'inline-block' }}
                colorScheme="blue" 
                variant="subtle" 
                fontSize="10px" 
                borderRadius="full" 
                px={2}
              >
                MM
              </Badge>
            </HStack>
            
            <Box flex="1" display="flex" justifyContent="center">
              <SymbolSelector />
            </Box>

            {isAuthenticated && (
              <HStack spacing={{ base: 2, md: 4 }}>
                <Box display={{ base: 'none', lg: 'block' }}>
                  <StatusBar />
                </Box>
                <LanguageSelector />
                <Button
                  variant="ghost"
                  colorScheme="gray"
                  size="xs"
                  onClick={handleLogout}
                  fontWeight="600"
                  borderRadius="full"
                >
                  {t('common.logout')}
                </Button>
              </HStack>
            )}
          </Flex>
        </Container>
      </Box>

      <Flex flex="1" overflow="hidden">
        {/* Desktop Sidebar */}
        <Box display={{ base: 'none', md: 'block' }}>
          <Sidebar />
        </Box>

        {/* Mobile Sidebar (Drawer) */}
        <Drawer isOpen={isOpen} placement="left" onClose={onClose}>
          <DrawerOverlay />
          <DrawerContent bg="white" maxW="240px">
            <DrawerCloseButton zIndex={20} />
            <Sidebar onNavItemClick={onClose} isDrawer />
          </DrawerContent>
        </Drawer>

        <Box 
          flex="1" 
          ml={{ base: 0, md: '240px' }} 
          bg={contentBg}
          minH="calc(100vh - 56px)"
          position="relative"
          transition="margin-left 0.3s"
          pb={isMobile ? '60px' : 0}
        >
          {/* Subtle Background Accent */}
          <Box
            position="absolute"
            top="0"
            right="0"
            w="400px"
            h="400px"
            bgGradient="radial(blue.500, transparent)"
            opacity="0.03"
            filter="blur(60px)"
            pointerEvents="none"
          />

          <Container maxW={isGlobalView ? "full" : "container.xl"} px={{ base: 4, md: 8 }} py={{ base: 6, md: 8 }}>
            <AnimatePresence mode="wait">
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
                <Route path="/basis-monitor" element={<ProtectedRoute><BasisMonitor /></ProtectedRoute>} />
                <Route path="/market-intelligence" element={<ProtectedRoute><MarketIntelligence /></ProtectedRoute>} />
                <Route path="/ai-analysis" element={<ProtectedRoute><AIAnalysis /></ProtectedRoute>} />
                <Route path="/ai-prompts" element={<ProtectedRoute><AIPromptManager /></ProtectedRoute>} />
                <Route path="/ai-config" element={<ProtectedRoute><AIConfigPage /></ProtectedRoute>} />
                <Route path="/events" element={<ProtectedRoute><EventCenter /></ProtectedRoute>} />
                <Route path="/strategy-market" element={<ProtectedRoute><StrategyMarket /></ProtectedRoute>} />
                <Route path="/capital-management" element={<ProtectedRoute><CapitalManagement /></ProtectedRoute>} />
                <Route path="/profit-management" element={<ProtectedRoute><ProfitManagement /></ProtectedRoute>} />
                <Route path="/logs" element={<ProtectedRoute><Logs /></ProtectedRoute>} />
                <Route path="/config" element={<ProtectedRoute><Configuration /></ProtectedRoute>} />
                <Route path="/profile" element={<ProtectedRoute><Profile /></ProtectedRoute>} />
                <Route path="/login" element={<Login />} />
                <Route path="/setup" element={<FirstTimeSetup />} />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            </AnimatePresence>
          </Container>
          {!isMobile && <Footer />}
        </Box>
      </Flex>

      {/* 移动端底部导航 */}
      {isMobile && isAuthenticated && <MobileNav onMenuOpen={onOpen} />}
    </Box>
  )
}

const ThemedApp: React.FC = () => {
  return (
    <ChakraProvider theme={lightTheme}>
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
