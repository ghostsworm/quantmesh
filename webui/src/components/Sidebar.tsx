import React, { useState, useEffect } from 'react'
import {
  Box,
  VStack,
  Icon,
  Text,
  Flex,
  Divider,
  Heading,
  IconButton,
  Tooltip,
} from '@chakra-ui/react'
import { Link as RouterLink, useLocation } from 'react-router-dom'
import { motion, AnimatePresence } from 'framer-motion'
import {
  InfoIcon,
  SettingsIcon,
  EditIcon,
  RepeatIcon,
  StarIcon,
  SearchIcon,
  LockIcon,
  ViewIcon,
  TriangleUpIcon,
  TimeIcon,
  AtSignIcon,
  CalendarIcon,
  QuestionIcon,
  DragHandleIcon,
  AddIcon,
  BellIcon,
  MoonIcon,
  ExternalLinkIcon,
  CheckCircleIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
} from '@chakra-ui/icons'
import { useSymbol } from '../contexts/SymbolContext'
import { useTranslation } from 'react-i18next'

const MotionBox = motion(Box)
const MotionFlex = motion(Flex)

interface NavItemProps {
  icon: any
  children: string
  to: string
  isActive?: boolean
  onClick?: () => void
  collapsed?: boolean
}

const NavItem: React.FC<NavItemProps> = ({ icon, children, to, isActive, onClick, collapsed = false }) => {
  const activeBg = 'blue.50'
  const activeColor = 'blue.600'
  const hoverBg = 'gray.50'
  const textColor = 'gray.600'

  const navContent = (
    <MotionFlex
      as={RouterLink}
      to={to}
      align="center"
      justify={collapsed ? 'center' : 'flex-start'}
      px={collapsed ? "2" : "4"}
      py="2.5"
      mx={collapsed ? "2" : "3"}
      borderRadius="xl"
      role="group"
      cursor="pointer"
      bg={isActive ? activeBg : 'transparent'}
      color={isActive ? activeColor : textColor}
      onClick={onClick}
      whileHover={collapsed ? { scale: 1.05 } : { x: 4 }}
      whileTap={{ scale: 0.98 }}
      _hover={{
        bg: isActive ? activeBg : hoverBg,
        color: isActive ? activeColor : 'gray.900',
      }}
      transition="all 0.2s"
      mb={0.5}
      position="relative"
    >
      <Icon
        mr={collapsed ? "0" : "3"}
        fontSize="18"
        as={icon}
        color={isActive ? activeColor : 'inherit'}
        _groupHover={{
          color: isActive ? activeColor : 'blue.500',
        }}
      />
      {!collapsed && (
        <Text fontSize="sm" fontWeight={isActive ? '600' : 'medium'} letterSpacing="tight">
          {children}
        </Text>
      )}
      
      {isActive && !collapsed && (
        <MotionBox
          layoutId="active-pill"
          position="absolute"
          left="-12px"
          width="4px"
          height="16px"
          bg="blue.500"
          borderRadius="full"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
        />
      )}
    </MotionFlex>
  )

  // 只在收起状态时显示 Tooltip
  if (collapsed) {
    return (
      <Tooltip label={children} placement="right" hasArrow>
        {navContent}
      </Tooltip>
    )
  }

  return navContent
}

interface SidebarProps {
  onNavItemClick?: () => void
  isDrawer?: boolean
}

const SIDEBAR_COLLAPSED_KEY = 'sidebar_collapsed'

const Sidebar: React.FC<SidebarProps> = ({ onNavItemClick, isDrawer }) => {
  const { isGlobalView, selectedSymbol } = useSymbol()
  const location = useLocation()
  const { t } = useTranslation()
  
  // 从 localStorage 读取收起状态，默认展开
  const [collapsed, setCollapsed] = useState(() => {
    if (isDrawer) return false // 移动端不收起
    const saved = localStorage.getItem(SIDEBAR_COLLAPSED_KEY)
    return saved === 'true'
  })
  
  const bgColor = 'rgba(255, 255, 255, 0.8)'
  const borderColor = 'gray.100'
  
  // 宽度：收起时 64px，展开时 200px（比原来的 240px 更窄）
  const sidebarWidth = collapsed ? '64px' : '200px'

  const isRouteActive = (path: string) => {
    if (path === '/' && location.pathname === '/') return true
    return path !== '/' && location.pathname.startsWith(path)
  }

  const menuTransition = {
    type: "spring",
    stiffness: 300,
    damping: 30
  }

  const toggleCollapse = () => {
    const newCollapsed = !collapsed
    setCollapsed(newCollapsed)
    if (!isDrawer) {
      localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(newCollapsed))
    }
  }

  // 同步更新主内容区域的左边距和 CSS 变量
  useEffect(() => {
    if (!isDrawer) {
      const root = document.documentElement
      root.style.setProperty('--sidebar-width', sidebarWidth)
    }
    return () => {
      // 清理时恢复默认值
      if (!isDrawer) {
        const root = document.documentElement
        root.style.setProperty('--sidebar-width', '200px')
      }
    }
  }, [sidebarWidth, isDrawer])

  return (
    <Box
      as="nav"
      pos={isDrawer ? 'relative' : 'fixed'}
      left="0"
      h={isDrawer ? '100vh' : 'calc(100vh - 56px)'}
      top={isDrawer ? '0' : '56px'}
      pb="10"
      overflowX="hidden"
      overflowY="auto"
      bg={isDrawer ? 'transparent' : bgColor}
      backdropFilter={isDrawer ? 'none' : 'blur(20px)'}
      borderRight={isDrawer ? 'none' : '1px solid'}
      borderRightColor={borderColor}
      w={isDrawer ? 'full' : sidebarWidth}
      zIndex="10"
      transition="width 0.3s ease"
      css={{
        '&::-webkit-scrollbar': {
          width: '4px',
        },
        '&::-webkit-scrollbar-track': {
          width: '6px',
        },
        '&::-webkit-scrollbar-thumb': {
          background: 'rgba(0,0,0,0.05)',
          borderRadius: '24px',
        },
      }}
    >
      <VStack align="stretch" spacing={1} mt={isDrawer ? 10 : 5}>
        {/* 收起/展开按钮 */}
        {!isDrawer && (
          <Flex justify="flex-end" px={2} py={2}>
            <IconButton
              aria-label={collapsed ? '展开侧边栏' : '收起侧边栏'}
              icon={collapsed ? <ChevronRightIcon /> : <ChevronLeftIcon />}
              size="sm"
              variant="ghost"
              onClick={toggleCollapse}
              borderRadius="md"
              _hover={{ bg: 'gray.100' }}
            />
          </Flex>
        )}
        
        {!collapsed && (
          <Box px="7" mb="2">
            <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
              {t('common.global')}
            </Heading>
          </Box>
        )}
        <NavItem 
          icon={InfoIcon} 
          to="/" 
          isActive={isRouteActive('/') && isGlobalView}
          onClick={onNavItemClick}
          collapsed={collapsed}
        >
          {t('sidebar.overview')}
        </NavItem>
        <NavItem 
          icon={SettingsIcon} 
          to="/system-monitor" 
          isActive={isRouteActive('/system-monitor')}
          onClick={onNavItemClick}
          collapsed={collapsed}
        >
          {t('sidebar.performanceMonitor')}
        </NavItem>
        <NavItem 
          icon={BellIcon} 
          to="/events" 
          isActive={isRouteActive('/events')}
          onClick={onNavItemClick}
          collapsed={collapsed}
        >
          {t('sidebar.eventCenter')}
        </NavItem>
        <NavItem 
          icon={EditIcon} 
          to="/logs" 
          isActive={isRouteActive('/logs')}
          onClick={onNavItemClick}
          collapsed={collapsed}
        >
          {t('sidebar.runLogs')}
        </NavItem>
        <NavItem 
          icon={QuestionIcon} 
          to="/ai-prompts" 
          isActive={isRouteActive('/ai-prompts')}
          onClick={onNavItemClick}
          collapsed={collapsed}
        >
          {t('sidebar.aiPrompts')}
        </NavItem>
        <NavItem 
          icon={MoonIcon} 
          to="/ai-config" 
          isActive={isRouteActive('/ai-config')}
          onClick={onNavItemClick}
          collapsed={collapsed}
        >
          {t('sidebar.aiConfig')}
        </NavItem>
        <NavItem 
          icon={ExternalLinkIcon} 
          to="/strategy-market" 
          isActive={isRouteActive('/strategy-market')}
          onClick={onNavItemClick}
          collapsed={collapsed}
        >
          {t('sidebar.strategyMarket')}
        </NavItem>
        <NavItem 
          icon={CheckCircleIcon} 
          to="/capital-management" 
          isActive={isRouteActive('/capital-management')}
          onClick={onNavItemClick}
          collapsed={collapsed}
        >
          {t('sidebar.capitalManagement')}
        </NavItem>
        <NavItem 
          icon={RepeatIcon} 
          to="/profit-management" 
          isActive={isRouteActive('/profit-management')}
          onClick={onNavItemClick}
          collapsed={collapsed}
        >
          {t('sidebar.profitManagement')}
        </NavItem>

        <AnimatePresence>
          {!isGlobalView && (
            <MotionBox
              initial={{ height: 0, opacity: 0 }}
              animate={{ height: 'auto', opacity: 1 }}
              exit={{ height: 0, opacity: 0 }}
              transition={menuTransition}
              overflow="hidden"
            >
              <Divider my={4} mx={collapsed ? "2" : "6"} borderColor={borderColor} />
              
              {!collapsed && (
                <Box px="7" mb="2">
                  <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
                    {t('common.trading')}: {selectedSymbol}
                  </Heading>
                </Box>
              )}
              <NavItem 
                icon={ViewIcon} 
                to="/" 
                isActive={isRouteActive('/') && !isGlobalView}
                onClick={onNavItemClick}
                collapsed={collapsed}
              >
                {t('sidebar.tradingPanel')}
              </NavItem>
              <NavItem 
                icon={DragHandleIcon} 
                to="/positions" 
                isActive={isRouteActive('/positions')}
                onClick={onNavItemClick}
                collapsed={collapsed}
              >
                {t('sidebar.currentPositions')}
              </NavItem>
              <NavItem 
                icon={RepeatIcon} 
                to="/orders" 
                isActive={isRouteActive('/orders')}
                onClick={onNavItemClick}
                collapsed={collapsed}
              >
                {t('sidebar.orderManagement')}
              </NavItem>
              <NavItem 
                icon={AddIcon} 
                to="/slots" 
                isActive={isRouteActive('/slots')}
                onClick={onNavItemClick}
                collapsed={collapsed}
              >
                {t('sidebar.strategySlots')}
              </NavItem>
              <NavItem 
                icon={StarIcon} 
                to="/strategies" 
                isActive={isRouteActive('/strategies')}
                onClick={onNavItemClick}
                collapsed={collapsed}
              >
                {t('sidebar.strategyAllocation')}
              </NavItem>
              <NavItem 
                icon={CalendarIcon} 
                to="/statistics" 
                isActive={isRouteActive('/statistics')}
                onClick={onNavItemClick}
                collapsed={collapsed}
              >
                {t('sidebar.profitStatistics')}
              </NavItem>
              <NavItem 
                icon={SearchIcon} 
                to="/reconciliation" 
                isActive={isRouteActive('/reconciliation')}
                onClick={onNavItemClick}
                collapsed={collapsed}
              >
                {t('sidebar.reconciliation')}
              </NavItem>
              <NavItem 
                icon={TriangleUpIcon} 
                to="/risk" 
                isActive={isRouteActive('/risk')}
                onClick={onNavItemClick}
                collapsed={collapsed}
              >
                {t('sidebar.riskMonitor')}
              </NavItem>
              <NavItem 
                icon={TimeIcon} 
                to="/kline" 
                isActive={isRouteActive('/kline')}
                onClick={onNavItemClick}
                collapsed={collapsed}
              >
                {t('sidebar.klineDepth')}
              </NavItem>
              <NavItem 
                icon={AtSignIcon} 
                to="/funding-rate" 
                isActive={isRouteActive('/funding-rate')}
                onClick={onNavItemClick}
                collapsed={collapsed}
              >
                {t('sidebar.fundingRate')}
              </NavItem>
              <NavItem 
                icon={AtSignIcon} 
                to="/basis-monitor" 
                isActive={isRouteActive('/basis-monitor')}
                onClick={onNavItemClick}
                collapsed={collapsed}
              >
                {t('sidebar.basisMonitor')}
              </NavItem>
            </MotionBox>
          )}
        </AnimatePresence>

        <Divider my={4} mx={collapsed ? "2" : "6"} borderColor={borderColor} />

        {!collapsed && (
          <Box px="7" mb="2">
            <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
              {t('common.system')}
            </Heading>
          </Box>
        )}
        <NavItem 
          icon={SettingsIcon} 
          to="/config" 
          isActive={isRouteActive('/config')}
          onClick={onNavItemClick}
          collapsed={collapsed}
        >
          {t('sidebar.configManagement')}
        </NavItem>
        <NavItem 
          icon={InfoIcon} 
          to="/wizard" 
          isActive={isRouteActive('/wizard')}
          onClick={onNavItemClick}
          collapsed={collapsed}
        >
          {t('sidebar.firstTimeWizard')}
        </NavItem>
        <NavItem 
          icon={LockIcon} 
          to="/profile" 
          isActive={isRouteActive('/profile')}
          onClick={onNavItemClick}
          collapsed={collapsed}
        >
          {t('sidebar.profile')}
        </NavItem>
      </VStack>
    </Box>
  )
}

export default Sidebar
