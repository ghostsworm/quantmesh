import React from 'react'
import { Box, Flex, Text, Tooltip, Icon } from '@chakra-ui/react'
import { ExternalLinkIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { useOpenSourceUpdate } from '../hooks/useOpenSourceUpdate'

interface SidebarUpdateHintProps {
  collapsed: boolean
  isDrawer?: boolean
  onOpenExternal?: () => void
}

const SidebarUpdateHint: React.FC<SidebarUpdateHintProps> = ({
  collapsed,
  isDrawer,
  onOpenExternal,
}) => {
  const { t } = useTranslation()
  const { hasUpdate, remoteTag, repoUrl, loading } = useOpenSourceUpdate()

  if (loading || !hasUpdate) {
    return null
  }

  const tooltip = t('sidebar.openSourceUpdateTooltip', {
    version: remoteTag ?? '',
  })
  const aria = t('sidebar.openSourceUpdateAria', {
    version: remoteTag ?? '',
  })

  const openRepo = () => {
    onOpenExternal?.()
    window.open(repoUrl, '_blank', 'noopener,noreferrer')
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      openRepo()
    }
  }

  if (collapsed) {
    return (
      <Flex justify="center" px={2} pb={3} pt={1}>
        <Tooltip label={tooltip} placement="right" hasArrow>
          <Flex
            as="button"
            type="button"
            align="center"
            justify="center"
            w="32px"
            h="32px"
            borderRadius="md"
            bg="orange.50"
            borderWidth="1px"
            borderColor="orange.200"
            cursor="pointer"
            aria-label={aria}
            onClick={openRepo}
            onKeyDown={onKeyDown}
            _hover={{ bg: 'orange.100' }}
          >
            <Box w="8px" h="8px" borderRadius="full" bg="orange.400" aria-hidden />
          </Flex>
        </Tooltip>
      </Flex>
    )
  }

  return (
    <Box px={3} pb={isDrawer ? 4 : 3} pt={2} flexShrink={0}>
      <Flex
        as="button"
        type="button"
        w="100%"
        align="center"
        gap={2}
        px={3}
        py={2}
        borderRadius="lg"
        bg="orange.50"
        borderWidth="1px"
        borderColor="orange.200"
        cursor="pointer"
        textAlign="left"
        onClick={openRepo}
        onKeyDown={onKeyDown}
        aria-label={aria}
        _hover={{ bg: 'orange.100', borderColor: 'orange.300' }}
        transition="background 0.15s"
      >
        <Box flex="1" minW={0}>
          <Text fontSize="xs" fontWeight="semibold" color="orange.800" lineHeight="short">
            {t('sidebar.openSourceUpdateShort')}
          </Text>
          {remoteTag ? (
            <Text fontSize="10px" color="orange.700" noOfLines={1}>
              {remoteTag}
            </Text>
          ) : null}
        </Box>
        <Icon as={ExternalLinkIcon} color="orange.600" boxSize={4} flexShrink={0} aria-hidden />
      </Flex>
    </Box>
  )
}

export default SidebarUpdateHint
