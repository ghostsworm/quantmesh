import React, { useState, useEffect, useRef } from 'react'
import { getLogs, LogEntry, subscribeLogs, cleanLogs, getLogStats, vacuumLogs, LogStats } from '../services/api'
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
} from '@chakra-ui/react'

// Alias for backward compatibility
type LogRecord = LogEntry

const Logs: React.FC = () => {
  const [logs, setLogs] = useState<LogRecord[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [limit] = useState(100)
  
  // 过滤条件
  const [level, setLevel] = useState<string>('')
  const [keyword, setKeyword] = useState<string>('')
  const [startTime, setStartTime] = useState<string>('')
  const [endTime, setEndTime] = useState<string>('')
  
  // 实时更新
  const [realtimeEnabled, setRealtimeEnabled] = useState(true)
  const [autoScroll, setAutoScroll] = useState(true)
  
  const logsEndRef = useRef<HTMLDivElement>(null)
  const unsubscribeRef = useRef<(() => void) | null>(null)

  // 加载日志
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
      setError(err.message || '加载日志失败')
    } finally {
      setLoading(false)
    }
  }

  // 初始化加载
  useEffect(() => {
    loadLogs()
  }, [page, level, keyword, startTime, endTime])

  // 实时日志订阅
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
          // 将新日志添加到列表末尾（因为日志是按时间倒序排列的，新日志应该在前面）
          // 但为了实时更新体验，我们添加到开头
          const newLogs = [log, ...prevLogs]
          // 限制最多保留1000条
          return newLogs.slice(0, 1000)
        })
        setTotal((prev) => prev + 1)
      },
      (err) => {
        console.error('WebSocket error:', err)
        setError('实时日志连接失败')
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

  // 获取日志级别样式
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

  // 格式化时间
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

  // 重置过滤条件
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

  const handleCleanLogs = async () => {
    setIsCleaning(true)
    try {
      const response = await cleanLogs({
        days: cleanDays,
        levels: cleanLevels.length > 0 ? cleanLevels : undefined,
      })
      toast({
        title: '清理成功',
        description: `已清理 ${response.rows_affected} 条日志`,
        status: 'success',
        duration: 3000,
      })
      onCleanClose()
      loadLogs() // 重新加载日志
    } catch (err: any) {
      toast({
        title: '清理失败',
        description: err.message || '清理日志时发生错误',
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
        title: '优化成功',
        description: '数据库优化完成',
        status: 'success',
        duration: 3000,
      })
    } catch (err: any) {
      toast({
        title: '优化失败',
        description: err.message || '优化数据库时发生错误',
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
        title: '获取统计失败',
        description: err.message || '获取日志统计时发生错误',
        status: 'error',
        duration: 5000,
      })
    }
  }

  return (
    <div className="logs-container">
      <div className="logs-header">
        <h2>系统日志</h2>
        <div className="logs-controls">
          <label>
            <input
              type="checkbox"
              checked={realtimeEnabled}
              onChange={(e) => setRealtimeEnabled(e.target.checked)}
            />
            实时更新
          </label>
          <label>
            <input
              type="checkbox"
              checked={autoScroll}
              onChange={(e) => setAutoScroll(e.target.checked)}
            />
            自动滚动
          </label>
          <button onClick={handleClear}>清空</button>
          <button onClick={loadLogStats}>统计</button>
          <button onClick={onCleanOpen}>清理</button>
          <button onClick={handleVacuum} disabled={isCleaning}>
            {isCleaning ? '优化中...' : '优化数据库'}
          </button>
          <button onClick={loadLogs} disabled={loading}>
            {loading ? '加载中...' : '刷新'}
          </button>
        </div>
      </div>

      <div className="logs-filters">
        <div className="filter-group">
          <label>日志级别：</label>
          <select value={level} onChange={(e) => setLevel(e.target.value)}>
            <option value="">全部</option>
            <option value="DEBUG">DEBUG</option>
            <option value="INFO">INFO</option>
            <option value="WARN">WARN</option>
            <option value="ERROR">ERROR</option>
            <option value="FATAL">FATAL</option>
          </select>
        </div>

        <div className="filter-group">
          <label>关键词：</label>
          <input
            type="text"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder="搜索日志内容..."
          />
        </div>

        <div className="filter-group">
          <label>开始时间：</label>
          <input
            type="datetime-local"
            value={startTime}
            onChange={(e) => setStartTime(e.target.value)}
          />
        </div>

        <div className="filter-group">
          <label>结束时间：</label>
          <input
            type="datetime-local"
            value={endTime}
            onChange={(e) => setEndTime(e.target.value)}
          />
        </div>

        <button onClick={handleReset}>重置</button>
      </div>

      {error && (
        <div className="logs-error">
          错误: {error}
        </div>
      )}

      <div className="logs-info">
        共 {total} 条日志，当前显示 {logs.length} 条
      </div>

      <div className="logs-list-container">
        <div className="logs-list">
          {logs.length === 0 && !loading ? (
            <div className="logs-empty">暂无日志</div>
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
            上一页
          </button>
          <span>
            第 {page} 页 / 共 {Math.ceil(total / limit)} 页
          </span>
          <button
            onClick={() => setPage((p) => p + 1)}
            disabled={page >= Math.ceil(total / limit) || loading}
          >
            下一页
          </button>
        </div>
      )}

      {/* 清理日志对话框 */}
      <Modal isOpen={isCleanOpen} onClose={onCleanClose}>
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>清理日志</ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <VStack spacing={4} align="stretch">
              <Alert status="warning">
                <AlertIcon />
                <AlertDescription>
                  此操作将永久删除指定天数之前的日志，无法恢复！
                </AlertDescription>
              </Alert>

              <FormControl>
                <FormLabel>保留天数</FormLabel>
                <NumberInput
                  value={cleanDays}
                  onChange={(_, value) => setCleanDays(value)}
                  min={1}
                  max={365}
                >
                  <NumberInputField />
                </NumberInput>
                <Text fontSize="sm" color="gray.500" mt={1}>
                  将删除 {cleanDays} 天之前的日志
                </Text>
              </FormControl>

              <FormControl>
                <FormLabel>日志级别（留空则清理所有级别）</FormLabel>
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
              取消
            </Button>
            <Button
              colorScheme="red"
              onClick={handleCleanLogs}
              isLoading={isCleaning}
            >
              确认清理
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 日志统计对话框 */}
      <Modal isOpen={isStatsOpen} onClose={onStatsClose}>
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>日志统计</ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            {logStats && (
              <VStack spacing={4} align="stretch">
                <Text>
                  <strong>总日志数：</strong>
                  {logStats.total.toLocaleString()}
                </Text>
                <Text>
                  <strong>按级别统计：</strong>
                </Text>
                {Object.entries(logStats.by_level).map(([level, count]) => (
                  <Text key={level} pl={4}>
                    {level}: {count.toLocaleString()}
                  </Text>
                ))}
                {logStats.oldest_time && (
                  <Text>
                    <strong>最早日志：</strong>
                    {new Date(logStats.oldest_time).toLocaleString('zh-CN')}
                  </Text>
                )}
                {logStats.newest_time && (
                  <Text>
                    <strong>最新日志：</strong>
                    {new Date(logStats.newest_time).toLocaleString('zh-CN')}
                  </Text>
                )}
              </VStack>
            )}
          </ModalBody>
          <ModalFooter>
            <Button onClick={onStatsClose}>关闭</Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </div>
  )
}

export default Logs

