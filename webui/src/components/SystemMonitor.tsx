import React, { useState, useEffect } from 'react';
import { getCurrentSystemMetrics, getDailySystemMetrics, SystemMetrics } from '../services/api';
import './SystemMonitor.css';

interface SystemMetrics {
  timestamp: string;
  cpu_percent: number;
  memory_mb: number;
  memory_percent: number;
  process_id: number;
}

interface DailySystemMetrics {
  date: string;
  avg_cpu_percent: number;
  max_cpu_percent: number;
  min_cpu_percent: number;
  avg_memory_mb: number;
  max_memory_mb: number;
  min_memory_mb: number;
  sample_count: number;
}

interface CurrentMetrics {
  timestamp: string;
  cpu_percent: number;
  memory_mb: number;
  memory_percent: number;
  process_id: number;
}

const SystemMonitor: React.FC = () => {
  const [currentMetrics, setCurrentMetrics] = useState<CurrentMetrics | null>(null);
  const [metrics, setMetrics] = useState<SystemMetrics[]>([]);
  const [dailyMetrics, setDailyMetrics] = useState<DailySystemMetrics[]>([]);
  const [timeRange, setTimeRange] = useState<string>('24h');
  const [metricType, setMetricType] = useState<'cpu' | 'memory'>('cpu');
  const [loading, setLoading] = useState<boolean>(false);

  // 获取当前系统状态
  const fetchCurrentMetrics = async () => {
    try {
      const data = await api.get('/api/system/metrics/current');
      setCurrentMetrics(data);
    } catch (error) {
      console.error('获取当前系统状态失败:', error);
    }
  };

  // 获取监控数据
  const fetchMetrics = async () => {
    setLoading(true);
    try {
      const now = new Date();
      let startTime: Date;
      let useDaily = false;

      switch (timeRange) {
        case '1h':
          startTime = new Date(now.getTime() - 60 * 60 * 1000);
          break;
        case '6h':
          startTime = new Date(now.getTime() - 6 * 60 * 60 * 1000);
          break;
        case '24h':
          startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000);
          break;
        case '7d':
          startTime = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
          break;
        case '30d':
          startTime = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
          useDaily = true;
          break;
        default:
          startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000);
      }

      if (useDaily) {
        const days = Math.ceil((now.getTime() - startTime.getTime()) / (24 * 60 * 60 * 1000));
        const data = await api.get(`/api/system/metrics/daily?days=${days}`);
        setDailyMetrics(data.metrics || []);
        setMetrics([]);
      } else {
        const startTimeStr = startTime.toISOString();
        const endTimeStr = now.toISOString();
        const data = await api.get(
          `/api/system/metrics?start_time=${startTimeStr}&end_time=${endTimeStr}&granularity=detail`
        );
        setMetrics(data.metrics || []);
        setDailyMetrics([]);
      }
    } catch (error) {
      console.error('获取监控数据失败:', error);
    } finally {
      setLoading(false);
    }
  };

  // 初始化数据
  useEffect(() => {
    fetchCurrentMetrics();
    fetchMetrics();

    // 每30秒刷新当前状态
    const interval = setInterval(() => {
      fetchCurrentMetrics();
    }, 30000);

    // 每5分钟刷新历史数据
    const metricsInterval = setInterval(() => {
      fetchMetrics();
    }, 5 * 60 * 1000);

    return () => {
      clearInterval(interval);
      clearInterval(metricsInterval);
    };
  }, []);

  // 当时间范围改变时重新获取数据
  useEffect(() => {
    fetchMetrics();
  }, [timeRange]);

  // 准备图表数据
  const prepareChartData = () => {
    if (timeRange === '30d' && dailyMetrics.length > 0) {
      const labels = dailyMetrics.map((m) => m.date || '');
      const data = dailyMetrics.map((m) => {
        const value = metricType === 'cpu' ? m.avg_cpu_percent : m.avg_memory_mb;
        return typeof value === 'number' && !isNaN(value) ? value : 0;
      });
      const maxData = dailyMetrics.map((m) => {
        const value = metricType === 'cpu' ? m.max_cpu_percent : m.max_memory_mb;
        return typeof value === 'number' && !isNaN(value) ? value : 0;
      });
      const minData = dailyMetrics.map((m) => {
        const value = metricType === 'cpu' ? m.min_cpu_percent : m.min_memory_mb;
        return typeof value === 'number' && !isNaN(value) ? value : 0;
      });

      return {
        labels,
        datasets: [
          { label: '平均值', data, color: '#4CAF50' },
          { label: '最大值', data: maxData, color: '#f44336' },
          { label: '最小值', data: minData, color: '#2196F3' },
        ],
      };
    } else if (metrics.length > 0) {
      const labels = metrics.map((m) => m.timestamp || '');
      const data = metrics.map((m) => {
        const value = metricType === 'cpu' ? m.cpu_percent : m.memory_mb;
        return typeof value === 'number' && !isNaN(value) ? value : 0;
      });

      return {
        labels,
        datasets: [{ label: metricType === 'cpu' ? 'CPU使用率 (%)' : '内存使用 (MB)', data, color: '#4CAF50' }],
      };
    }

    return {
      labels: [],
      datasets: [],
    };
  };

  const chartData = prepareChartData();

  // 简化的数据展示
  const renderSimpleChart = () => {
    if (chartData.datasets.length === 0) {
      return <div className="no-data">暂无数据</div>;
    }

    const mainDataset = chartData.datasets[0];
    const values = (mainDataset.data as number[]).filter((v) => v != null && !isNaN(v));
    
    if (values.length === 0) {
      return <div className="no-data">暂无数据</div>;
    }
    
    const maxValue = Math.max(...values.map((v) => v || 0));
    const minValue = Math.min(...values.map((v) => v || 0));
    const range = maxValue - minValue || 1;
    const avgValue = values.reduce((a, b) => a + b, 0) / values.length;

    return (
      <div className="simple-chart">
        <h3>{metricType === 'cpu' ? 'CPU使用率趋势' : '内存使用趋势'}</h3>
        <div className="chart-bars">
          {values.map((value: number, index: number) => {
            if (value == null || isNaN(value)) {
              return null;
            }
            const height = ((value - minValue) / range) * 100;
            const label = chartData.labels[index] || '';
            return (
              <div key={index} className="chart-bar-container">
                <div
                  className="chart-bar"
                  style={{ height: `${Math.max(height, 2)}%` }}
                  title={`${label}: ${value.toFixed(2)}${metricType === 'cpu' ? '%' : ' MB'}`}
                />
              </div>
            );
          })}
        </div>
        <div className="chart-labels">
          {chartData.labels.slice(0, Math.min(20, chartData.labels.length)).map((label: string, index: number) => (
            <span key={index} className="chart-label">
              {label ? new Date(label).toLocaleTimeString() : ''}
            </span>
          ))}
        </div>
        <div className="chart-stats">
          <span>最大值: {typeof maxValue === 'number' && !isNaN(maxValue) ? maxValue.toFixed(2) : '--'}{metricType === 'cpu' ? '%' : ' MB'}</span>
          <span>最小值: {typeof minValue === 'number' && !isNaN(minValue) ? minValue.toFixed(2) : '--'}{metricType === 'cpu' ? '%' : ' MB'}</span>
          <span>平均值: {typeof avgValue === 'number' && !isNaN(avgValue) ? avgValue.toFixed(2) : '--'}{metricType === 'cpu' ? '%' : ' MB'}</span>
        </div>
      </div>
    );
  };

  return (
    <div className="system-monitor">
      <div className="monitor-header">
        <h1>系统监控</h1>
        <div className="controls">
          <select
            value={timeRange}
            onChange={(e) => setTimeRange(e.target.value)}
            className="time-range-select"
          >
            <option value="1h">最近1小时</option>
            <option value="6h">最近6小时</option>
            <option value="24h">最近24小时</option>
            <option value="7d">最近7天</option>
            <option value="30d">最近30天</option>
          </select>
          <select
            value={metricType}
            onChange={(e) => setMetricType(e.target.value as 'cpu' | 'memory')}
            className="metric-type-select"
          >
            <option value="cpu">CPU使用率</option>
            <option value="memory">内存使用</option>
          </select>
        </div>
      </div>

      {/* 当前状态卡片 */}
      <div className="current-status">
        <div className="status-card">
          <h3>当前CPU使用率</h3>
          <div className="status-value">
            {currentMetrics && typeof currentMetrics.cpu_percent === 'number' 
              ? `${currentMetrics.cpu_percent.toFixed(2)}%` 
              : '--'}
          </div>
        </div>
        <div className="status-card">
          <h3>当前内存使用</h3>
          <div className="status-value">
            {currentMetrics && typeof currentMetrics.memory_mb === 'number' 
              ? `${currentMetrics.memory_mb.toFixed(2)} MB` 
              : '--'}
          </div>
        </div>
        <div className="status-card">
          <h3>内存占用百分比</h3>
          <div className="status-value">
            {currentMetrics && typeof currentMetrics.memory_percent === 'number' 
              ? `${currentMetrics.memory_percent.toFixed(2)}%` 
              : '--'}
          </div>
        </div>
        <div className="status-card">
          <h3>进程ID</h3>
          <div className="status-value">
            {currentMetrics && currentMetrics.process_id 
              ? currentMetrics.process_id 
              : '--'}
          </div>
        </div>
      </div>

      {/* 图表 */}
      <div className="chart-container">
        {loading ? (
          <div className="loading">加载中...</div>
        ) : (
          renderSimpleChart()
        )}
      </div>
    </div>
  );
};

export default SystemMonitor;
