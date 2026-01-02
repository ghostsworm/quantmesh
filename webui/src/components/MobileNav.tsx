import React from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
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
} from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'

interface MobileNavProps {
  onMenuOpen?: () => void
}

/**
 * 移动端底部导航栏
 */
export const MobileNav: React.FC<MobileNavProps> = ({ onMenuOpen }) => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.700')
  const activeColor = useColorModeValue('blue.500', 'blue.300')
  const inactiveColor = useColorModeValue('gray.600', 'gray.400')

  const navItems = [
    {
      path: '/',
      icon: ViewIcon,
      label: t('nav.dashboard', '仪表盘'),
    },
    {
      path: '/positions',
      icon: TriangleUpIcon,
      label: t('nav.positions', '持仓'),
    },
    {
      path: '/statistics',
      icon: InfoIcon,
      label: t('nav.statistics', '统计'),
    },
    {
      path: '/configuration',
      icon: SettingsIcon,
      label: t('nav.settings', '设置'),
    },
  ]

  const isActive = (path: string) => {
    if (path === '/') {
      return location.pathname === '/'
    }
    return location.pathname.startsWith(path)
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

