import React, { useEffect, useState } from 'react'
import {
  Box,
  Container,
  Heading,
  Text,
  VStack,
  HStack,
  Badge,
  Spinner,
  Card,
  CardBody,
  CardHeader,
  useToast,
  IconButton,
  Icon,
} from '@chakra-ui/react'
import { CheckCircleIcon, WarningIcon, RepeatIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { getServicesStatus, ServiceStatusItem } from '../services/api'

const ServiceStatusPage: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const [loading, setLoading] = useState(true)
  const [services, setServices] = useState<ServiceStatusItem[]>([])

  const load = async () => {
    setLoading(true)
    try {
      const r = await getServicesStatus()
      if (r?.services) setServices(r.services)
    } catch (e) {
      toast({
        title: t('common.loadFailed'),
        description: (e as Error)?.message,
        status: 'error',
      })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  return (
    <Container maxW="container.md" py={6}>
      <HStack justify="space-between" mb={6}>
        <Heading size="md">{t('servicesStatus.title')}</Heading>
        <IconButton
          aria-label={t('common.refresh')}
          icon={<RepeatIcon />}
          size="sm"
          variant="outline"
          onClick={load}
          isLoading={loading}
        />
      </HStack>
      <Text fontSize="sm" color="gray.500" mb={4}>
        {t('servicesStatus.hint', ))}
      </Text>

      {loading && !services.length ? (
        <Box py={8} textAlign="center">
          <Spinner size="lg" />
        </Box>
      ) : (
        <VStack align="stretch" spacing={4}>
          {services.map((s) => (
            <Card key={s.id} size="sm" borderWidth="1px" borderColor={s.ok ? 'gray.100' : 'red.100'}>
              <CardHeader py={3}>
                <HStack justify="space-between">
                  <Text fontWeight="600">{s.name}</Text>
                  <Badge colorScheme={s.ok ? 'green' : 'red'} fontSize="xs">
                    {s.ok ? t('servicesStatus.ok') : t('servicesStatus.unavailable')}
                  </Badge>
                </HStack>
              </CardHeader>
              <CardBody pt={0} pb={3}>
                <HStack align="flex-start" spacing={2}>
                  <Icon
                    as={s.ok ? CheckCircleIcon : WarningIcon}
                    color={s.ok ? 'green.500' : 'red.500'}
                    boxSize={4}
                    mt={0.5}
                  />
                  <Text fontSize="sm" color={s.ok ? 'gray.600' : 'red.600'}>
                    {s.message || (s.ok ? t('servicesStatus.normal') : t('servicesStatus.checkHint'))}
                  </Text>
                </HStack>
              </CardBody>
            </Card>
          ))}
        </VStack>
      )}
    </Container>
  )
}

export default ServiceStatusPage
