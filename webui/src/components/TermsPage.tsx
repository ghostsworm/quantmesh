import React from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Box,
  Container,
  Flex,
  VStack,
  Heading,
  Text,
  Button,
  HStack,
} from '@chakra-ui/react'
import { ArrowBackIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../contexts/AuthContext'
import LanguageSelector from './LanguageSelector'

const TermsPage: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { isAuthenticated } = useAuth()

  const sections = t('terms.sections', { returnObjects: true }) as Array<{
    title: string
    content: string
  }>

  return (
    <Box
      minH="100vh"
      display="flex"
      flexDirection="column"
      bg="gray.50"
      position="relative"
    >
      {/* Top bar: Logo, language selector, back button */}
      <Box
        position="sticky"
        top={0}
        zIndex={10}
        bg="white"
        borderBottom="1px"
        borderColor="gray.200"
        py={3}
      >
        <Container maxW="container.lg">
          <Flex justify="space-between" align="center">
            <HStack spacing={4}>
              <Button
                leftIcon={<ArrowBackIcon />}
                variant="ghost"
                size="sm"
                onClick={() => (isAuthenticated ? navigate('/') : navigate('/login'))}
              >
                {isAuthenticated ? t('terms.backToApp') : t('terms.backToLogin')}
              </Button>
              <Heading size="sm" fontWeight="700" color="blue.600">
                QuantMesh
              </Heading>
            </HStack>
            <LanguageSelector />
          </Flex>
        </Container>
      </Box>

      <Container maxW="container.lg" py={8} flex="1">
        <VStack align="stretch" spacing={8}>
          <Heading size="lg">{t('terms.title')}</Heading>
          <Text color="gray.500" fontSize="sm">
            {t('terms.lastUpdated')}
          </Text>

          {Array.isArray(sections) &&
            sections.map((section, idx) => (
              <Box key={idx}>
                <Heading size="md" mb={2}>
                  {section.title}
                </Heading>
                <Text fontSize="md" lineHeight="tall" color="gray.700" whiteSpace="pre-line">
                  {section.content}
                </Text>
              </Box>
            ))}
        </VStack>
      </Container>
    </Box>
  )
}

export default TermsPage
