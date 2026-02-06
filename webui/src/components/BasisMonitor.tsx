import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getBasisCurrent, getBasisHistory, getBasisStatistics, BasisData, BasisStats } from '../services/api';
import { Line } from 'react-chartjs-2';
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  ChartOptions
} from 'chart.js';

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend
);

const BasisMonitor: React.FC = () => {
  const { t } = useTranslation();
  const [currentBasis, setCurrentBasis] = useState<BasisData[]>([]);
  const [selectedSymbol, setSelectedSymbol] = useState<string>('BTCUSDT');
  const [history, setHistory] = useState<BasisData[]>([]);
  const [statistics, setStatistics] = useState<BasisStats | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [serviceUnavailable, setServiceUnavailable] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);

  // 检查是否是服務不可用錯误
  const isServiceUnavailable = (err: unknown): boolean => {
    if (err instanceof Error) {
      return err.message.includes('503') || err.message.includes('service_unavailable');
    }
    return false;
  };

  // 獲取所有交易對的當前價差
  const fetchCurrentBasis = async () => {
    try {
      const data = await getBasisCurrent();
      setCurrentBasis(data);
      setError(null);
      setServiceUnavailable(false);
    } catch (err) {
      if (isServiceUnavailable(err)) {
        setServiceUnavailable(true);
        setError(t('basisMonitor.serviceUnavailableMsg'));
      } else {
        setError(err instanceof Error ? err.message : t('basisMonitor.fetchBasisDataFailed'));
        setServiceUnavailable(false);
      }
    }
  };

  // 獲取歷史數據
  const fetchHistory = async (symbol: string) => {
    try {
      setLoading(true);
      const data = await getBasisHistory(symbol, 100);
      setHistory(data);
      setError(null);
      setServiceUnavailable(false);
    } catch (err) {
      if (isServiceUnavailable(err)) {
        setServiceUnavailable(true);
        if (!error) {
          setError(t('basisMonitor.serviceUnavailableMsg'));
        }
      } else {
        if (!serviceUnavailable) {
          setError(err instanceof Error ? err.message : t('basisMonitor.fetchHistoryFailed'));
        }
      }
    } finally {
      setLoading(false);
    }
  };

  // 獲取统计數據
  const fetchStatistics = async (symbol: string, hours: number = 24) => {
    try {
      const data = await getBasisStatistics(symbol, hours);
      setStatistics(data);
      setError(null);
      setServiceUnavailable(false);
    } catch (err) {
      if (isServiceUnavailable(err)) {
        setServiceUnavailable(true);
        if (!error) {
          setError(t('basisMonitor.serviceUnavailableMsg'));
        }
      } else {
        if (!serviceUnavailable) {
          setError(err instanceof Error ? err.message : t('basisMonitor.fetchStatisticsFailed'));
        }
      }
    }
  };

  // 初始加載
  useEffect(() => {
    fetchCurrentBasis();
    fetchHistory(selectedSymbol);
    fetchStatistics(selectedSymbol);
  }, [selectedSymbol]);

  // 自动刷新
  useEffect(() => {
    if (!autoRefresh) return;

    const interval = setInterval(() => {
      fetchCurrentBasis();
      if (selectedSymbol) {
        fetchHistory(selectedSymbol);
        fetchStatistics(selectedSymbol);
      }
    }, 30000); // 30秒刷新一次

    return () => clearInterval(interval);
  }, [autoRefresh, selectedSymbol]);

  // 准备图表數據
  const chartData = {
    labels: history.map(d => new Date(d.timestamp).toLocaleTimeString()),
    datasets: [
      {
        label: t('basisMonitor.basisPercent'),
        data: history.map(d => d.basis_percent),
        borderColor: 'rgb(75, 192, 192)',
        backgroundColor: 'rgba(75, 192, 192, 0.2)',
        tension: 0.1,
      },
      {
        label: t('basisMonitor.fundingRatePercent'),
        data: history.map(d => d.funding_rate * 100),
        borderColor: 'rgb(255, 99, 132)',
        backgroundColor: 'rgba(255, 99, 132, 0.2)',
        tension: 0.1,
      }
    ]
  };

  const chartOptions: ChartOptions<'line'> = {
    responsive: true,
    plugins: {
      legend: {
        position: 'top' as const,
      },
      title: {
        display: true,
        text: t('basisMonitor.chartTitle', { symbol: selectedSymbol }),
      },
    },
    scales: {
      y: {
        beginAtZero: false,
      }
    }
  };

  // 格式化價格
  const formatPrice = (price: number) => {
    return price.toLocaleString('en-US', {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2
    });
  };

  // 格式化百分比
  const formatPercent = (value: number) => {
    const sign = value >= 0 ? '+' : '';
    return `${sign}${value.toFixed(4)}%`;
  };

  // 獲取價差颜色
  const getBasisColor = (basisPercent: number) => {
    if (basisPercent > 0.3) return 'text-red-600';
    if (basisPercent < -0.3) return 'text-green-600';
    return 'text-gray-600';
  };

  return (
    <div className="p-6 space-y-6">
      {/* 標题和控制 */}
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold">{t('basisMonitor.title')}</h1>
        <div className="flex items-center space-x-4">
          <label className="flex items-center space-x-2">
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={(e) => setAutoRefresh(e.target.checked)}
              className="form-checkbox"
            />
            <span>{t('basisMonitor.autoRefresh')}</span>
          </label>
          <button
            onClick={() => {
              fetchCurrentBasis();
              fetchHistory(selectedSymbol);
              fetchStatistics(selectedSymbol);
            }}
            className="px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600"
          >
            🔄 {t('basisMonitor.refresh')}
          </button>
        </div>
      </div>

      {/* 錯誤提示 */}
      {error && (
        <div className={`border px-4 py-3 rounded ${
          serviceUnavailable 
            ? 'bg-yellow-50 border-yellow-400 text-yellow-800' 
            : 'bg-red-100 border-red-400 text-red-700'
        }`}>
          <div className="font-semibold mb-2">
            {serviceUnavailable ? t('basisMonitor.serviceNotEnabled') : t('basisMonitor.error')}
          </div>
          <div className="text-sm">{error}</div>
          {serviceUnavailable && (
            <div className="mt-3 text-sm">
              <p className="font-semibold mb-1">{t('basisMonitor.enableMethod')}</p>
              <ol className="list-decimal list-inside space-y-1 ml-2">
                <li>{t('basisMonitor.editConfigFile')} <code className="bg-yellow-100 px-1 rounded">config.yaml</code></li>
                <li>{t('basisMonitor.addOrModifyConfig')}</li>
              </ol>
              <pre className="mt-2 p-3 bg-yellow-100 rounded text-xs overflow-x-auto">
{`basis_monitor:
  enabled: true
  interval_minutes: 1
  symbols:
    - BTCUSDT
    - ETHUSDT`}
              </pre>
              <p className="mt-2 text-xs">{t('basisMonitor.restartServiceHint')}</p>
            </div>
          )}
        </div>
      )}

      {/* 當前價差概览 */}
      <div className="bg-white rounded-lg shadow p-6">
        <h2 className="text-xl font-semibold mb-4">{t('basisMonitor.realtimeBasis')}</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          {currentBasis.map((data) => (
            <div
              key={data.symbol}
              className={`p-4 border rounded-lg cursor-pointer transition-all ${
                selectedSymbol === data.symbol
                  ? 'border-blue-500 bg-blue-50'
                  : 'border-gray-200 hover:border-gray-300'
              }`}
              onClick={() => setSelectedSymbol(data.symbol)}
            >
              <div className="font-semibold text-lg mb-2">{data.symbol}</div>
              <div className="space-y-1 text-sm">
                <div className="flex justify-between">
                  <span className="text-gray-600">{t('basisMonitor.spot')}</span>
                  <span>${formatPrice(data.spot_price)}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-gray-600">{t('basisMonitor.futures')}</span>
                  <span>${formatPrice(data.futures_price)}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-gray-600">{t('basisMonitor.basis')}</span>
                  <span className={getBasisColor(data.basis_percent)}>
                    {formatPercent(data.basis_percent)}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-gray-600">{t('basisMonitor.fundingRate')}</span>
                  <span>{formatPercent(data.funding_rate * 100)}</span>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* 统计數據 */}
      {statistics && (
        <div className="bg-white rounded-lg shadow p-6">
          <h2 className="text-xl font-semibold mb-4">
            {t('basisMonitor.statisticsTitle', { symbol: selectedSymbol, hours: statistics.hours })}
          </h2>
          <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
            <div className="text-center p-4 bg-gray-50 rounded">
              <div className="text-gray-600 text-sm">{t('basisMonitor.avgBasis')}</div>
              <div className="text-lg font-semibold">
                {formatPercent(statistics.avg_basis)}
              </div>
            </div>
            <div className="text-center p-4 bg-gray-50 rounded">
              <div className="text-gray-600 text-sm">{t('basisMonitor.maxBasis')}</div>
              <div className="text-lg font-semibold text-red-600">
                {formatPercent(statistics.max_basis)}
              </div>
            </div>
            <div className="text-center p-4 bg-gray-50 rounded">
              <div className="text-gray-600 text-sm">{t('basisMonitor.minBasis')}</div>
              <div className="text-lg font-semibold text-green-600">
                {formatPercent(statistics.min_basis)}
              </div>
            </div>
            <div className="text-center p-4 bg-gray-50 rounded">
              <div className="text-gray-600 text-sm">{t('basisMonitor.stdDev')}</div>
              <div className="text-lg font-semibold">
                {statistics.std_dev.toFixed(4)}%
              </div>
            </div>
            <div className="text-center p-4 bg-gray-50 rounded">
              <div className="text-gray-600 text-sm">{t('basisMonitor.dataPoints')}</div>
              <div className="text-lg font-semibold">
                {statistics.data_points}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* 历史图表 */}
      <div className="bg-white rounded-lg shadow p-6">
        <h2 className="text-xl font-semibold mb-4">{t('basisMonitor.historyTrend')}</h2>
        {loading ? (
          <div className="text-center py-8">{t('common.loading')}</div>
        ) : history.length > 0 ? (
          <Line data={chartData} options={chartOptions} />
        ) : (
          <div className="text-center py-8 text-gray-500">{t('basisMonitor.noHistoryData')}</div>
        )}
      </div>

      {/* 說明 */}
      <div className="bg-blue-50 border border-blue-200 rounded-lg p-4">
        <h3 className="font-semibold mb-2">{t('basisMonitor.basisExplanation')}</h3>
        <ul className="text-sm space-y-1 text-gray-700">
          <li>• <strong>{t('basisMonitor.positiveBasis')}</strong>{t('basisMonitor.positiveBasisDesc')}</li>
          <li>• <strong>{t('basisMonitor.negativeBasis')}</strong>{t('basisMonitor.negativeBasisDesc')}</li>
          <li>• <strong>{t('basisMonitor.fundingRateLabel')}</strong>{t('basisMonitor.fundingRateDesc')}</li>
          <li>• {t('basisMonitor.basisCorrelationHint')}</li>
        </ul>
      </div>
    </div>
  );
};

export default BasisMonitor;
