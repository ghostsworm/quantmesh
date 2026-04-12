import React, { useEffect, useState, Suspense, lazy } from 'react'
import { BrowserRouter, Routes, Route, Navigate, useLocation, useNavigate } from 'react-router-dom'
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
  Grid,
  GridItem,
  IconButton,
  Drawer,
  DrawerOverlay,
  DrawerContent,
  DrawerCloseButton,
  useDisclosure,
} from '@chakra-ui/react'
import { HamburgerIcon, ChevronLeftIcon } from '@chakra-ui/icons'
import { AuthProvider, useAuth } from './contexts/AuthContext'
import { BotProvider, useBot } from './contexts/BotContext'
import { SymbolProvider, useSymbol } from './contexts/SymbolContext'
import { ConfigProvider } from './contexts/ConfigContext'
import { lightTheme } from './theme'
// 布局组件 - 每个页面都需要，保持静态导入
import SymbolSelector from './components/SymbolSelector'
import GlobalHeaderStatus from './components/GlobalHeaderStatus'
import Footer from './components/Footer'
import Sidebar from './components/Sidebar'
import MobileNav from './components/MobileNav'
import LanguageSelector from './components/LanguageSelector'
import GeminiUsageMenu from './components/GeminiUsageMenu'
import ConnectionStatusBanner from './components/ConnectionStatusBanner'
import { checkSetupStatus } from './services/setup'
import { logout } from './services/auth'
import { useTranslation } from 'react-i18next'
import { useResponsive } from './hooks/useResponsive'
import './App.css'

// ─── 路由级组件 — React.lazy 懒加载，按需拆分 chunk ───
const Dashboard = lazy(() => import('./components/Dashboard'))
const GlobalDashboard = lazy(() => import('./components/GlobalDashboard'))
const Positions = lazy(() => import('./components/Positions'))
const Orders = lazy(() => import('./components/Orders'))
const Statistics = lazy(() => import('./components/Statistics'))
const DailyPnLBreakdown = lazy(() => import('./components/DailyPnLBreakdown'))
const SystemMonitor = lazy(() => import('./components/SystemMonitor'))
const Logs = lazy(() => import('./components/Logs'))
const Slots = lazy(() => import('./components/Slots'))
const StrategyAllocation = lazy(() => import('./components/StrategyAllocation'))
const Reconciliation = lazy(() => import('./components/Reconciliation'))
const RiskMonitor = lazy(() => import('./components/RiskMonitor'))
const OpeningControl = lazy(() => import('./components/OpeningControl'))
const NewsAnalysis = lazy(() => import('./components/NewsAnalysis'))
const Profile = lazy(() => import('./components/Profile'))
const Login = lazy(() => import('./components/Login'))
const TermsPage = lazy(() => import('./components/TermsPage'))
const PrivacyPage = lazy(() => import('./components/PrivacyPage'))
const FirstTimeSetup = lazy(() => import('./components/FirstTimeSetup'))
const FirstTimeWizard = lazy(() => import('./components/FirstTimeWizard'))
const ConfigSetup = lazy(() => import('./components/ConfigSetup'))
const KlineChart = lazy(() => import('./components/KlineChart'))
const GlobalKlinePage = lazy(() => import('./components/GlobalKlinePage'))
const Configuration = lazy(() => import('./components/Configuration'))
const FundingRate = lazy(() => import('./components/FundingRate'))
const BasisMonitor = lazy(() => import('./components/BasisMonitor'))
const MarketIntelligence = lazy(() => import('./components/MarketIntelligence'))
const AIAnalysis = lazy(() => import('./components/AIAnalysis'))
const AIPromptManager = lazy(() => import('./components/AIPromptManager'))
const AIConfigPage = lazy(() => import('./components/AIConfigPage'))
const EventCenter = lazy(() => import('./components/EventCenter'))
const AITaskManager = lazy(() => import('./components/AITaskManager'))
const CapitalManagement = lazy(() => import('./components/CapitalManagement'))
const ProfitManagement = lazy(() => import('./components/ProfitManagement'))
const FundingCarryDashboard = lazy(() => import('./components/FundingCarryDashboard'))
const PositionPlan = lazy(() => import('./components/PositionPlan'))
const NewbieRiskCheck = lazy(() => import('./components/NewbieRiskCheck'))
const BacktestMenu = lazy(() => import('./components/BacktestMenu'))
const ServiceStatusPage = lazy(() => import('./components/ServiceStatusPage'))
const FixManagement = lazy(() => import('./components/FixManagement'))
const DataExport = lazy(() => import('./components/DataExport'))
const KlineFilesManager = lazy(() => import('./components/KlineFilesManager'))
const StrategyMarket = lazy(() => import('./components/StrategyMarket'))
const StrategyDetail = lazy(() => import('./components/StrategyDetail'))
const BotList = lazy(() => import('./components/BotList'))
const BotDetail = lazy(() => import('./components/BotDetail'))
const BotCreateWizard = lazy(() => import('./components/BotCreateWizard'))
const AgentChat = lazy(() => import('./components/AgentChat'))
const GlobalPositions = lazy(() => import('./components/GlobalPositions'))
const OscillationAnalysis = lazy(() => import('./components/OscillationAnalysis'))

// 懒加载 fallback
const LazyFallback = () => (
  <Center h="200px">
    <Spinner size="lg" thickness="3px" color="blue.500" />
  </Center>
)

const PageWrapper: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const location = useLocation()
  // 勿使用 initial opacity:0：在部分嵌入 WebView / 與 AnimatePresence 組合下，進場動畫可能未執行完畢，導致頁面看似白屏
  return (
    <Box key={location.pathname} w="100%" minH="100%">
      {children}
    </Box>
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
  const navigate = useNavigate()
  const { isAuthenticated, hasPassword, isLoading, connectionError, securityCompromised, passwordManagerError } = useAuth()
  const { isGlobalView } = useSymbol()
  const { botId } = useBot()
  const { isOpen, onOpen, onClose } = useDisclosure()
  const { t } = useTranslation()
  const [needsConfig, setNeedsConfig] = useState<boolean | null>(null)
  const [configLoading, setConfigLoading] = useState(true)
  const { isMobile } = useResponsive()
  const isInBotMode = !!botId

  const headerBg = isInBotMode ? 'rgba(237, 242, 247, 0.95)' : 'rgba(255, 255, 255, 0.8)'
  const borderColor = isInBotMode ? 'blue.100' : 'gray.100'
  const contentBg = isInBotMode ? 'blue.50' : (isGlobalView ? 'gray.50' : 'white')

  // 检查配置状態 - 每次登錄時都检查
  useEffect(() => {
    const checkConfig = async () => {
      try {
        const status = await checkSetupStatus()
        // 如果配置不完整，且本次登錄未跳過配置，则显示配置页面
        const skipped = sessionStorage.getItem('config_setup_skipped') === 'true'
        setNeedsConfig(status.needs_setup && !skipped)
        
        // 如果配置已完成，清除 wizard_step 標記，避免反複跳轉
        if (!status.needs_setup) {
          sessionStorage.removeItem('wizard_step')
        }
      } catch (error) {
        console.error('检查配置状態失败:', error)
        // 如果检查失败，假設需要配置
        const skipped = sessionStorage.getItem('config_setup_skipped') === 'true'
        setNeedsConfig(!skipped)
      } finally {
        setConfigLoading(false)
      }
    }
    
    // 只在已认证時检查配置
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

  // 如果是连接錯误且没有缓存的密碼状態，显示连接錯误页面
  // 而不是錯误地显示設置密碼界面
  if (connectionError && !hasPassword) {
    return (
      <Center h="100vh" bg="gray.50">
        <Box textAlign="center" p={8} maxW="400px">
          <Text fontSize="6xl" mb={4}>🔌</Text>
          <Heading size="lg" mb={4} color="gray.700">
            {t('common.connectionError', '無法连接到服務器')}
          </Heading>
          <Text color="gray.500" mb={6}>
            {t('common.connectionErrorDesc', '请检查网络连接或服務器是否正常运行')}
          </Text>
          <Button
            colorScheme="blue"
            onClick={() => window.location.reload()}
          >
            {t('common.retry', '重試')}
          </Button>
        </Box>
      </Center>
    )
  }

  // 🔒 密碼管理器初始化失敗：通常是 data 目錄問題
  if (passwordManagerError) {
    return (
      <Center h="100vh" bg="orange.50">
        <Box textAlign="center" p={8} maxW="500px" bg="white" borderRadius="lg" boxShadow="lg">
          <Text fontSize="6xl" mb={4}>⚠️</Text>
          <Heading size="lg" mb={4} color="orange.600">
            {t('security.passwordManagerError', '認證系統初始化失敗')}
          </Heading>
          <Text color="gray.700" mb={4} fontWeight="bold">
            {t('security.passwordManagerErrorMessage', '服務器無法初始化密碼管理器')}
          </Text>
          <Box textAlign="left" bg="gray.50" p={4} borderRadius="md" mb={6}>
            <Text fontSize="sm" color="gray.600" mb={2} fontWeight="bold">請檢查以下問題：</Text>
            <Text fontSize="sm" color="gray.600">1. data 目錄是否存在且可寫</Text>
            <Text fontSize="sm" color="gray.600">2. Docker 是否正確掛載了 data 卷</Text>
            <Text fontSize="sm" color="gray.600">3. 查看服務器日誌中的詳細錯誤信息</Text>
          </Box>
          <Box textAlign="left" bg="blue.50" p={4} borderRadius="md" mb={6}>
            <Text fontSize="sm" color="blue.700" fontWeight="bold" mb={1}>Docker 部署提示：</Text>
            <Text fontSize="xs" color="blue.600" fontFamily="mono">
              docker run -v $(pwd)/data:/app/data ...
            </Text>
          </Box>
          <Button
            colorScheme="orange"
            onClick={() => window.location.reload()}
          >
            {t('common.retry', '重試')}
          </Button>
        </Box>
      </Center>
    )
  }

  // 🔒 安全隱患檢測：認證數據可能已丟失
  if (securityCompromised) {
    return (
      <Center h="100vh" bg="red.50">
        <Box textAlign="center" p={8} maxW="500px" bg="white" borderRadius="lg" boxShadow="lg">
          <Text fontSize="6xl" mb={4}>🔐</Text>
          <Heading size="lg" mb={4} color="red.600">
            {t('security.compromisedTitle', '安全警告')}
          </Heading>
          <Text color="gray.700" mb={4} fontWeight="bold">
            {t('security.compromisedMessage', '系統檢測到認證數據可能已丟失')}
          </Text>
          <Text color="gray.600" mb={6} fontSize="sm">
            {t('security.compromisedDetails', '系統之前已完成初始化，但認證數據庫中的密碼記錄已丟失。這可能是由於：')}
          </Text>
          <Box textAlign="left" bg="gray.50" p={4} borderRadius="md" mb={6}>
            <Text fontSize="sm" color="gray.600">• Docker 容器重新部署時未掛載 data 目錄</Text>
            <Text fontSize="sm" color="gray.600">• auth.db 數據庫文件被刪除或損壞</Text>
            <Text fontSize="sm" color="gray.600">• 數據目錄權限問題</Text>
          </Box>
          <Text color="red.500" fontWeight="bold" mb={4} fontSize="sm">
            {t('security.compromisedAction', '請聯繫管理員檢查服務器的 data 目錄，確保 auth.db 文件完整。')}
          </Text>
          <Text color="gray.500" fontSize="xs">
            {t('security.compromisedNote', '為防止未授權訪問，系統已阻止重新設置密碼。')}
          </Text>
        </Box>
      </Center>
    )
  }

  // 如果配置不完整，显示配置引導页面
  if (needsConfig) {
    return (
      <Box bg="gray.50" minH="100vh">
        <Suspense fallback={<LazyFallback />}>
          <Routes>
            <Route path="/config-setup" element={<ConfigSetup />} />
            <Route path="*" element={<Navigate to="/config-setup" replace />} />
          </Routes>
        </Suspense>
      </Box>
    )
  }

  const isInSetupFlow = sessionStorage.getItem('setup_step') !== null
  const isWizardPending = sessionStorage.getItem('wizard_step') === 'pending'

  if (!hasPassword || isInSetupFlow) {
    return (
      <Box bg="gray.50" minH="100vh">
        <Suspense fallback={<LazyFallback />}>
          <Routes>
            <Route path="/setup" element={<FirstTimeSetup />} />
            <Route path="*" element={<Navigate to="/setup" replace />} />
          </Routes>
        </Suspense>
      </Box>
    )
  }

  // 检查是否正在访问独立页面路径（不需要侧边欄的页面）
  const isStandalonePage = location.pathname === '/wizard' || location.pathname === '/setup' || location.pathname === '/config-setup'

  // 处理向導页面的独立显示
  if (isAuthenticated && location.pathname === '/wizard') {
    return (
      <Box bg="gray.50" minH="100vh">
        <Suspense fallback={<LazyFallback />}>
          <Routes>
            <Route path="/wizard" element={<FirstTimeWizard />} />
            <Route path="*" element={<Navigate to="/wizard" replace />} />
          </Routes>
        </Suspense>
      </Box>
    )
  }

  // 如果密碼已設置但需要配置向導，且已登錄，自动跳轉到向導
  // 但只有在配置确實未完成時才跳轉（避免反複跳轉）
  if (isAuthenticated && isWizardPending && needsConfig !== false) {
    // 如果配置状態未知（还在加載），等待加載完成
    if (needsConfig === null) {
      return (
        <Center h="100vh">
          <Spinner size="xl" thickness="4px" color="blue.500" />
        </Center>
      )
    }
    
    // 如果配置需要設置，才跳轉到向導
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
        <Suspense fallback={<LazyFallback />}>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/terms" element={<TermsPage />} />
            <Route path="/privacy" element={<PrivacyPage />} />
            <Route path="*" element={<Navigate to="/login" replace />} />
          </Routes>
        </Suspense>
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
          <Grid
            templateAreas={{
              base: `"brand actions"\n"status status"`,
              md: `"brand status actions"`,
            }}
            templateColumns={{
              base: 'minmax(0,1fr) auto',
              md: 'auto minmax(0,1fr) auto',
            }}
            alignItems="center"
            columnGap={{ base: 2, md: 4 }}
            rowGap={{ base: 2, md: 0 }}
            py={{ base: 2, md: 0 }}
            minH={{ md: '14' }}
          >
            <GridItem area="brand" minW={0}>
              <HStack spacing={{ base: 2, md: 4 }}>
                <IconButton
                  display={{ base: 'flex', md: 'none' }}
                  aria-label="Open menu"
                  icon={<HamburgerIcon />}
                  variant="ghost"
                  onClick={onOpen}
                />
                {isInBotMode && (
                  <Button
                    leftIcon={<ChevronLeftIcon />}
                    variant="ghost"
                    size="sm"
                    colorScheme="blue"
                    onClick={() => navigate('/bots')}
                    fontWeight="600"
                  >
                    {t('sidebar.backToBotList')}
                  </Button>
                )}
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
            </GridItem>

            <GridItem area="status" minW={0} justifySelf={{ base: 'center', md: 'stretch' }} w="100%">
              <Flex justify="center" align="center" w="100%" minW={0}>
                <GlobalHeaderStatus />
              </Flex>
            </GridItem>

            {isAuthenticated && (
              <GridItem area="actions" justifySelf="end">
                <HStack spacing={{ base: 2, md: 4 }}>
                  <GeminiUsageMenu />
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
              </GridItem>
            )}
          </Grid>
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
          ml={{ base: 0, md: 'var(--sidebar-width, 200px)' }} 
          bg={contentBg}
          minH="calc(100vh - 56px)"
          position="relative"
          transition="margin-left 0.3s ease"
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
            <Suspense fallback={<LazyFallback />}>
              <Routes>
                <Route path="/" element={
                  <ProtectedRoute>
                    <GlobalDashboard />
                  </ProtectedRoute>
                } />
                {/* 旧 flat 路由重定向到 /bots，bot 工作区改用 /bots/:botId/* */}
                <Route path="/positions" element={<Navigate to="/bots" replace />} />
                <Route path="/orders" element={<Navigate to="/bots" replace />} />
                <Route path="/slots" element={<Navigate to="/bots" replace />} />
                <Route path="/strategies" element={<Navigate to="/bots" replace />} />
                <Route path="/statistics" element={<ProtectedRoute><Statistics /></ProtectedRoute>} />
                <Route path="/statistics/daily/:date" element={<ProtectedRoute><DailyPnLBreakdown /></ProtectedRoute>} />
                <Route path="/reconciliation" element={<Navigate to="/bots" replace />} />
                <Route path="/risk" element={<ProtectedRoute><RiskMonitor /></ProtectedRoute>} />
                <Route path="/opening-control" element={<Navigate to="/bots" replace />} />
                <Route path="/news-analysis" element={<ProtectedRoute><NewsAnalysis /></ProtectedRoute>} />
                <Route path="/profit-management" element={<ProtectedRoute><ProfitManagement /></ProtectedRoute>} />
                <Route path="/funding-carry" element={<ProtectedRoute><FundingCarryDashboard /></ProtectedRoute>} />
                <Route path="/position-plan" element={<Navigate to="/bots" replace />} />
                <Route path="/kline" element={<ProtectedRoute><GlobalKlinePage /></ProtectedRoute>} />
                <Route path="/funding-rate" element={<ProtectedRoute><FundingRate /></ProtectedRoute>} />
                <Route path="/basis-monitor" element={<ProtectedRoute><BasisMonitor /></ProtectedRoute>} />
                <Route path="/oscillation" element={<ProtectedRoute><OscillationAnalysis /></ProtectedRoute>} />
                <Route path="/news-analysis/history" element={<Navigate to="/news-analysis" replace />} />
                <Route path="/news-analysis/predictions" element={<Navigate to="/news-analysis" replace />} />
                <Route path="/system-monitor" element={<ProtectedRoute><SystemMonitor /></ProtectedRoute>} />
                <Route path="/market-intelligence" element={<ProtectedRoute><MarketIntelligence /></ProtectedRoute>} />
                <Route path="/ai-analysis" element={<ProtectedRoute><AIAnalysis /></ProtectedRoute>} />
                <Route path="/ai-prompts" element={<ProtectedRoute><AIPromptManager /></ProtectedRoute>} />
                <Route path="/newbie-risk-check" element={<ProtectedRoute><NewbieRiskCheck /></ProtectedRoute>} />
                <Route path="/ai-config" element={<ProtectedRoute><AIConfigPage /></ProtectedRoute>} />
                <Route path="/ai/tasks" element={<ProtectedRoute><AITaskManager /></ProtectedRoute>} />
                <Route path="/events" element={<ProtectedRoute><EventCenter /></ProtectedRoute>} />
                <Route path="/bots" element={<ProtectedRoute><BotList /></ProtectedRoute>} />
                <Route path="/bots/create" element={<ProtectedRoute><BotCreateWizard /></ProtectedRoute>} />
                <Route path="/bots/:botId/dashboard" element={<ProtectedRoute><Dashboard /></ProtectedRoute>} />
                <Route path="/bots/:botId/positions" element={<ProtectedRoute><Positions /></ProtectedRoute>} />
                <Route path="/bots/:botId/orders" element={<ProtectedRoute><Orders /></ProtectedRoute>} />
                <Route path="/bots/:botId/slots" element={<ProtectedRoute><Slots /></ProtectedRoute>} />
                <Route path="/bots/:botId/strategies" element={<ProtectedRoute><StrategyAllocation /></ProtectedRoute>} />
                <Route path="/bots/:botId/statistics" element={<ProtectedRoute><Statistics /></ProtectedRoute>} />
                <Route path="/bots/:botId/statistics/daily/:date" element={<ProtectedRoute><DailyPnLBreakdown /></ProtectedRoute>} />
                <Route path="/bots/:botId/reconciliation" element={<ProtectedRoute><Reconciliation /></ProtectedRoute>} />
                <Route path="/bots/:botId/risk" element={<Navigate to="/risk" replace />} />
                <Route path="/bots/:botId/opening-control" element={<ProtectedRoute><OpeningControl /></ProtectedRoute>} />
                <Route path="/bots/:botId/profit-management" element={<Navigate to="/profit-management" replace />} />
                <Route path="/bots/:botId/position-plan" element={<ProtectedRoute><PositionPlan /></ProtectedRoute>} />
                <Route path="/bots/:botId/news-analysis" element={<ProtectedRoute><NewsAnalysis /></ProtectedRoute>} />
                <Route path="/bots/:botId/kline" element={<ProtectedRoute><KlineChart /></ProtectedRoute>} />
                <Route path="/bots/:botId/funding-rate" element={<ProtectedRoute><FundingRate /></ProtectedRoute>} />
                <Route path="/bots/:botId/basis-monitor" element={<ProtectedRoute><BasisMonitor /></ProtectedRoute>} />
                <Route path="/bots/:botId/config" element={<ProtectedRoute><Configuration /></ProtectedRoute>} />
                <Route path="/bots/:botId" element={<ProtectedRoute><BotDetail /></ProtectedRoute>} />
                <Route path="/strategy-detail" element={<ProtectedRoute><StrategyDetail /></ProtectedRoute>} />
                <Route path="/strategy-market" element={<ProtectedRoute><StrategyMarket /></ProtectedRoute>} />
                <Route path="/agent-chat" element={<ProtectedRoute><AgentChat /></ProtectedRoute>} />
                <Route path="/global-positions" element={<ProtectedRoute><GlobalPositions /></ProtectedRoute>} />
                <Route path="/capital-management" element={<ProtectedRoute><CapitalManagement /></ProtectedRoute>} />
                <Route path="/backtest" element={<ProtectedRoute><BacktestMenu /></ProtectedRoute>} />
                <Route path="/data-export" element={<ProtectedRoute><DataExport /></ProtectedRoute>} />
                <Route path="/kline-files" element={<ProtectedRoute><KlineFilesManager /></ProtectedRoute>} />
                <Route path="/logs" element={<ProtectedRoute><Logs /></ProtectedRoute>} />
                <Route path="/services/status" element={<ProtectedRoute><ServiceStatusPage /></ProtectedRoute>} />
                <Route path="/fix" element={<ProtectedRoute><FixManagement /></ProtectedRoute>} />
                <Route path="/config" element={<ProtectedRoute><Configuration /></ProtectedRoute>} />
                <Route path="/profile" element={<ProtectedRoute><Profile /></ProtectedRoute>} />
                <Route path="/terms" element={<TermsPage />} />
                <Route path="/privacy" element={<PrivacyPage />} />
                <Route path="/login" element={<Login />} />
                <Route path="/setup" element={<FirstTimeSetup />} />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            </Suspense>
          </Container>
          {!isMobile && <Footer />}
        </Box>
      </Flex>

      {/* 移动端底部導航 */}
      {isMobile && isAuthenticated && <MobileNav onMenuOpen={onOpen} />}
    </Box>
  )
}

const ThemedApp: React.FC = () => {
  return (
    <ChakraProvider theme={lightTheme}>
      <ConnectionStatusBanner />
      <AppContent />
    </ChakraProvider>
  )
}

function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <BotProvider>
          <SymbolProvider>
            <ConfigProvider>
              <ThemedApp />
            </ConfigProvider>
          </SymbolProvider>
        </BotProvider>
      </AuthProvider>
    </BrowserRouter>
  )
}

export default App
