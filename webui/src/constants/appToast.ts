import type { UseToastOptions } from '@chakra-ui/react'

/**
 * 全站統一 Toast：右下角浮入，與主界面其餘操作反饋一致。
 * Unified toast: bottom-right, matches the rest of the app feedback.
 */
export const DEFAULT_APP_TOAST_OPTIONS: Partial<UseToastOptions> = {
  position: 'bottom-right',
  duration: 4500,
  isClosable: true,
}
