import React, { useState, useEffect } from 'react'
import {
  Box,
  VStack,
  HStack,
  Text,
  Slider,
  SliderTrack,
  SliderFilledTrack,
  SliderThumb,
  SliderMark,
  Input,
  InputGroup,
  InputRightAddon,
  Switch,
  FormControl,
  FormLabel,
  Tooltip,
  useColorModeValue,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'

interface CapitalSliderProps {
  strategyId: string
  strategyName: string
  currentValue: number
  maxValue: number
  totalCapital: number
  percentage: number
  onChange: (strategyId: string, value: number, isPercentage: boolean) => void
  isPercentageMode?: boolean
  onModeChange?: (isPercentage: boolean) => void
  disabled?: boolean
}

const CapitalSlider: React.FC<CapitalSliderProps> = ({
  strategyId,
  strategyName,
  currentValue,
  maxValue,
  totalCapital,
  percentage,
  onChange,
  isPercentageMode = false,
  onModeChange,
  disabled = false,
}) => {
  const { t } = useTranslation()
  const [showTooltip, setShowTooltip] = useState(false)
  const [localValue, setLocalValue] = useState(isPercentageMode ? percentage : currentValue)
  const [inputValue, setInputValue] = useState(isPercentageMode ? percentage.toString() : currentValue.toString())

  const trackBg = useColorModeValue('gray.100', 'gray.700')
  const filledBg = useColorModeValue('blue.500', 'blue.400')

  useEffect(() => {
    setLocalValue(isPercentageMode ? percentage : currentValue)
    setInputValue(isPercentageMode ? percentage.toFixed(1) : currentValue.toFixed(2))
  }, [isPercentageMode, percentage, currentValue])

  const handleSliderChange = (value: number) => {
    setLocalValue(value)
    if (isPercentageMode) {
      setInputValue(value.toFixed(1))
    } else {
      setInputValue(value.toFixed(2))
    }
  }

  const handleSliderChangeEnd = (value: number) => {
    onChange(strategyId, value, isPercentageMode)
  }

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setInputValue(e.target.value)
  }

  const handleInputBlur = () => {
    const numValue = parseFloat(inputValue) || 0
    const clampedValue = Math.min(Math.max(numValue, 0), isPercentageMode ? 100 : totalCapital)
    setLocalValue(clampedValue)
    setInputValue(isPercentageMode ? clampedValue.toFixed(1) : clampedValue.toFixed(2))
    onChange(strategyId, clampedValue, isPercentageMode)
  }

  const getSliderMax = () => {
    if (isPercentageMode) return 100
    return totalCapital
  }

  const getDisplayValue = () => {
    if (isPercentageMode) {
      return `${localValue.toFixed(1)}% (${((localValue / 100) * totalCapital).toFixed(2)} USDT)`
    }
    return `${localValue.toFixed(2)} USDT (${((localValue / totalCapital) * 100).toFixed(1)}%)`
  }

  return (
    <Box
      p={4}
      borderWidth="1px"
      borderRadius="lg"
      borderColor={useColorModeValue('gray.200', 'gray.600')}
      bg={useColorModeValue('white', 'gray.800')}
      opacity={disabled ? 0.6 : 1}
    >
      <VStack align="stretch" spacing={3}>
        <HStack justify="space-between">
          <Text fontWeight="bold" fontSize="sm">
            {strategyName}
          </Text>
          {onModeChange && (
            <HStack spacing={2}>
              <Text fontSize="xs" color="gray.500">
                USDT
              </Text>
              <Switch
                size="sm"
                isChecked={isPercentageMode}
                onChange={(e) => onModeChange(e.target.checked)}
                isDisabled={disabled}
              />
              <Text fontSize="xs" color="gray.500">
                %
              </Text>
            </HStack>
          )}
        </HStack>

        <Slider
          id={`slider-${strategyId}`}
          value={localValue}
          min={0}
          max={getSliderMax()}
          step={isPercentageMode ? 0.1 : 1}
          onChange={handleSliderChange}
          onChangeEnd={handleSliderChangeEnd}
          onMouseEnter={() => setShowTooltip(true)}
          onMouseLeave={() => setShowTooltip(false)}
          isDisabled={disabled}
        >
          <SliderMark value={0} mt="2" ml="-2" fontSize="xs" color="gray.500">
            0
          </SliderMark>
          <SliderMark
            value={getSliderMax() / 2}
            mt="2"
            ml="-4"
            fontSize="xs"
            color="gray.500"
          >
            {isPercentageMode ? '50%' : `${(totalCapital / 2).toFixed(0)}`}
          </SliderMark>
          <SliderMark value={getSliderMax()} mt="2" ml="-6" fontSize="xs" color="gray.500">
            {isPercentageMode ? '100%' : `${totalCapital.toFixed(0)}`}
          </SliderMark>
          <SliderTrack bg={trackBg}>
            <SliderFilledTrack bg={filledBg} />
          </SliderTrack>
          <Tooltip
            hasArrow
            bg="blue.500"
            color="white"
            placement="top"
            isOpen={showTooltip}
            label={getDisplayValue()}
          >
            <SliderThumb boxSize={4} />
          </Tooltip>
        </Slider>

        <HStack justify="space-between" mt={4}>
          <InputGroup size="sm" maxW="150px">
            <Input
              type="number"
              value={inputValue}
              onChange={handleInputChange}
              onBlur={handleInputBlur}
              isDisabled={disabled}
              textAlign="right"
            />
            <InputRightAddon>{isPercentageMode ? '%' : 'USDT'}</InputRightAddon>
          </InputGroup>
          <Text fontSize="sm" color="gray.500">
            {t('capitalManagement.allocated')}: {getDisplayValue()}
          </Text>
        </HStack>
      </VStack>
    </Box>
  )
}

export default CapitalSlider
