import React, { useState } from 'react'
import {
  Box,
  Container,
  Heading,
  VStack,
  Text,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Button,
  useToast,
} from '@chakra-ui/react'
import { StarIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import AIConfigWizard from './AIConfigWizard'

const AIConfigPage: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const [isWizardOpen, setIsWizardOpen] = useState(false)

  const handleSuccess = () => {
    setIsWizardOpen(false)
    toast({
      title: 'AI 配置已应用',
      description: '配置已成功保存，请重启服务使配置生效',
      status: 'success',
      duration: 5000,
    })
  }

  return (
    <Container maxW="4xl" py={8}>
      <VStack spacing={6} align="stretch">
        <Box>
          <Heading size="lg" mb={2}>
            AI 智能配置助手
          </Heading>
          <Text color="gray.600">
            根据您的资金和风险偏好，AI 将为您生成最优的网格交易参数和资金分配方案
          </Text>
        </Box>

        <Alert status="info" borderRadius="md">
          <AlertIcon />
          <Box>
            <AlertTitle>使用说明</AlertTitle>
            <AlertDescription fontSize="sm">
              此功能需要配置 Gemini API Key。您可以在设置页面中配置 Gemini API Key。
              AI 配置助手将根据您提供的资金、风险偏好和交易币种，自动生成最优的网格交易参数。
            </AlertDescription>
          </Box>
        </Alert>

        <Box
          p={8}
          bg="white"
          borderRadius="lg"
          boxShadow="sm"
          border="1px solid"
          borderColor="gray.200"
        >
          <VStack spacing={4}>
            <StarIcon boxSize={12} color="purple.500" />
            <Heading size="md">开始 AI 配置</Heading>
            <Text textAlign="center" color="gray.600" maxW="md">
              点击下方按钮打开 AI 配置助手，输入您的资金、风险偏好和交易币种，
              AI 将为您生成最优的配置方案。
            </Text>
            <Button
              leftIcon={<StarIcon />}
              colorScheme="purple"
              size="lg"
              onClick={() => setIsWizardOpen(true)}
            >
              打开 AI 配置助手
            </Button>
          </VStack>
        </Box>

        <Alert status="warning" borderRadius="md">
          <AlertIcon />
          <Box>
            <AlertTitle>注意事项</AlertTitle>
            <AlertDescription fontSize="sm">
              • AI 生成的配置仅供参考，请根据实际情况调整
              <br />
              • 应用配置后需要重启服务才能生效
              <br />
              • 建议在应用配置前备份当前配置
            </AlertDescription>
          </Box>
        </Alert>
      </VStack>

      <AIConfigWizard
        isOpen={isWizardOpen}
        onClose={() => setIsWizardOpen(false)}
        onSuccess={handleSuccess}
      />
    </Container>
  )
}

export default AIConfigPage
