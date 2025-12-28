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
  useColorModeValue,
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
  const { isAuthenticated, hasPassword, isLoading } = useAuth()
  const { isGlobalView } = useSymbol()
  const { isOpen, onOpen, onClose } = useDisclosure()
  
  const headerBg = useColorModeValue('rgba(255, 255, 255, 0.8)', 'rgba(26, 32, 44, 0.8)')
  const borderColor = useColorModeValue('gray.100', 'whiteAlpha.100')
  const contentBg = useColorModeValue(
    isGlobalView ? 'gray.50' : 'white',
    isGlobalView ? 'gray.900' : 'gray.800'
  )

  const handleLogout = async () => {
    try {
      await logout()
      window.location.href = '/login'
    } catch (error) {
      console.error('退出登录失败:', error)
    }
  }

  if (isLoading) {
    return (
      <Center h="100vh">
        <Spinner size="xl" thickness="4px" color="blue.500" />
      </Center>
    )
  }

  const isInSetupFlow = sessionStorage.getItem('setup_step') !== null

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
                <Button
                  variant="ghost"
                  colorScheme="gray"
                  size="xs"
                  onClick={handleLogout}
                  fontWeight="600"
                  borderRadius="full"
                >
                  退出
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
          <DrawerContent bg={useColorModeValue('white', 'gray.800')} maxW="240px">
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
            </AnimatePresence>
          </Container>
          <Footer />
        </Box>
      </Flex>
    </Box>
  )
}

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
