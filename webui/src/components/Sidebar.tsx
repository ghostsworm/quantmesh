import React from 'react'
import {
  Box,
  VStack,
  Icon,
  Text,
  Flex,
  Divider,
  Heading,
  useColorModeValue,
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
} from '@chakra-ui/icons'
import { useSymbol } from '../contexts/SymbolContext'

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
  const activeBg = useColorModeValue('blue.50', 'rgba(66, 153, 225, 0.15)')
  const activeColor = useColorModeValue('blue.600', 'blue.300')
  const hoverBg = useColorModeValue('gray.50', 'rgba(255, 255, 255, 0.05)')
  const textColor = useColorModeValue('gray.600', 'gray.400')

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
        color: isActive ? activeColor : useColorModeValue('gray.900', 'white'),
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
          color: isActive ? activeColor : useColorModeValue('blue.500', 'blue.200'),
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

const Sidebar: React.FC = () => {
  const { isGlobalView, selectedSymbol } = useSymbol()
  const location = useLocation()
  
  const bgColor = useColorModeValue('rgba(255, 255, 255, 0.8)', 'rgba(26, 32, 44, 0.8)')
  const borderColor = useColorModeValue('gray.100', 'rgba(255, 255, 255, 0.08)')

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
      pos="fixed"
      left="0"
      h="calc(100vh - 64px)"
      top="64px"
      pb="10"
      overflowX="hidden"
      overflowY="auto"
      bg={bgColor}
      backdropFilter="blur(20px)"
      borderRight="1px"
      borderRightColor={borderColor}
      w="240px"
      zIndex="10"
      display={{ base: 'none', md: 'block' }}
      css={{
        '&::-webkit-scrollbar': {
          width: '4px',
        },
        '&::-webkit-scrollbar-track': {
          width: '6px',
        },
        '&::-webkit-scrollbar-thumb': {
          background: useColorModeValue('rgba(0,0,0,0.05)', 'rgba(255,255,255,0.05)'),
          borderRadius: '24px',
        },
      }}
    >
      <VStack align="stretch" spacing={1} mt={5}>
        <Box px="7" mb="2">
          <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
            Global
          </Heading>
        </Box>
        <NavItem 
          icon={InfoIcon} 
          to="/" 
          isActive={isRouteActive('/') && isGlobalView}
        >
          概览
        </NavItem>
        <NavItem 
          icon={SettingsIcon} 
          to="/system-monitor" 
          isActive={isRouteActive('/system-monitor')}
        >
          性能监控
        </NavItem>
        <NavItem 
          icon={EditIcon} 
          to="/logs" 
          isActive={isRouteActive('/logs')}
        >
          运行日志
        </NavItem>
        <NavItem 
          icon={QuestionIcon} 
          to="/ai-prompts" 
          isActive={isRouteActive('/ai-prompts')}
        >
          AI 提示词
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
                  Trading: {selectedSymbol}
                </Heading>
              </Box>
              <NavItem 
                icon={ViewIcon} 
                to="/" 
                isActive={isRouteActive('/') && !isGlobalView}
              >
                交易面板
              </NavItem>
              <NavItem 
                icon={DragHandleIcon} 
                to="/positions" 
                isActive={isRouteActive('/positions')}
              >
                当前持仓
              </NavItem>
              <NavItem 
                icon={RepeatIcon} 
                to="/orders" 
                isActive={isRouteActive('/orders')}
              >
                订单管理
              </NavItem>
              <NavItem 
                icon={AddIcon} 
                to="/slots" 
                isActive={isRouteActive('/slots')}
              >
                策略槽位
              </NavItem>
              <NavItem 
                icon={StarIcon} 
                to="/strategies" 
                isActive={isRouteActive('/strategies')}
              >
                策略配比
              </NavItem>
              <NavItem 
                icon={CalendarIcon} 
                to="/statistics" 
                isActive={isRouteActive('/statistics')}
              >
                收益统计
              </NavItem>
              <NavItem 
                icon={SearchIcon} 
                to="/reconciliation" 
                isActive={isRouteActive('/reconciliation')}
              >
                对账校验
              </NavItem>
              <NavItem 
                icon={TriangleUpIcon} 
                to="/risk" 
                isActive={isRouteActive('/risk')}
              >
                风控监控
              </NavItem>
              <NavItem 
                icon={TimeIcon} 
                to="/kline" 
                isActive={isRouteActive('/kline')}
              >
                K线深度
              </NavItem>
              <NavItem 
                icon={AtSignIcon} 
                to="/funding-rate" 
                isActive={isRouteActive('/funding-rate')}
              >
                资金费率
              </NavItem>
            </MotionBox>
          )}
        </AnimatePresence>

        <Divider my={4} mx="6" borderColor={borderColor} />

        <Box px="7" mb="2">
          <Heading size="xs" color="gray.400" textTransform="uppercase" letterSpacing="0.1em" fontSize="10px">
            System
          </Heading>
        </Box>
        <NavItem 
          icon={SettingsIcon} 
          to="/config" 
          isActive={isRouteActive('/config')}
        >
          配置管理
        </NavItem>
        <NavItem 
          icon={LockIcon} 
          to="/profile" 
          isActive={isRouteActive('/profile')}
        >
          个人资料
        </NavItem>
      </VStack>
    </Box>
  )
}

export default Sidebar
