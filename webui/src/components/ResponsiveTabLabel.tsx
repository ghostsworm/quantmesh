import { Box, HStack, Text, Tooltip, useBreakpointValue } from '@chakra-ui/react'
import type { ReactNode } from 'react'

/**
 * 窄屏（< md）時：未選中分頁只顯示圖標；當前選中分頁顯示圖標 + 完整標題。
 * 寬屏：始終顯示圖標 + 標題。
 */
export function useCompactConfigTabs(): boolean {
  const v = useBreakpointValue({ base: true, md: false })
  return v ?? false
}

type ResponsiveTabLabelProps = {
  icon: ReactNode
  label: string
  selected: boolean
  compact: boolean
}

export function ResponsiveTabLabel({ icon, label, selected, compact }: ResponsiveTabLabelProps) {
  const showText = !compact || selected
  const inner = (
    <HStack spacing={2} as="span" justify="center" align="center">
      <Box as="span" display="flex" flexShrink={0} alignItems="center" justifyContent="center">
        {icon}
      </Box>
      {showText && (
        <Text as="span" fontSize="sm" fontWeight="600" whiteSpace="nowrap" lineHeight="1.2">
          {label}
        </Text>
      )}
    </HStack>
  )

  if (compact && !selected) {
    return (
      <Tooltip label={label} hasArrow placement="bottom" openDelay={300}>
        <Box as="span" display="inline-flex">
          {inner}
        </Box>
      </Tooltip>
    )
  }
  return inner
}
