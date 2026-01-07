import React from 'react'
import {
  Box,
  VStack,
  Icon,
  Text,
  Flex,
  Divider,
  Heading,
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
}

const NavItem: React.FC<NavItemProps> = ({ icon, children, to, isActive, onClick }) => {
  const activeBg = 'blue.50'
  const activeColor = 'blue.600'
  const hoverBg = 'gray.50'
  const textColor = 'gray.600'

  return (
    <MotionFlex
      as={RouterLink}
      to={to}
      align="center"
      px="4"
      py="2.5"
      mx="3"
      borderRadius="xl"
      role="group"
      cursor="pointer"
      bg={isActive ? activeBg : 'transparent'}
      color={isActive ? activeColor : textColor}
      onClick={onClick}
      whileHover={{ x: 4 }}
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
        mr="3"
        fontSize="18"
        as={icon}
        color={isActive ? activeColor : 'inherit'}
        _groupHover={{
          color: isActive ? activeColor : 'blue.500',
        }}
      />
      <Text fontSize="sm" fontWeight={isActive ? '600' : 'medium'} letterSpacing="tight">
        {children}
      </Text>
      
      {isActive && (
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
}

interface SidebarProps {
  onNavItemClick?: () => void
  isDrawer?: boolean
}

const Sidebar: React.FC<SidebarProps> = ({ onNavItemClick, isDrawer }) => {
  const { isGlobalView, selectedSymbol } = useSymbol()
  const location = useLocation()
  const { t } = useTranslation()
  
  const bgColor = 'rgba(255, 255, 255, 0.8)'
  const borderColor = 'gray.100'

  const isRouteActive = (path: string) => {
    if (path === '/' && location.pathname === '/') return true
    return path !== '/' && location.pathname.startsWith(path)
  }

  const menuTransition = {
    type: "spring",
    stiffness: 300,
    damping: 30
  }

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
      w={isDrawer ? 'full' : '240px'}
      zIndex="10"
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
        <Box px="7" mb="2">
          <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
            {t('common.global')}
          </Heading>
        </Box>
        <NavItem 
          icon={InfoIcon} 
          to="/" 
          isActive={isRouteActive('/') && isGlobalView}
          onClick={onNavItemClick}
        >
          {t('sidebar.overview')}
        </NavItem>
        <NavItem 
          icon={SettingsIcon} 
          to="/system-monitor" 
          isActive={isRouteActive('/system-monitor')}
          onClick={onNavItemClick}
        >
          {t('sidebar.performanceMonitor')}
        </NavItem>
        <NavItem 
          icon={BellIcon} 
          to="/events" 
          isActive={isRouteActive('/events')}
          onClick={onNavItemClick}
        >
          {t('sidebar.eventCenter')}
        </NavItem>
        <NavItem 
          icon={EditIcon} 
          to="/logs" 
          isActive={isRouteActive('/logs')}
          onClick={onNavItemClick}
        >
          {t('sidebar.runLogs')}
        </NavItem>
        <NavItem 
          icon={QuestionIcon} 
          to="/ai-prompts" 
          isActive={isRouteActive('/ai-prompts')}
          onClick={onNavItemClick}
        >
          {t('sidebar.aiPrompts')}
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
              <Divider my={4} mx="6" borderColor={borderColor} />
              
              <Box px="7" mb="2">
                <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
                  {t('common.trading')}: {selectedSymbol}
                </Heading>
              </Box>
              <NavItem 
                icon={ViewIcon} 
                to="/" 
                isActive={isRouteActive('/') && !isGlobalView}
                onClick={onNavItemClick}
              >
                {t('sidebar.tradingPanel')}
              </NavItem>
              <NavItem 
                icon={DragHandleIcon} 
                to="/positions" 
                isActive={isRouteActive('/positions')}
                onClick={onNavItemClick}
              >
                {t('sidebar.currentPositions')}
              </NavItem>
              <NavItem 
                icon={RepeatIcon} 
                to="/orders" 
                isActive={isRouteActive('/orders')}
                onClick={onNavItemClick}
              >
                {t('sidebar.orderManagement')}
              </NavItem>
              <NavItem 
                icon={AddIcon} 
                to="/slots" 
                isActive={isRouteActive('/slots')}
                onClick={onNavItemClick}
              >
                {t('sidebar.strategySlots')}
              </NavItem>
              <NavItem 
                icon={StarIcon} 
                to="/strategies" 
                isActive={isRouteActive('/strategies')}
                onClick={onNavItemClick}
              >
                {t('sidebar.strategyAllocation')}
              </NavItem>
              <NavItem 
                icon={CalendarIcon} 
                to="/statistics" 
                isActive={isRouteActive('/statistics')}
                onClick={onNavItemClick}
              >
                {t('sidebar.profitStatistics')}
              </NavItem>
              <NavItem 
                icon={SearchIcon} 
                to="/reconciliation" 
                isActive={isRouteActive('/reconciliation')}
                onClick={onNavItemClick}
              >
                {t('sidebar.reconciliation')}
              </NavItem>
              <NavItem 
                icon={TriangleUpIcon} 
                to="/risk" 
                isActive={isRouteActive('/risk')}
                onClick={onNavItemClick}
              >
                {t('sidebar.riskMonitor')}
              </NavItem>
              <NavItem 
                icon={TimeIcon} 
                to="/kline" 
                isActive={isRouteActive('/kline')}
                onClick={onNavItemClick}
              >
                {t('sidebar.klineDepth')}
              </NavItem>
              <NavItem 
                icon={AtSignIcon} 
                to="/funding-rate" 
                isActive={isRouteActive('/funding-rate')}
                onClick={onNavItemClick}
              >
                {t('sidebar.fundingRate')}
              </NavItem>
              <NavItem 
                icon={AtSignIcon} 
                to="/basis-monitor" 
                isActive={isRouteActive('/basis-monitor')}
                onClick={onNavItemClick}
              >
                {t('sidebar.basisMonitor')}
              </NavItem>
            </MotionBox>
          )}
        </AnimatePresence>

        <Divider my={4} mx="6" borderColor={borderColor} />

        <Box px="7" mb="2">
          <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
            {t('common.system')}
          </Heading>
        </Box>
        <NavItem 
          icon={SettingsIcon} 
          to="/config" 
          isActive={isRouteActive('/config')}
          onClick={onNavItemClick}
        >
          {t('sidebar.configManagement')}
        </NavItem>
        <NavItem 
          icon={LockIcon} 
          to="/profile" 
          isActive={isRouteActive('/profile')}
          onClick={onNavItemClick}
        >
          {t('sidebar.profile')}
        </NavItem>
      </VStack>
    </Box>
  )
}

export default Sidebar
