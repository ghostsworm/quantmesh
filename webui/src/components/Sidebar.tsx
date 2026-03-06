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
  AttachmentIcon,
  CheckCircleIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  WarningIcon,
  DownloadIcon,
} from '@chakra-ui/icons'
import { useSymbol } from '../contexts/SymbolContext'
import { useBot } from '../contexts/BotContext'
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

  // 只在收起状態時显示 Tooltip
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
  const { botId, bot } = useBot()
  const location = useLocation()
  const botPrefix = botId ? `/bots/${botId}` : ''
  const { t } = useTranslation()
  const isInBotMode = !!botId

  // 從 localStorage 读取收起状態，默认展开
  const [collapsed, setCollapsed] = useState(() => {
    if (isDrawer) return false // 移动端不收起
    const saved = localStorage.getItem(SIDEBAR_COLLAPSED_KEY)
    return saved === 'true'
  })

  // Bot 模式下使用不同的背景色，让用户感知「在 Bot 内」
  const bgColor = isInBotMode ? 'blue.50' : 'rgba(255, 255, 255, 0.8)'
  const borderColor = isInBotMode ? 'blue.200' : 'gray.100'
  
  // 宽度：收起時 64px，展开時 200px（比原来的 240px 更窄）
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
      // 清理時恢複默认值
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
              aria-label={collapsed ? t('sidebar.expandSidebar') : t('sidebar.collapseSidebar')}
              icon={collapsed ? <ChevronRightIcon /> : <ChevronLeftIcon />}
              size="sm"
              variant="ghost"
              onClick={toggleCollapse}
              borderRadius="md"
              _hover={{ bg: 'gray.100' }}
            />
          </Flex>
        )}

        {isInBotMode ? (
          /* ─── Bot 模式：仅显示 Bot 相关菜单 + 返回 ─── */
          <>
            <NavItem
              icon={ChevronLeftIcon}
              to="/bots"
              isActive={false}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.backToBotList')}
            </NavItem>
            {!collapsed && (
              <Box px="7" mb="2" mt={2}>
                <Heading size="xs" color="blue.500" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
                  {t('common.trading')}: {bot?.name || bot?.symbol || selectedSymbol || botId}
                </Heading>
              </Box>
            )}
            <NavItem
              icon={ViewIcon}
              to={`${botPrefix}/dashboard`}
              isActive={location.pathname === `${botPrefix}/dashboard`}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.tradingPanel')}
            </NavItem>
            <NavItem
              icon={DragHandleIcon}
              to={`${botPrefix}/positions`}
              isActive={location.pathname.startsWith(`${botPrefix}/positions`)}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.currentPositions')}
            </NavItem>
            <NavItem
              icon={RepeatIcon}
              to={`${botPrefix}/orders`}
              isActive={location.pathname.startsWith(`${botPrefix}/orders`)}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.orderManagement')}
            </NavItem>
            <NavItem
              icon={AddIcon}
              to={`${botPrefix}/slots`}
              isActive={location.pathname.startsWith(`${botPrefix}/slots`)}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.strategySlots')}
            </NavItem>
            <NavItem
              icon={CalendarIcon}
              to={`${botPrefix}/statistics`}
              isActive={location.pathname.startsWith(`${botPrefix}/statistics`)}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.profitStatistics')}
            </NavItem>
            <NavItem
              icon={SearchIcon}
              to={`${botPrefix}/reconciliation`}
              isActive={location.pathname.startsWith(`${botPrefix}/reconciliation`)}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.reconciliation')}
            </NavItem>
            <NavItem
              icon={LockIcon}
              to={`${botPrefix}/opening-control`}
              isActive={location.pathname.startsWith(`${botPrefix}/opening-control`)}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.openingControl')}
            </NavItem>
            <NavItem
              icon={StarIcon}
              to={`${botPrefix}/position-plan`}
              isActive={location.pathname.startsWith(`${botPrefix}/position-plan`)}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.positionPlan')}
            </NavItem>
            <NavItem
              icon={TimeIcon}
              to={`${botPrefix}/kline`}
              isActive={location.pathname.startsWith(`${botPrefix}/kline`)}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.klineDepth')}
            </NavItem>
            <Divider my={4} mx={collapsed ? "2" : "6"} borderColor={borderColor} />
            <NavItem
              icon={SettingsIcon}
              to={`${botPrefix}/config`}
              isActive={isRouteActive(`${botPrefix}/config`)}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.configManagement')}
            </NavItem>
          </>
        ) : (
          /* ─── 全局模式：完整菜单 ─── */
          <>
            {!collapsed && (
              <Box px="7" mb="2">
                <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
                  {t('sidebar.groupBotPlaza')}
                </Heading>
              </Box>
            )}
            <NavItem
              icon={RepeatIcon}
              to="/bots"
              isActive={isRouteActive('/bots')}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.botList')}
            </NavItem>
            {!collapsed && (
              <Box px="7" mb="2" mt={2}>
                <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
                  {t('sidebar.groupGlobalDashboard')}
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
              {t('sidebar.globalDashboard')}
            </NavItem>
            <NavItem
              icon={ViewIcon}
              to="/strategy-overview"
              isActive={isRouteActive('/strategy-overview')}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.strategyOverview')}
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
              icon={TriangleUpIcon}
              to="/risk"
              isActive={isRouteActive('/risk')}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.riskMonitor')}
            </NavItem>
            <NavItem
              icon={DownloadIcon}
              to="/profit-management"
              isActive={isRouteActive('/profit-management')}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.profitManagement')}
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
            {!collapsed && (
              <Box px="7" mb="2" mt={2}>
                <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
                  {t('common.global')} · {t('sidebar.groupBacktestData')}
                </Heading>
              </Box>
            )}
            <NavItem
              icon={TimeIcon}
              to="/backtest"
              isActive={isRouteActive('/backtest')}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.backtest')}
            </NavItem>
            <NavItem
              icon={ExternalLinkIcon}
              to="/data-export"
              isActive={isRouteActive('/data-export')}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.dataExport')}
            </NavItem>
            <NavItem
              icon={AttachmentIcon}
              to="/kline-files"
              isActive={isRouteActive('/kline-files')}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.klineFiles')}
            </NavItem>
            {!collapsed && (
              <Box px="7" mb="2" mt={2}>
                <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
                  {t('common.global')} · {t('sidebar.groupMarketData')}
                </Heading>
              </Box>
            )}
            <NavItem
              icon={SearchIcon}
              to="/news-analysis"
              isActive={isRouteActive('/news-analysis')}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.newsAnalysis')}
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
            {!collapsed && (
              <Box px="7" mb="2" mt={2}>
                <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
                  {t('common.global')} · {t('sidebar.groupAI')}
                </Heading>
              </Box>
            )}
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
              icon={WarningIcon}
              to="/newbie-risk-check"
              isActive={isRouteActive('/newbie-risk-check')}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.newbieRiskCheck')}
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
              icon={StarIcon}
              to="/ai/tasks"
              isActive={isRouteActive('/ai/tasks')}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.aiTasks')}
            </NavItem>
            {!collapsed && (
              <Box px="7" mb="2" mt={2}>
                <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
                  {t('common.global')} · {t('sidebar.groupStrategyCapital')}
                </Heading>
              </Box>
            )}
            <NavItem
              icon={CheckCircleIcon}
              to="/capital-management"
              isActive={isRouteActive('/capital-management')}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.capitalManagement')}
            </NavItem>
            <Divider my={4} mx={collapsed ? "2" : "6"} borderColor={borderColor} />
            {!collapsed && (
              <Box px="7" mb="2">
                <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
                  {t('sidebar.groupSystemSettings')}
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
              icon={CheckCircleIcon}
              to="/services/status"
              isActive={isRouteActive('/services/status')}
              onClick={onNavItemClick}
              collapsed={collapsed}
            >
              {t('sidebar.servicesStatus')}
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
          </>
        )}

        {/* 版本信息 */}
        {!collapsed && (
          <Box
            px="6"
            py="3"
            mt="auto"
            position="absolute"
            bottom="0"
            left="0"
            right="0"
            bg={isInBotMode ? 'blue.50' : 'rgba(255, 255, 255, 0.8)'}
            borderTop="1px solid"
            borderTopColor={borderColor}
          >
            <Text fontSize="xs" color="gray.400" fontWeight="medium">
              v{__APP_VERSION__}
            </Text>
            <Text fontSize="xs" color="gray.400" fontWeight="medium">
              {__GIT_HASH__}
            </Text>
          </Box>
        )}
      </VStack>
    </Box>
  )
}

export default Sidebar
