import React, { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { getLogs, LogEntry, subscribeLogs, cleanLogs, getLogStats, vacuumLogs, LogStats, getEventCenterStatus, setEventCenterStatus } from '../services/api'
import './Logs.css'
import {
  Button,
  useToast,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  useDisclosure,
  FormControl,
  FormLabel,
  NumberInput,
  NumberInputField,
  Checkbox,
  CheckboxGroup,
  VStack,
  Text,
  Alert,
  AlertIcon,
  AlertDescription,
  Switch,
  HStack,
  Badge,
  Box,
} from '@chakra-ui/react'

// Alias for backward compatibility
type LogRecord = LogEntry

const Logs: React.FC = () => {
  const { t } = useTranslation()
  const [logs, setLogs] = useState<LogRecord[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [limit] = useState(100)
  
  // 過滤条件
  const [level, setLevel] = useState<string>('')
  const [keyword, setKeyword] = useState<string>('')
  const [startTime, setStartTime] = useState<string>('')
  const [endTime, setEndTime] = useState<string>('')
  
  // 實時更新
  const [realtimeEnabled, setRealtimeEnabled] = useState(true)
  const [autoScroll, setAutoScroll] = useState(true)
  
  const logsEndRef = useRef<HTMLDivElement>(null)
  const unsubscribeRef = useRef<(() => void) | null>(null)

  // 加載日志
  const loadLogs = async () => {
    setLoading(true)
    setError(null)
    
    try {
      const params: any = {
        limit,
        offset: (page - 1) * limit,
      }
      
      if (level) params.level = level
      if (keyword) params.keyword = keyword
      if (startTime) params.start_time = new Date(startTime).toISOString()
      if (endTime) params.end_time = new Date(endTime).toISOString()
      
      const response = await getLogs(params)
      setLogs(response.logs)
      setTotal(response.total)
    } catch (err: any) {
      setError(err.message || t('logs.loadFailed'))
    } finally {
      setLoading(false)
    }
  }

  // 初始化加載
  useEffect(() => {
    loadLogs()
  }, [page, level, keyword, startTime, endTime])

  // 實時日志订阅
  useEffect(() => {
    if (!realtimeEnabled) {
      if (unsubscribeRef.current) {
        unsubscribeRef.current()
        unsubscribeRef.current = null
      }
      return
    }

    const unsubscribe = subscribeLogs(
      (log) => {
        setLogs((prevLogs) => {
          // 將新日志添加到列表末尾（因為日志是按時间倒序排列的，新日志应該在前面）
          // 但為了實時更新体驗，我们添加到开头
          const newLogs = [log, ...prevLogs]
          // 限制最多保留1000条
          return newLogs.slice(0, 1000)
        })
        setTotal((prev) => prev + 1)
      },
      (err) => {
        console.error('WebSocket error:', err)
        setError(t('logs.realtimeFailed'))
      }
    )

    unsubscribeRef.current = unsubscribe

    return () => {
      if (unsubscribeRef.current) {
        unsubscribeRef.current()
        unsubscribeRef.current = null
      }
    }
  }, [realtimeEnabled])

  // 自动滚动到底部
  useEffect(() => {
    if (autoScroll && logsEndRef.current) {
      logsEndRef.current.scrollIntoView({ behavior: 'smooth' })
    }
  }, [logs, autoScroll])

  // 獲取日志级别样式
  const getLevelClass = (level: string) => {
    switch (level.toUpperCase()) {
      case 'DEBUG':
        return 'log-level-debug'
      case 'INFO':
        return 'log-level-info'
      case 'WARN':
        return 'log-level-warn'
      case 'ERROR':
        return 'log-level-error'
      case 'FATAL':
        return 'log-level-fatal'
      default:
        return ''
    }
  }

  // 格式化時间
  const formatTime = (timestamp: string) => {
    const date = new Date(timestamp)
    return date.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false,
    })
  }

  // 重置過滤条件
  const handleReset = () => {
    setLevel('')
    setKeyword('')
    setStartTime('')
    setEndTime('')
    setPage(1)
  }

  // 清空日志列表
  const handleClear = () => {
    setLogs([])
    setTotal(0)
  }

  // 日志清理相关
  const { isOpen: isCleanOpen, onOpen: onCleanOpen, onClose: onCleanClose } = useDisclosure()
  const { isOpen: isStatsOpen, onOpen: onStatsOpen, onClose: onStatsClose } = useDisclosure()
  const [cleanDays, setCleanDays] = useState(7)
  const [cleanLevels, setCleanLevels] = useState<string[]>(['INFO', 'WARN'])
  const [isCleaning, setIsCleaning] = useState(false)
  const [logStats, setLogStats] = useState<LogStats | null>(null)
  const toast = useToast()
  
  // 事件中心状態
  const [eventCenterEnabled, setEventCenterEnabled] = useState(false)
  const [eventCenterLoading, setEventCenterLoading] = useState(false)

  const handleCleanLogs = async () => {
    setIsCleaning(true)
    try {
      const response = await cleanLogs({
        days: cleanDays,
        levels: cleanLevels.length > 0 ? cleanLevels : undefined,
      })
      toast({
        title: t('logs.cleanSuccess'),
        description: t('logs.cleanSuccessDesc', { count: response.rows_affected }),
        status: 'success',
        duration: 3000,
      })
      onCleanClose()
      loadLogs() // 重新加載日志
    } catch (err: any) {
      toast({
        title: t('logs.cleanFailed'),
        description: err.message || t('logs.cleanFailed'),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setIsCleaning(false)
    }
  }

  const handleVacuum = async () => {
    setIsCleaning(true)
    try {
      await vacuumLogs()
      toast({
        title: t('logs.optimizeSuccess'),
        description: t('logs.optimizeSuccessDesc'),
        status: 'success',
        duration: 3000,
      })
    } catch (err: any) {
      toast({
        title: t('logs.optimizeFailed'),
        description: err.message || t('logs.optimizeFailed'),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setIsCleaning(false)
    }
  }

  const loadLogStats = async () => {
    try {
      const stats = await getLogStats()
      setLogStats(stats)
      onStatsOpen()
    } catch (err: any) {
      toast({
        title: t('logs.statsFailed'),
        description: err.message || t('logs.statsFailed'),
        status: 'error',
        duration: 5000,
      })
    }
  }

  // 加載事件中心状態
  const loadEventCenterStatus = async () => {
    try {
      const status = await getEventCenterStatus()
      setEventCenterEnabled(status.enabled)
    } catch (err: any) {
      console.error('獲取事件中心状態失败:', err)
    }
  }

  // 切换事件中心状態
  const handleToggleEventCenter = async (enabled: boolean) => {
    setEventCenterLoading(true)
    try {
      const response = await setEventCenterStatus(enabled)
      setEventCenterEnabled(response.enabled)
      toast({
        title: enabled ? t('logs.eventCenterStarted') : t('logs.eventCenterStoppedToast'),
        description: response.message,
        status: 'success',
        duration: 3000,
      })
    } catch (err: any) {
      toast({
        title: t('logs.operationFailed'),
        description: err.message || t('logs.operationFailed'),
        status: 'error',
        duration: 5000,
      })
      // 恢複原状態
      setEventCenterEnabled(!enabled)
    } finally {
      setEventCenterLoading(false)
    }
  }

  // 初始化加載事件中心状態
  useEffect(() => {
    loadEventCenterStatus()
  }, [])

  return (
    <div className="logs-container">
      <div className="logs-header">
        <h2>{t('logs.title')}</h2>
        <div className="logs-controls">
          <label>
            <input
              type="checkbox"
              checked={realtimeEnabled}
              onChange={(e) => setRealtimeEnabled(e.target.checked)}
            />
            {t('logs.realtimeUpdate')}
          </label>
          <label>
            <input
              type="checkbox"
              checked={autoScroll}
              onChange={(e) => setAutoScroll(e.target.checked)}
            />
            {t('logs.autoScroll')}
          </label>
          <button onClick={handleClear}>{t('logs.clear')}</button>
          <button onClick={loadLogStats}>{t('logs.statistics')}</button>
          <button onClick={onCleanOpen}>{t('logs.cleanUp')}</button>
          <button onClick={handleVacuum} disabled={isCleaning}>
            {isCleaning ? t('logs.optimizing') : t('logs.optimizeDb')}
          </button>
          <button onClick={loadLogs} disabled={loading}>
            {loading ? t('logs.loading') : t('common.refresh')}
          </button>
        </div>
      </div>

      {/* 事件中心状態 */}
      <Box 
        bg="blue.50" 
        p={3} 
        borderRadius="md" 
        mb={4}
        borderLeft="4px solid"
        borderColor="blue.500"
      >
        <HStack justify="space-between" align="center">
          <HStack spacing={3}>
            <Text fontWeight="bold" fontSize="md">
              📊 {t('logs.eventCenterStatus')}
            </Text>
            <Badge 
              colorScheme={eventCenterEnabled ? 'green' : 'gray'}
              fontSize="sm"
              px={2}
              py={1}
            >
              {eventCenterEnabled ? `✅ ${t('logs.running')}` : `⏸️ ${t('logs.stopped')}`}
            </Badge>
            <Text fontSize="sm" color="gray.600">
              {eventCenterEnabled 
                ? t('logs.eventCenterRecording') 
                : t('logs.eventCenterStopped')}
            </Text>
          </HStack>
          <HStack spacing={2}>
            <Text fontSize="sm" fontWeight="medium">
              {eventCenterEnabled ? t('logs.disable') : t('logs.enable')}
            </Text>
            <Switch
              colorScheme="green"
              size="lg"
              isChecked={eventCenterEnabled}
              onChange={(e) => handleToggleEventCenter(e.target.checked)}
              isDisabled={eventCenterLoading}
            />
          </HStack>
        </HStack>
      </Box>

      <div className="logs-filters">
        <div className="filter-group">
          <label>{t('logs.logLevel')}</label>
          <select value={level} onChange={(e) => setLevel(e.target.value)}>
            <option value="">{t('logs.all')}</option>
            <option value="DEBUG">DEBUG</option>
            <option value="INFO">INFO</option>
            <option value="WARN">WARN</option>
            <option value="ERROR">ERROR</option>
            <option value="FATAL">FATAL</option>
          </select>
        </div>

        <div className="filter-group">
          <label>{t('logs.keyword')}</label>
          <input
            type="text"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder={t('logs.keywordPlaceholder')}
          />
        </div>

        <div className="filter-group">
          <label>{t('logs.startTime')}</label>
          <input
            type="datetime-local"
            value={startTime}
            onChange={(e) => setStartTime(e.target.value)}
          />
        </div>

        <div className="filter-group">
          <label>{t('logs.endTime')}</label>
          <input
            type="datetime-local"
            value={endTime}
            onChange={(e) => setEndTime(e.target.value)}
          />
        </div>

        <button onClick={handleReset}>{t('logs.reset')}</button>
      </div>

      {error && (
        <div className="logs-error">
          {t('logs.error')}: {error}
        </div>
      )}

      <div className="logs-info">
        {t('logs.logCount', { total, showing: logs.length })}
      </div>

      <div className="logs-list-container">
        <div className="logs-list">
          {logs.length === 0 && !loading ? (
            <div className="logs-empty">{t('logs.noLogs')}</div>
          ) : (
            logs.map((log) => (
              <div key={log.id} className={`log-item ${getLevelClass(log.level)}`}>
                <span className="log-time">{formatTime(log.timestamp)}</span>
                <span className={`log-level ${getLevelClass(log.level)}`}>
                  [{log.level}]
                </span>
                <span className="log-message">{log.message}</span>
              </div>
            ))
          )}
          <div ref={logsEndRef} />
        </div>
      </div>

      {!realtimeEnabled && total > limit && (
        <div className="logs-pagination">
          <button
            onClick={() => setPage((p) => Math.max(1, p - 1))}
            disabled={page === 1 || loading}
          >
            {t('logs.prevPage')}
          </button>
          <span>
            {t('logs.pageInfo', { current: page, total: Math.ceil(total / limit) })}
          </span>
          <button
            onClick={() => setPage((p) => p + 1)}
            disabled={page >= Math.ceil(total / limit) || loading}
          >
            {t('logs.nextPage')}
          </button>
        </div>
      )}

      {/* 清理日志對话框 */}
      <Modal isOpen={isCleanOpen} onClose={onCleanClose}>
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{t('logs.cleanLogs')}</ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <VStack spacing={4} align="stretch">
              <Alert status="warning">
                <AlertIcon />
                <AlertDescription>
                  {t('logs.cleanWarning')}
                </AlertDescription>
              </Alert>

              <FormControl>
                <FormLabel>{t('logs.keepDays')}</FormLabel>
                <NumberInput
                  value={cleanDays}
                  onChange={(_, value) => setCleanDays(value)}
                  min={1}
                  max={365}
                >
                  <NumberInputField />
                </NumberInput>
                <Text fontSize="sm" color="gray.500" mt={1}>
                  {t('logs.deleteBeforeDays', { days: cleanDays })}
                </Text>
              </FormControl>

              <FormControl>
                <FormLabel>{t('logs.logLevelEmpty')}</FormLabel>
                <CheckboxGroup
                  value={cleanLevels}
                  onChange={(values) => setCleanLevels(values as string[])}
                >
                  <VStack align="start" spacing={2}>
                    <Checkbox value="DEBUG">DEBUG</Checkbox>
                    <Checkbox value="INFO">INFO</Checkbox>
                    <Checkbox value="WARN">WARN</Checkbox>
                    <Checkbox value="ERROR">ERROR</Checkbox>
                    <Checkbox value="FATAL">FATAL</Checkbox>
                  </VStack>
                </CheckboxGroup>
              </FormControl>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" mr={3} onClick={onCleanClose}>
              {t('common.cancel')}
            </Button>
            <Button
              colorScheme="red"
              onClick={handleCleanLogs}
              isLoading={isCleaning}
            >
              {t('logs.confirmClean')}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 日志统计對话框 */}
      <Modal isOpen={isStatsOpen} onClose={onStatsClose}>
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{t('logs.logStats')}</ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            {logStats && (
              <VStack spacing={4} align="stretch">
                <Text>
                  <strong>{t('logs.totalLogs')}: </strong>
                  {logStats.total.toLocaleString()}
                </Text>
                <Text>
                  <strong>{t('logs.byLevel')}: </strong>
                </Text>
                {Object.entries(logStats.by_level).map(([level, count]) => (
                  <Text key={level} pl={4}>
                    {level}: {count.toLocaleString()}
                  </Text>
                ))}
                {logStats.oldest_time && (
                  <Text>
                    <strong>{t('logs.oldestLog')}: </strong>
                    {new Date(logStats.oldest_time).toLocaleString('zh-CN')}
                  </Text>
                )}
                {logStats.newest_time && (
                  <Text>
                    <strong>{t('logs.newestLog')}: </strong>
                    {new Date(logStats.newest_time).toLocaleString('zh-CN')}
                  </Text>
                )}
              </VStack>
            )}
          </ModalBody>
          <ModalFooter>
            <Button onClick={onStatsClose}>{t('logs.close')}</Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </div>
  )
}

export default Logs

