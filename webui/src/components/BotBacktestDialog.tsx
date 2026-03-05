import React, { useState, useEffect } from 'react'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  Box,
  Typography,
  CircularProgress,
  Alert,
  Chip,
  Tabs,
  Tab,
  Paper,
  Grid,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
} from '@mui/material'
import { useTranslation } from 'react-i18next'
import {
  createBotBacktest,
  getBotBacktestTask,
  getBotBacktestResult,
  type BotBacktestRequest,
  type BotBacktestTask,
  type BotBacktestResult,
} from '../services/api'
import { Line } from 'react-chartjs-2'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler,
} from 'chart.js'

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
)

interface BotBacktestDialogProps {
  open: boolean
  onClose: () => void
  botId: string
  botName: string
  botConfig?: any
}

interface TabPanelProps {
  children?: React.ReactNode
  index: number
  value: number
}

function TabPanel({ children, value, index }: TabPanelProps) {
  return (
    <div role="tabpanel" hidden={value !== index}>
      {value === index && <Box sx={{ py: 3 }}>{children}</Box>}
    </div>
  )
}

const BotBacktestDialog: React.FC<BotBacktestDialogProps> = ({
  open,
  onClose,
  botId,
  botName,
  botConfig,
}) => {
  const { t } = useTranslation()
  const [tabValue, setTabValue] = useState(0)
  const [loading, setLoading] = useState(false)
  const [task, setTask] = useState<BotBacktestTask | null>(null)
  const [result, setResult] = useState<BotBacktestResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pollInterval, setPollInterval] = useState<NodeJS.Timeout | null>(null)

  // 表单状态
  const [formData, setFormData] = useState<BotBacktestRequest>({
    bot_id: botId,
    start_date: new Date(Date.now() - 90 * 24 * 60 * 60 * 1000).toISOString().split('T')[0],
    end_date: new Date().toISOString().split('T')[0],
    data_dir: './data',
    commission: 0.0004,
    leverage: 1,
  })

  useEffect(() => {
    return () => {
      if (pollInterval) {
        clearInterval(pollInterval)
      }
    }
  }, [pollInterval])

  const handleCreateBacktest = async () => {
    setLoading(true)
    setError(null)
    setTask(null)
    setResult(null)

    try {
      const response = await createBotBacktest(botId, formData)
      setTask({
        task_id: response.task_id,
        bot_id: botId,
        status: 'pending',
        created_at: new Date().toISOString(),
        progress: 0,
      })

      // 开始轮询任务状态
      startPolling(response.task_id)
      setTabValue(1) // 切换到结果标签页
    } catch (err: any) {
      setError(err.message || t('backtest.createFailed'))
      setLoading(false)
    }
  }

  const startPolling = (taskId: string) => {
    const interval = setInterval(async () => {
      try {
        const taskStatus = await getBotBacktestTask(taskId)
        setTask(taskStatus)

        if (taskStatus.status === 'completed') {
          clearInterval(interval)
          setLoading(false)
          // 获取详细结果
          const backtestResult = await getBotBacktestResult(taskId)
          setResult(backtestResult)
        } else if (taskStatus.status === 'failed') {
          clearInterval(interval)
          setLoading(false)
          setError(taskStatus.error || t('backtest.executionFailed'))
        }
      } catch (err: any) {
        console.error('Failed to poll backtest status:', err)
      }
    }, 2000) // 每2秒轮询一次

    setPollInterval(interval)
  }

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'completed':
        return 'success'
      case 'failed':
        return 'error'
      case 'running':
        return 'info'
      default:
        return 'default'
    }
  }

  const renderConfigForm = () => (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
      <Typography variant="h6">
        {t('backtest.configureParameters')}
      </Typography>

      <Grid container spacing={2}>
        <Grid item xs={6}>
          <TextField
            fullWidth
            label={t('backtest.startDate')}
            type="date"
            value={formData.start_date?.split('T')[0] || ''}
            onChange={(e) => setFormData({ ...formData, start_date: e.target.value })}
            InputLabelProps={{ shrink: true }}
          />
        </Grid>
        <Grid item xs={6}>
          <TextField
            fullWidth
            label={t('backtest.endDate')}
            type="date"
            value={formData.end_date?.split('T')[0] || ''}
            onChange={(e) => setFormData({ ...formData, end_date: e.target.value })}
            InputLabelProps={{ shrink: true }}
          />
        </Grid>
        <Grid item xs={6}>
          <TextField
            fullWidth
            label={t('backtest.commissionRate')}
            type="number"
            value={formData.commission}
            onChange={(e) => setFormData({ ...formData, commission: parseFloat(e.target.value) })}
            inputProps={{ step: 0.0001, min: 0 }}
          />
        </Grid>
        <Grid item xs={6}>
          <TextField
            fullWidth
            label={t('backtest.leverage')}
            type="number"
            value={formData.leverage}
            onChange={(e) => setFormData({ ...formData, leverage: parseFloat(e.target.value) })}
            inputProps={{ step: 0.1, min: 1 }}
          />
        </Grid>
        <Grid item xs={12}>
          <TextField
            fullWidth
            label={t('backtest.dataDirectory')}
            value={formData.data_dir}
            onChange={(e) => setFormData({ ...formData, data_dir: e.target.value })}
            helperText={t('backtest.dataDirectoryHelp')}
          />
        </Grid>
      </Grid>

      {error && (
        <Alert severity="error" onClose={() => setError(null)}>
          {error}
        </Alert>
      )}
    </Box>
  )

  const renderResults = () => {
    if (!task) {
      return (
        <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
          <Typography color="text.secondary">
            {t('backtest.noTask')}
          </Typography>
        </Box>
      )
    }

    return (
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
        {/* 任务状态 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
          <Chip
            label={task.status.toUpperCase()}
            color={getStatusColor(task.status) as any}
            size="small"
          />
          <Typography variant="body2" color="text.secondary">
            {t('backtest.taskId')}: {task.task_id}
          </Typography>
          {task.status === 'running' && (
            <CircularProgress size={20} />
          )}
        </Box>

        {/* 进度条 */}
        {task.status === 'running' && (
          <Box>
            <Typography variant="body2" gutterBottom>
              {t('backtest.progress')}: {task.progress.toFixed(1)}%
            </Typography>
            <Box
              sx={{
                width: '100%',
                height: 8,
                backgroundColor: 'action.hover',
                borderRadius: 1,
                overflow: 'hidden',
              }}
            >
              <Box
                sx={{
                  width: `${task.progress}%`,
                  height: '100%',
                  backgroundColor: 'primary.main',
                  transition: 'width 0.3s ease',
                }}
              />
            </Box>
          </Box>
        )}

        {/* 错误信息 */}
        {task.status === 'failed' && task.error && (
          <Alert severity="error">
            {task.error}
          </Alert>
        )}

        {/* 回测结果 */}
        {result && (
          <>
            {/* 关键指标 */}
            <Paper sx={{ p: 2 }}>
              <Typography variant="h6" gutterBottom>
                {t('backtest.summary')}
              </Typography>
              <Grid container spacing={2}>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">
                    {t('backtest.totalReturn')}
                  </Typography>
                  <Typography
                    variant="h6"
                    color={result.total_return_pct >= 0 ? 'success.main' : 'error.main'}
                  >
                    {result.total_return_pct.toFixed(2)}%
                  </Typography>
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">
                    {t('backtest.totalTrades')}
                  </Typography>
                  <Typography variant="h6">
                    {result.total_trades}
                  </Typography>
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">
                    {t('backtest.maxDrawdown')}
                  </Typography>
                  <Typography variant="h6" color="error.main">
                    {result.risk_metrics.max_drawdown_pct.toFixed(2)}%
                  </Typography>
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">
                    {t('backtest.winRate')}
                  </Typography>
                  <Typography variant="h6">
                    {result.risk_metrics.win_rate.toFixed(1)}%
                  </Typography>
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">
                    {t('backtest.sharpeRatio')}
                  </Typography>
                  <Typography variant="h6">
                    {result.risk_metrics.sharpe_ratio.toFixed(2)}
                  </Typography>
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">
                    {t('backtest.profitFactor')}
                  </Typography>
                  <Typography variant="h6">
                    {result.risk_metrics.profit_factor.toFixed(2)}
                  </Typography>
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">
                    {t('backtest.totalFees')}
                  </Typography>
                  <Typography variant="h6">
                    ${result.total_fees.toFixed(2)}
                  </Typography>
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">
                    {t('backtest.totalSlippage')}
                  </Typography>
                  <Typography variant="h6">
                    ${result.total_slippage.toFixed(2)}
                  </Typography>
                </Grid>
              </Grid>
            </Paper>

            {/* 权益曲线 */}
            <Paper sx={{ p: 2 }}>
              <Typography variant="h6" gutterBottom>
                {t('backtest.equityCurve')}
              </Typography>
              <Box sx={{ height: 300 }}>
                <Line
                  data={{
                    labels: result.equity_curve.map(p =>
                      new Date(p.timestamp).toLocaleDateString()
                    ),
                    datasets: [
                      {
                        label: t('backtest.equity'),
                        data: result.equity_curve.map(p => p.equity),
                        borderColor: 'rgb(75, 192, 192)',
                        backgroundColor: 'rgba(75, 192, 192, 0.1)',
                        fill: true,
                        tension: 0.1,
                      },
                    ],
                  }}
                  options={{
                    responsive: true,
                    maintainAspectRatio: false,
                    plugins: {
                      legend: {
                        display: false,
                      },
                    },
                    scales: {
                      x: {
                        display: true,
                        title: {
                          display: true,
                          text: t('backtest.date'),
                        },
                      },
                      y: {
                        display: true,
                        title: {
                          display: true,
                          text: t('backtest.equity'),
                        },
                      },
                    },
                  }}
                />
              </Box>
            </Paper>

            {/* 策略统计 */}
            {Object.keys(result.stats_by_strategy).length > 0 && (
              <Paper sx={{ p: 2 }}>
                <Typography variant="h6" gutterBottom>
                  {t('backtest.strategyStats')}
                </Typography>
                <TableContainer>
                  <Table size="small">
                    <TableHead>
                      <TableRow>
                        <TableCell>{t('backtest.strategy')}</TableCell>
                        <TableCell align="right">{t('backtest.trades')}</TableCell>
                        <TableCell align="right">{t('backtest.pnl')}</TableCell>
                        <TableCell align="right">{t('backtest.winRate')}</TableCell>
                        <TableCell align="right">{t('backtest.drawdown')}</TableCell>
                      </TableRow>
                    </TableHead>
                    <TableBody>
                      {Object.entries(result.stats_by_strategy).map(([key, stats]) => (
                        <TableRow key={key}>
                          <TableCell>{stats.name}</TableCell>
                          <TableCell align="right">{stats.total_trades}</TableCell>
                          <TableCell align="right">
                            <Typography
                              color={stats.realized_pnl >= 0 ? 'success.main' : 'error.main'}
                            >
                              ${stats.realized_pnl.toFixed(2)}
                            </Typography>
                          </TableCell>
                          <TableCell align="right">{stats.win_rate.toFixed(1)}%</TableCell>
                          <TableCell align="right">{stats.max_drawdown.toFixed(2)}%</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </TableContainer>
              </Paper>
            )}
          </>
        )}
      </Box>
    )
  }

  return (
    <Dialog open={open} onClose={onClose} maxWidth="lg" fullWidth>
      <DialogTitle>
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <Typography variant="h6">
            {t('backtest.title')}: {botName}
          </Typography>
        </Box>
      </DialogTitle>

      <DialogContent>
        <Tabs value={tabValue} onChange={(_, v) => setTabValue(v)}>
          <Tab label={t('backtest.config')} />
          <Tab label={t('backtest.results')} />
        </Tabs>

        <TabPanel value={tabValue} index={0}>
          {renderConfigForm()}
        </TabPanel>

        <TabPanel value={tabValue} index={1}>
          {renderResults()}
        </TabPanel>
      </DialogContent>

      <DialogActions>
        <Button onClick={onClose} disabled={loading}>
          {t('common.close')}
        </Button>
        {tabValue === 0 && (
          <Button
            variant="contained"
            onClick={handleCreateBacktest}
            disabled={loading}
            startIcon={loading ? <CircularProgress size={20} /> : null}
          >
            {loading ? t('backtest.running') : t('backtest.start')}
          </Button>
        )}
      </DialogActions>
    </Dialog>
  )
}

export default BotBacktestDialog
