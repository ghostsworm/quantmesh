/**
 * 支持小数输入的 NumberInput 封装
 * Chakra NumberInput 在受控模式下，输入 "3." 时 valueAsNumber 会变成 3 导致小数点丢失
 * 本组件保留字符串中间态，确保能正常输入小数
 */
import React from 'react'
import {
  NumberInput,
  NumberInputField,
  NumberInputStepper,
  NumberIncrementStepper,
  NumberDecrementStepper,
  type NumberInputProps,
} from '@chakra-ui/react'

export type DecimalNumberInputValue = number | string | undefined

export interface DecimalNumberInputProps extends Omit<NumberInputProps, 'onChange' | 'value' | 'onBlur'> {
  value?: DecimalNumberInputValue
  onChange?: (value: number | string | undefined) => void
  /** blur 时将字符串转为数字，便于父组件持久化 */
  onBlur?: () => void
  showStepper?: boolean
  placeholder?: string
}

/** 解析小数输入，保留 "3." 等中间态字符串，供单元测试使用 */
export function parseDecimalValue(
  valueAsString: string,
  valueAsNumber: number
): number | string | undefined {
  const isPartial =
    valueAsString !== '' &&
    (valueAsString.endsWith('.') || valueAsString !== String(valueAsNumber))
  if (isPartial) return valueAsString
  if (!Number.isNaN(valueAsNumber)) return valueAsNumber
  return valueAsString !== '' ? valueAsString : undefined
}

export const DecimalNumberInput: React.FC<DecimalNumberInputProps> = ({
  value,
  onChange,
  onBlur,
  showStepper = false,
  step = 0.01,
  precision = 2,
  placeholder,
  ...rest
}) => {
  const displayValue =
    value !== undefined && value !== '' ? (value as number | string) : undefined

  const handleBlur = () => {
    if (typeof value === 'string') {
      const n = parseFloat(value)
      if (!Number.isNaN(n)) onChange?.(n)
    }
    onBlur?.()
  }

  return (
    <NumberInput
      {...rest}
      value={displayValue}
      step={step}
      precision={precision}
      onChange={(valueAsString: string, valueAsNumber: number) => {
        onChange?.(parseDecimalValue(valueAsString, valueAsNumber))
      }}
      onBlur={handleBlur}
    >
      <NumberInputField inputMode="decimal" placeholder={placeholder} />
      {showStepper && (
        <NumberInputStepper>
          <NumberIncrementStepper />
          <NumberDecrementStepper />
        </NumberInputStepper>
      )}
    </NumberInput>
  )
}

export default DecimalNumberInput
