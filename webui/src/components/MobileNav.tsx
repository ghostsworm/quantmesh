import React from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { useBot } from '../contexts/BotContext'
import {
  Box,
  Flex,
  IconButton,
  Text,
  VStack,
  useColorModeValue,
} from '@chakra-ui/react'
import {
  ViewIcon,
  SettingsIcon,
  InfoIcon,
  TriangleUpIcon,
  ChevronLeftIcon,
} from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { isBotWorkspaceRoot } from '../utils/botRouteActive'

interface MobileNavProps {
  onMenuOpen?: () => void
}

/**
 * 移动端底部導航欄
 */
export const MobileNav: React.FC<MobileNavProps> = ({ onMenuOpen }) => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const { botId } = useBot()
  const botPrefix = botId ? `/bots/${botId}` : ''
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.700')
  const activeColor = useColorModeValue('blue.500', 'blue.300')
  const inactiveColor = useColorModeValue('gray.600', 'gray.400')

  const navItems = botId
    ? [
        { path: '/bots', icon: ChevronLeftIcon, label: t('sidebar.backToBotList', 'Back') },
        { path: botPrefix, icon: InfoIcon, label: t('sidebar.botDetail', 'Bot Details') },
        { path: `${botPrefix}/dashboard`, icon: ViewIcon, label: t('nav.dashboard', 'Dashboard') },
        { path: `${botPrefix}/positions`, icon: TriangleUpIcon, label: t('nav.positions', 'Positions') },
        { path: `${botPrefix}/config`, icon: SettingsIcon, label: t('nav.settings', 'Settings') },
      ]
    : [
        { path: '/', icon: ViewIcon, label: t('nav.dashboard', 'Dashboard') },
        { path: '/bots', icon: TriangleUpIcon, label: t('nav.positions', 'Positions') },
        { path: '/', icon: InfoIcon, label: t('nav.statistics', 'Statistics') },
        { path: '/config', icon: SettingsIcon, label: t('nav.settings', 'Settings') },
      ]

  const isActive = (path: string) => {
    if (path === '/') return location.pathname === '/'
    if (botId && path === botPrefix) {
      return isBotWorkspaceRoot(location.pathname, botPrefix)
    }
    return location.pathname === path || location.pathname.startsWith(path + '/')
  }

  return (
    <Box
      position="fixed"
      bottom="0"
      left="0"
      right="0"
      bg={bgColor}
      borderTop="1px"
      borderColor={borderColor}
      zIndex="sticky"
      pb="env(safe-area-inset-bottom)"
      boxShadow="0 -2px 10px rgba(0,0,0,0.1)"
    >
      <Flex justify="space-around" align="center" h="60px">
        {navItems.map((item) => {
          const active = isActive(item.path)
          const Icon = item.icon
          
          return (
            <VStack
              key={item.path}
              spacing={0}
              flex={1}
              cursor="pointer"
              onClick={() => navigate(item.path)}
              color={active ? activeColor : inactiveColor}
              _active={{ transform: 'scale(0.95)' }}
              transition="all 0.2s"
            >
              <IconButton
                aria-label={item.label}
                icon={<Icon />}
                variant="ghost"
                size="sm"
                color="inherit"
                _hover={{ bg: 'transparent' }}
              />
              <Text fontSize="xs" fontWeight={active ? 'bold' : 'normal'}>
                {item.label}
              </Text>
            </VStack>
          )
        })}
      </Flex>
    </Box>
  )
}

export default MobileNav

