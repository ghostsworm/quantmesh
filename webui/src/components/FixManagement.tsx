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
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
  Tabs,
  TabList,
  TabPanels,
  Tab,
  TabPanel,
  Button,
} from '@chakra-ui/react'
import { RepeatIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import {
  getFixSessions,
  getFixOrders,
  fixLogout,
  FixSessionItem,
  FixOrderLinkItem,
} from '../services/api'

const FixManagement: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const [loadingSessions, setLoadingSessions] = useState(true)
  const [loadingOrders, setLoadingOrders] = useState(true)
  const [sessions, setSessions] = useState<FixSessionItem[]>([])
  const [orders, setOrders] = useState<FixOrderLinkItem[]>([])
  const [loggingOut, setLoggingOut] = useState<string | null>(null)

  const loadSessions = async () => {
    setLoadingSessions(true)
    try {
      const r = await getFixSessions(100, 0)
      if (r?.sessions) setSessions(r.sessions)
    } catch (e) {
      toast({
        title: t('common.loadFailed'),
        description: (e as Error)?.message,
        status: 'error',
      })
    } finally {
      setLoadingSessions(false)
    }
  }

  const loadOrders = async () => {
    setLoadingOrders(true)
    try {
      const r = await getFixOrders({ limit: 100, offset: 0 })
      if (r?.orders) setOrders(r.orders)
    } catch (e) {
      toast({
        title: t('common.loadFailed'),
        description: (e as Error)?.message,
        status: 'error',
      })
    } finally {
      setLoadingOrders(false)
    }
  }

  const handleLogout = async (sessionId: string) => {
    setLoggingOut(sessionId)
    try {
      await fixLogout(sessionId)
      toast({ title: t('fixManagement.logoutSuccess'), status: 'success' })
      loadSessions()
    } catch (e) {
      toast({
        title: t('fixManagement.logoutFailed'),
        description: (e as Error)?.message,
        status: 'error',
      })
    } finally {
      setLoggingOut(null)
    }
  }

  useEffect(() => {
    loadSessions()
    loadOrders()
  }, [])

  return (
    <Container maxW="container.xl" py={6}>
      <HStack justify="space-between" mb={6}>
        <Heading size="md">{t('fixManagement.title')}</Heading>
      </HStack>
      <Text fontSize="sm" color="gray.500" mb={4}>
        {t('fixManagement.hint')}
      </Text>

      <Tabs variant="enclosed">
        <TabList>
          <Tab>{t('fixManagement.sessions')}</Tab>
          <Tab>{t('fixManagement.orders')}</Tab>
        </TabList>
        <TabPanels>
          <TabPanel px={0}>
            <HStack justify="flex-end" mb={3}>
              <IconButton
                aria-label={t('common.refresh')}
                icon={<RepeatIcon />}
                size="sm"
                variant="outline"
                onClick={loadSessions}
                isLoading={loadingSessions}
              />
            </HStack>
            {loadingSessions && !sessions.length ? (
              <Box py={8} textAlign="center">
                <Spinner size="lg" />
              </Box>
            ) : sessions.length === 0 ? (
              <Text color="gray.500" py={8}>
                {t('fixManagement.noSessions')}
              </Text>
            ) : (
              <TableContainer>
                <Table size="sm">
                  <Thead>
                    <Tr>
                      <Th>{t('fixManagement.sessionId')}</Th>
                      <Th>{t('fixManagement.botId')}</Th>
                      <Th>{t('fixManagement.role')}</Th>
                      <Th>{t('fixManagement.status')}</Th>
                      <Th>{t('fixManagement.lastHeartbeat')}</Th>
                      <Th>{t('common.actions')}</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {sessions.map((s) => (
                      <Tr key={s.session_id}>
                        <Td fontFamily="mono" fontSize="xs">
                          {s.session_id}
                        </Td>
                        <Td>{s.bot_id || '-'}</Td>
                        <Td>{s.role || '-'}</Td>
                        <Td>
                          <Badge colorScheme={s.is_logged_on ? 'green' : 'gray'}>
                            {s.is_logged_on ? t('fixManagement.loggedOn') : t('fixManagement.loggedOut')}
                          </Badge>
                        </Td>
                        <Td fontSize="xs">{s.last_heartbeat_at || '-'}</Td>
                        <Td>
                          {s.is_logged_on && (
                            <Button
                              size="xs"
                              variant="outline"
                              colorScheme="red"
                              onClick={() => handleLogout(s.session_id)}
                              isLoading={loggingOut === s.session_id}
                            >
                              {t('fixManagement.logout')}
                            </Button>
                          )}
                        </Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </TableContainer>
            )}
          </TabPanel>
          <TabPanel px={0}>
            <HStack justify="flex-end" mb={3}>
              <IconButton
                aria-label={t('common.refresh')}
                icon={<RepeatIcon />}
                size="sm"
                variant="outline"
                onClick={loadOrders}
                isLoading={loadingOrders}
              />
            </HStack>
            {loadingOrders && !orders.length ? (
              <Box py={8} textAlign="center">
                <Spinner size="lg" />
              </Box>
            ) : orders.length === 0 ? (
              <Text color="gray.500" py={8}>
                {t('fixManagement.noOrders')}
              </Text>
            ) : (
              <TableContainer overflowX="auto">
                <Table size="sm">
                  <Thead>
                    <Tr>
                      <Th>{t('fixManagement.clOrdId')}</Th>
                      <Th>{t('fixManagement.sessionId')}</Th>
                      <Th>{t('fixManagement.symbol')}</Th>
                      <Th>{t('fixManagement.side')}</Th>
                      <Th>{t('fixManagement.ordStatus')}</Th>
                      <Th>{t('fixManagement.internalOrderId')}</Th>
                      <Th>{t('fixManagement.updatedAt')}</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {orders.map((o) => (
                      <Tr key={`${o.session_id}-${o.cl_ord_id}`}>
                        <Td fontFamily="mono">{o.cl_ord_id}</Td>
                        <Td fontSize="xs">{o.session_id}</Td>
                        <Td>{o.symbol}</Td>
                        <Td>{o.side}</Td>
                        <Td>
                          <Badge
                            colorScheme={
                              o.ord_status === 'FILLED'
                                ? 'green'
                                : o.ord_status === 'CANCELED' || o.ord_status === 'REJECTED'
                                  ? 'red'
                                  : 'blue'
                            }
                          >
                            {o.ord_status}
                          </Badge>
                        </Td>
                        <Td>{o.internal_order_id}</Td>
                        <Td fontSize="xs">{o.updated_at}</Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </TableContainer>
            )}
          </TabPanel>
        </TabPanels>
      </Tabs>
    </Container>
  )
}

export default FixManagement
